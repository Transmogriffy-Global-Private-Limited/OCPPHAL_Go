package chargerdir

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Transmogriffy-Global-Private-Limited/OCPPHAL_Go/internal/config"
)

type Service struct {
	cfg    config.Config
	logger *slog.Logger
	client *http.Client

	mu        sync.Mutex
	cachedIDs map[string]struct{}
	expiresAt time.Time
}

func NewService(cfg config.Config, logger *slog.Logger) *Service {
	return &Service{
		cfg:       cfg,
		logger:    logger,
		client:    &http.Client{Timeout: 10 * time.Second},
		cachedIDs: map[string]struct{}{},
	}
}

func (s *Service) Enabled() bool {
	return strings.TrimSpace(s.cfg.ChargerDataURL) != ""
}

func (s *Service) IsKnownCharger(ctx context.Context, chargerID string) (bool, error) {
	chargerID = strings.TrimSpace(chargerID)
	if chargerID == "" {
		return false, nil
	}

	if !s.Enabled() {
		return true, nil
	}

	ids, err := s.getIDs(ctx)
	if err != nil {
		return false, err
	}

	_, ok := ids[chargerID]
	return ok, nil
}

func (s *Service) KnownChargerIDs(ctx context.Context) ([]string, error) {
	if !s.Enabled() {
		return nil, nil
	}

	ids, err := s.getIDs(ctx)
	if err != nil {
		return nil, err
	}

	out := make([]string, 0, len(ids))
	for id := range ids {
		out = append(out, id)
	}

	sort.Strings(out)
	return out, nil
}

func (s *Service) Refresh(ctx context.Context) error {
	_, err := s.fetchAndCache(ctx)
	return err
}

func (s *Service) getIDs(ctx context.Context) (map[string]struct{}, error) {
	s.mu.Lock()

	if time.Now().Before(s.expiresAt) && s.cachedIDs != nil {
		copyIDs := cloneIDSet(s.cachedIDs)
		s.mu.Unlock()
		return copyIDs, nil
	}

	s.mu.Unlock()

	return s.fetchAndCache(ctx)
}

func (s *Service) fetchAndCache(ctx context.Context) (map[string]struct{}, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.cfg.ChargerDataURL, nil)
	if err != nil {
		return nil, err
	}

	if s.cfg.APIAuthKey != "" {
		req.Header.Set("apiauthkey", s.cfg.APIAuthKey)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("charger data endpoint returned HTTP %d", resp.StatusCode)
	}

	var raw any
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}

	ids := extractIDs(raw)

	s.mu.Lock()
	s.cachedIDs = ids
	s.expiresAt = time.Now().Add(time.Duration(s.cfg.ChargerDataCacheTTLSeconds) * time.Second)
	s.mu.Unlock()

	s.logger.Info("charger directory refreshed", "count", len(ids), "ttl_seconds", s.cfg.ChargerDataCacheTTLSeconds)

	return cloneIDSet(ids), nil
}

func extractIDs(raw any) map[string]struct{} {
	ids := map[string]struct{}{}

	var walk func(any)
	walk = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			if uidRaw, ok := x["uid"]; ok {
				if uid := strings.TrimSpace(fmt.Sprint(uidRaw)); uid != "" {
					ids[uid] = struct{}{}
				}
			}

			for key, value := range x {
				if key == "data" || key == "chargers" || key == "charger_data" || key == "user_chargerunit_details" {
					walk(value)
				}
			}

		case []any:
			for _, item := range x {
				walk(item)
			}
		}
	}

	walk(raw)
	return ids
}

func cloneIDSet(in map[string]struct{}) map[string]struct{} {
	out := make(map[string]struct{}, len(in))
	for key := range in {
		out[key] = struct{}{}
	}
	return out
}
