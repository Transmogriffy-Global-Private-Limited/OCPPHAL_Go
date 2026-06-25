package session

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/Transmogriffy-Global-Private-Limited/OCPPHAL_Go/internal/ocpp"
	"github.com/Transmogriffy-Global-Private-Limited/OCPPHAL_Go/internal/state"
)

var (
	ErrSessionClosed = errors.New("charger session closed")
	ErrCallError     = errors.New("charger returned OCPP CallError")
)

type InboundHandler func(sess *Session, chargerID string, call *ocpp.Call)

type Response struct {
	Payload      json.RawMessage
	ErrorCode    string
	Description  string
	ErrorDetails json.RawMessage
}

type outgoingFrame struct {
	data []byte
}

type pendingResponse struct {
	ch chan Response
}

type Manager struct {
	mu       sync.RWMutex
	sessions map[string]*Session
	registry *state.Registry
	logger   *slog.Logger
}

func NewManager(registry *state.Registry, logger *slog.Logger) *Manager {
	return &Manager{
		sessions: make(map[string]*Session),
		registry: registry,
		logger:   logger,
	}
}

func (m *Manager) Handle(conn *websocket.Conn, chargerID string, serialNumber string, handler InboundHandler) {
	old := m.Register(conn, chargerID, serialNumber)
	if old != nil {
		old.Close("replaced by newer connection")
	}

	sess, ok := m.Get(chargerID)
	if !ok {
		return
	}

	m.logger.Info("charger session started", "charger_id", chargerID, "serial_number", serialNumber)

	sess.run(handler)

	m.Remove(chargerID, sess)
	m.registry.MarkOffline(chargerID)

	m.logger.Info("charger session ended", "charger_id", chargerID)
}

func (m *Manager) Register(conn *websocket.Conn, chargerID string, serialNumber string) *Session {
	m.mu.Lock()
	defer m.mu.Unlock()

	old := m.sessions[chargerID]

	ctx, cancel := context.WithCancel(context.Background())

	sess := &Session{
		ChargerID:    chargerID,
		SerialNumber: serialNumber,
		conn:         conn,
		outgoing:     make(chan outgoingFrame, 64),
		pending:      make(map[string]pendingResponse),
		ctx:          ctx,
		cancel:       cancel,
		logger:       m.logger.With("charger_id", chargerID),
		registry:     m.registry,
	}

	m.sessions[chargerID] = sess
	m.registry.Touch(chargerID)

	return old
}

func (m *Manager) Get(chargerID string) (*Session, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	sess := m.sessions[chargerID]
	return sess, sess != nil
}

func (m *Manager) Remove(chargerID string, sess *Session) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.sessions[chargerID] == sess {
		delete(m.sessions, chargerID)
	}
}

func (m *Manager) Call(ctx context.Context, chargerID string, action string, payload any) (json.RawMessage, error) {
	sess, ok := m.Get(chargerID)
	if !ok {
		return nil, fmt.Errorf("charger %s not connected", chargerID)
	}

	return sess.Call(ctx, action, payload)
}

type Session struct {
	ChargerID    string
	SerialNumber string

	conn     *websocket.Conn
	outgoing chan outgoingFrame

	mu      sync.Mutex
	pending map[string]pendingResponse
	closed  bool

	ctx    context.Context
	cancel context.CancelFunc

	logger   *slog.Logger
	registry *state.Registry
}

func (s *Session) run(handler InboundHandler) {
	writeDone := make(chan struct{})

	go func() {
		defer close(writeDone)
		s.writePump()
	}()

	s.readPump(handler)

	s.Close("read pump ended")
	<-writeDone
}

func (s *Session) readPump(handler InboundHandler) {
	for {
		messageType, raw, err := s.conn.ReadMessage()
		if err != nil {
			s.logger.Info("charger read loop ended", "error", err)
			return
		}

		s.registry.Touch(s.ChargerID)

		if messageType != websocket.TextMessage {
			continue
		}

		msgType, err := ocpp.MessageType(raw)
		if err != nil {
			s.logger.Warn("invalid OCPP frame", "error", err)
			continue
		}

		switch msgType {
		case ocpp.MessageTypeCall:
			call, err := ocpp.ParseCall(raw)
			if err != nil {
				s.logger.Warn("invalid OCPP CALL frame", "error", err)
				continue
			}

			handler(s, s.ChargerID, call)

		case ocpp.MessageTypeCallResult:
			result, err := ocpp.ParseCallResult(raw)
			if err != nil {
				s.logger.Warn("invalid OCPP CALLRESULT frame", "error", err)
				continue
			}

			s.resolvePending(result.UniqueID, Response{
				Payload: result.Payload,
			})

		case ocpp.MessageTypeCallError:
			callErr, err := ocpp.ParseCallError(raw)
			if err != nil {
				s.logger.Warn("invalid OCPP CALLERROR frame", "error", err)
				continue
			}

			s.resolvePending(callErr.UniqueID, Response{
				ErrorCode:    callErr.ErrorCode,
				Description:  callErr.Description,
				ErrorDetails: callErr.Details,
			})

		default:
			s.logger.Warn("unknown OCPP message type", "message_type", msgType)
		}
	}
}

func (s *Session) writePump() {
	for {
		select {
		case <-s.ctx.Done():
			return

		case frame, ok := <-s.outgoing:
			if !ok {
				return
			}

			if err := s.conn.WriteMessage(websocket.TextMessage, frame.data); err != nil {
				s.logger.Warn("charger write failed", "error", err)
				s.Close("write failed")
				return
			}
		}
	}
}

func (s *Session) Call(ctx context.Context, action string, payload any) (json.RawMessage, error) {
	uniqueID := newUniqueID()

	frame, err := ocpp.CallFrame(uniqueID, action, payload)
	if err != nil {
		return nil, err
	}

	ch := make(chan Response, 1)

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil, ErrSessionClosed
	}
	s.pending[uniqueID] = pendingResponse{ch: ch}
	s.mu.Unlock()

	defer s.deletePending(uniqueID)

	select {
	case s.outgoing <- outgoingFrame{data: frame}:
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-s.ctx.Done():
		return nil, ErrSessionClosed
	}

	select {
	case resp := <-ch:
		if resp.ErrorCode != "" {
			return nil, fmt.Errorf("%w: %s: %s", ErrCallError, resp.ErrorCode, resp.Description)
		}
		return resp.Payload, nil

	case <-ctx.Done():
		return nil, ctx.Err()

	case <-s.ctx.Done():
		return nil, ErrSessionClosed
	}
}

func (s *Session) WriteCallResult(uniqueID string, payload any) {
	data, err := ocpp.CallResult(uniqueID, payload)
	if err != nil {
		s.logger.Error("failed to encode OCPP CallResult", "unique_id", uniqueID, "error", err)
		return
	}

	s.enqueue(data)
}

func (s *Session) WriteCallError(uniqueID string, code string, description string) {
	data, err := ocpp.CallError(uniqueID, code, description, map[string]any{})
	if err != nil {
		s.logger.Error("failed to encode OCPP CallError", "unique_id", uniqueID, "error", err)
		return
	}

	s.enqueue(data)
}

func (s *Session) enqueue(data []byte) {
	select {
	case s.outgoing <- outgoingFrame{data: data}:
	case <-s.ctx.Done():
	}
}

func (s *Session) resolvePending(uniqueID string, resp Response) {
	s.mu.Lock()
	pending, ok := s.pending[uniqueID]
	if ok {
		delete(s.pending, uniqueID)
	}
	s.mu.Unlock()

	if ok {
		pending.ch <- resp
	}
}

func (s *Session) deletePending(uniqueID string) {
	s.mu.Lock()
	delete(s.pending, uniqueID)
	s.mu.Unlock()
}

func (s *Session) Close(reason string) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	s.mu.Unlock()

	s.logger.Info("closing charger session", "reason", reason)

	s.cancel()
	_ = s.conn.Close()

	s.mu.Lock()
	for uniqueID, pending := range s.pending {
		delete(s.pending, uniqueID)
		close(pending.ch)
	}
	s.mu.Unlock()
}

func newUniqueID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err == nil {
		return hex.EncodeToString(b[:])
	}

	return fmt.Sprintf("%d", time.Now().UnixNano())
}
