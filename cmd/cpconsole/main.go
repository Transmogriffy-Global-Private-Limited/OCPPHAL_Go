package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	ocpp16 "github.com/lorenzodonini/ocpp-go/ocpp1.6"
	"github.com/lorenzodonini/ocpp-go/ocpp1.6/core"
	"github.com/lorenzodonini/ocpp-go/ocpp1.6/firmware"
	"github.com/lorenzodonini/ocpp-go/ocpp1.6/remotetrigger"
	"github.com/lorenzodonini/ocpp-go/ocpp1.6/types"
)

const helpText = `Commands:
  help                              show this help
  state                             show connection, connector and transaction state
  heartbeat                         send Heartbeat
  plug                              report Preparing
  unplug                            report Available (only when no transaction is active)
  authorize <id-tag>                send Authorize
  start <id-tag>                    start a local transaction on the configured connector
  remote-start                      execute a pending accepted RemoteStartTransaction
  tick <seconds> [power-kW]         advance the cumulative meter and send MeterValues
  meter                             resend the current meter sample without adding energy
  auto <seconds> <power-kW>         periodically tick while charging
  auto off                          stop periodic metering
  suspend ev|evse                   report SuspendedEV or SuspendedEVSE
  resume                            report Charging for an active transaction
  stop [reason]                     stop locally; reason defaults to Local
  remote-stop                       show that accepted remote stops complete automatically
  finish                            report Available after a stopped, still-plugged session
  fault <error-code>                report Faulted using an OCPP 1.6 error code
  clear-fault                       return to Preparing or Available
  status <ocpp-status>              send an explicit valid StatusNotification
  policy remote-start accept|reject set response policy for future remote starts
  policy remote-stop accept|reject  set response policy for future remote stops
  policy auto-remote on|off         automatically execute accepted remote starts
  quit                              stop the simulator

Stop reasons: DeAuthorized, EmergencyStop, EVDisconnected, HardReset, Local,
Other, PowerLoss, Reboot, Remote, SoftReset, UnlockCommand.
Fault codes: ConnectorLockFailure, EVCommunicationError, GroundFailure,
HighTemperature, InternalError, LocalListConflict, OtherError,
OverCurrentFailure, OverVoltage, PowerMeterFailure, PowerSwitchFailure,
ReaderFailure, ResetFailure, UnderVoltage, WeakSignal.`

type simulator struct {
	cp          ocpp16.ChargePoint
	clientID    string
	model       string
	vendor      string
	connectorID int

	opsMu sync.Mutex
	mu    sync.RWMutex

	booted               bool
	status               core.ChargePointStatus
	errorCode            core.ChargePointErrorCode
	plugged              bool
	idTag                string
	transaction          int
	meterWh              float64
	powerKW              float64
	voltage              float64
	soc                  float64
	remoteStart          *core.RemoteStartTransactionRequest
	stoppingTransaction  int
	acceptStart          bool
	acceptStop           bool
	autoRemote           bool
	unavailableAfterStop bool
	autoCancel           chan struct{}
	configuration        map[string]string
	startTransactionFunc func(connectorID int, idTag string, meterStart int) (*core.StartTransactionConfirmation, error)
	stopTransactionFunc  func(meterStop, transactionID int, idTag string, reason core.Reason) (*core.StopTransactionConfirmation, error)
	statusFunc           func(status core.ChargePointStatus, code core.ChargePointErrorCode) error
}

func newSimulator(clientID, model, vendor string, connectorID int, meterStartWh, voltage, soc float64) *simulator {
	s := &simulator{
		clientID:      clientID,
		model:         model,
		vendor:        vendor,
		connectorID:   connectorID,
		status:        core.ChargePointStatusUnavailable,
		errorCode:     core.NoError,
		meterWh:       meterStartWh,
		voltage:       voltage,
		soc:           soc,
		acceptStart:   true,
		acceptStop:    true,
		autoRemote:    true,
		configuration: map[string]string{"HeartbeatInterval": "900", "MeterValueSampleInterval": "60"},
	}
	s.cp = ocpp16.NewChargePoint(clientID, nil, nil)
	s.cp.SetCoreHandler(s)
	s.cp.SetFirmwareManagementHandler(s)
	s.cp.SetRemoteTriggerHandler(s)
	return s
}

func main() {
	centralURL := flag.String("url", env("CP_SIM_URL", "ws://127.0.0.1:18081"), "central-system WebSocket base URL")
	clientID := flag.String("id", env("CP_SIM_ID", "CP-SIM-001"), "OCPP charge point identity")
	model := flag.String("model", env("CP_SIM_MODEL", "TransEV-Simulator"), "BootNotification chargePointModel")
	vendor := flag.String("vendor", env("CP_SIM_VENDOR", "TransEV"), "BootNotification chargePointVendor")
	connector := flag.Int("connector", envInt("CP_SIM_CONNECTOR", 1), "connector ID")
	meterStart := flag.Float64("meter-start-wh", envFloat("CP_SIM_METER_START_WH", 100000), "initial cumulative energy register in Wh")
	voltage := flag.Float64("voltage", envFloat("CP_SIM_VOLTAGE", 230), "simulated voltage in V")
	soc := flag.Float64("soc", envFloat("CP_SIM_SOC", 35), "initial state of charge percentage")
	flag.Parse()

	if *connector < 1 || *meterStart < 0 || *voltage <= 0 || *soc < 0 || *soc > 100 {
		log.Fatal("connector must be >= 1, meter/voltage must be valid, and SoC must be between 0 and 100")
	}
	if strings.TrimSpace(*clientID) == "" || strings.TrimSpace(*model) == "" || len(*model) > 20 || strings.TrimSpace(*vendor) == "" || len(*vendor) > 20 {
		log.Fatal("charger ID must be non-empty; OCPP model and vendor must contain 1 to 20 characters")
	}
	normalizedURL, err := normalizeWebSocketURL(*centralURL)
	if err != nil {
		log.Fatal(err)
	}

	sim := newSimulator(*clientID, *model, *vendor, *connector, *meterStart, *voltage, *soc)
	if err := sim.connectAndBoot(normalizedURL); err != nil {
		log.Fatal(err)
	}
	defer sim.close()

	fmt.Printf("\nOCPP 1.6J software charger %s is ready. Type help for commands.\n", *clientID)
	if err := runConsole(os.Stdin, os.Stdout, sim); err != nil {
		log.Fatal(err)
	}
}

func (s *simulator) connectAndBoot(url string) error {
	if err := s.cp.Start(url); err != nil {
		return fmt.Errorf("connect %s to %s: %w", s.clientID, url, err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for !s.cp.IsConnected() && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	if !s.cp.IsConnected() {
		return errors.New("charge point connection did not become ready within 10s")
	}
	if err := s.boot(); err != nil {
		return err
	}
	return s.setStatus(core.ChargePointStatusAvailable, core.NoError)
}

func (s *simulator) close() {
	s.stopAuto()
	s.cp.Stop()
}

func (s *simulator) boot() error {
	conf, err := s.cp.BootNotification(s.model, s.vendor)
	if err != nil {
		return fmt.Errorf("BootNotification: %w", err)
	}
	if conf == nil || conf.Status != core.RegistrationStatusAccepted {
		status := "nil"
		if conf != nil {
			status = string(conf.Status)
		}
		return fmt.Errorf("BootNotification was not accepted: %s", status)
	}
	s.mu.Lock()
	s.booted = true
	s.mu.Unlock()
	fmt.Printf("[OCPP] BootNotification accepted; heartbeat interval=%ds\n", conf.Interval)
	return nil
}

func runConsole(in io.Reader, out io.Writer, sim *simulator) error {
	scanner := bufio.NewScanner(in)
	for {
		fmt.Fprint(out, "cp> ")
		if !scanner.Scan() {
			return scanner.Err()
		}
		quit, err := sim.execute(strings.Fields(scanner.Text()))
		if err != nil {
			fmt.Fprintf(out, "error: %v\n", err)
		}
		if quit {
			return nil
		}
	}
}

func (s *simulator) execute(args []string) (bool, error) {
	if len(args) == 0 {
		return false, nil
	}
	switch strings.ToLower(args[0]) {
	case "help", "?":
		fmt.Println(helpText)
	case "state":
		s.printState()
	case "heartbeat":
		return false, s.heartbeat()
	case "plug":
		return false, s.plug()
	case "unplug":
		return false, s.unplug()
	case "authorize":
		if len(args) != 2 {
			return false, errors.New("usage: authorize <id-tag>")
		}
		return false, s.authorize(args[1])
	case "start":
		if len(args) != 2 {
			return false, errors.New("usage: start <id-tag>")
		}
		return false, s.startTransaction(args[1], false)
	case "remote-start":
		return false, s.executePendingRemoteStart()
	case "tick":
		return false, s.executeTick(args)
	case "meter":
		return false, s.sendMeter(0, s.currentPower())
	case "auto":
		return false, s.executeAuto(args)
	case "suspend":
		if len(args) != 2 {
			return false, errors.New("usage: suspend ev|evse")
		}
		if strings.EqualFold(args[1], "ev") {
			return false, s.requireTransactionStatus(core.ChargePointStatusSuspendedEV)
		}
		if strings.EqualFold(args[1], "evse") {
			return false, s.requireTransactionStatus(core.ChargePointStatusSuspendedEVSE)
		}
		return false, errors.New("usage: suspend ev|evse")
	case "resume":
		return false, s.requireTransactionStatus(core.ChargePointStatusCharging)
	case "stop":
		reason := core.ReasonLocal
		if len(args) > 2 {
			return false, errors.New("usage: stop [reason]")
		}
		if len(args) == 2 {
			parsed, ok := parseStopReason(args[1])
			if !ok {
				return false, fmt.Errorf("invalid stop reason %q", args[1])
			}
			reason = parsed
		}
		return false, s.stopTransaction(reason)
	case "remote-stop":
		return false, s.executePendingRemoteStop()
	case "finish":
		if s.hasTransaction() {
			return false, errors.New("stop the active transaction before finish")
		}
		return false, s.setStatus(core.ChargePointStatusAvailable, core.NoError)
	case "fault":
		if len(args) != 2 {
			return false, errors.New("usage: fault <error-code>")
		}
		code, ok := parseErrorCode(args[1])
		if !ok || code == core.NoError {
			return false, fmt.Errorf("invalid fault code %q", args[1])
		}
		return false, s.setStatus(core.ChargePointStatusFaulted, code)
	case "clear-fault":
		s.mu.RLock()
		plugged := s.plugged
		s.mu.RUnlock()
		if plugged {
			return false, s.setStatus(core.ChargePointStatusPreparing, core.NoError)
		}
		return false, s.setStatus(core.ChargePointStatusAvailable, core.NoError)
	case "status":
		if len(args) != 2 {
			return false, errors.New("usage: status <ocpp-status>")
		}
		status, ok := parseStatus(args[1])
		if !ok {
			return false, fmt.Errorf("invalid OCPP status %q", args[1])
		}
		return false, s.setStatus(status, core.NoError)
	case "policy":
		return false, s.executePolicy(args)
	case "quit", "exit":
		return true, nil
	default:
		return false, fmt.Errorf("unknown command %q; type help", args[0])
	}
	return false, nil
}

func (s *simulator) executeTick(args []string) error {
	if len(args) < 2 || len(args) > 3 {
		return errors.New("usage: tick <seconds> [power-kW]")
	}
	seconds, err := strconv.ParseFloat(args[1], 64)
	if err != nil || seconds <= 0 {
		return errors.New("seconds must be greater than zero")
	}
	power := s.currentPower()
	if len(args) == 3 {
		power, err = strconv.ParseFloat(args[2], 64)
		if err != nil || power < 0 {
			return errors.New("power-kW must be zero or greater")
		}
	}
	if power == 0 {
		power = 7.2
	}
	return s.sendMeter(time.Duration(seconds*float64(time.Second)), power)
}

func (s *simulator) executeAuto(args []string) error {
	if len(args) == 2 && strings.EqualFold(args[1], "off") {
		s.stopAuto()
		fmt.Println("[SIM] automatic metering stopped")
		return nil
	}
	if len(args) != 3 {
		return errors.New("usage: auto <seconds> <power-kW> | auto off")
	}
	seconds, err := strconv.ParseFloat(args[1], 64)
	if err != nil || seconds <= 0 {
		return errors.New("seconds must be greater than zero")
	}
	power, err := strconv.ParseFloat(args[2], 64)
	if err != nil || power <= 0 {
		return errors.New("power-kW must be greater than zero")
	}
	return s.startAuto(time.Duration(seconds*float64(time.Second)), power)
}

func (s *simulator) executePolicy(args []string) error {
	if len(args) != 3 {
		return errors.New("usage: policy remote-start accept|reject | policy remote-stop accept|reject | policy auto-remote on|off")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	switch strings.ToLower(args[1]) {
	case "remote-start":
		value, ok := parseBoolPolicy(args[2], "accept", "reject")
		if !ok {
			return errors.New("remote-start policy must be accept or reject")
		}
		s.acceptStart = value
	case "remote-stop":
		value, ok := parseBoolPolicy(args[2], "accept", "reject")
		if !ok {
			return errors.New("remote-stop policy must be accept or reject")
		}
		s.acceptStop = value
	case "auto-remote":
		value, ok := parseBoolPolicy(args[2], "on", "off")
		if !ok {
			return errors.New("auto-remote policy must be on or off")
		}
		s.autoRemote = value
	default:
		return fmt.Errorf("unknown policy %q", args[1])
	}
	fmt.Printf("[SIM] policy %s=%s\n", args[1], strings.ToLower(args[2]))
	return nil
}

func (s *simulator) heartbeat() error {
	conf, err := s.cp.Heartbeat()
	if err != nil {
		return fmt.Errorf("Heartbeat: %w", err)
	}
	fmt.Printf("[OCPP] Heartbeat accepted; central time=%s\n", conf.CurrentTime.String())
	return nil
}

func (s *simulator) plug() error {
	s.mu.Lock()
	if s.transaction != 0 {
		s.mu.Unlock()
		return errors.New("connector already has an active transaction")
	}
	s.plugged = true
	s.mu.Unlock()
	return s.setStatus(core.ChargePointStatusPreparing, core.NoError)
}

func (s *simulator) unplug() error {
	s.mu.Lock()
	if s.transaction != 0 {
		s.mu.Unlock()
		return errors.New("stop the transaction before unplugging")
	}
	s.plugged = false
	s.mu.Unlock()
	return s.setStatus(core.ChargePointStatusAvailable, core.NoError)
}

func (s *simulator) authorize(idTag string) error {
	if idTag == "" || len(idTag) > 20 {
		return errors.New("id-tag must contain 1 to 20 characters")
	}
	conf, err := s.cp.Authorize(idTag)
	if err != nil {
		return fmt.Errorf("Authorize: %w", err)
	}
	fmt.Printf("[OCPP] Authorize idTag=%s status=%s\n", idTag, conf.IdTagInfo.Status)
	if conf.IdTagInfo.Status != types.AuthorizationStatusAccepted {
		return fmt.Errorf("id-tag was not accepted: %s", conf.IdTagInfo.Status)
	}
	return nil
}

func (s *simulator) startTransaction(idTag string, remote bool) error {
	s.opsMu.Lock()
	defer s.opsMu.Unlock()
	if idTag == "" || len(idTag) > 20 {
		return errors.New("id-tag must contain 1 to 20 characters")
	}
	s.mu.RLock()
	active := s.transaction != 0
	status := s.status
	meter := s.meterWh
	s.mu.RUnlock()
	if active {
		return errors.New("a transaction is already active")
	}
	if status == core.ChargePointStatusFaulted || status == core.ChargePointStatusUnavailable {
		return fmt.Errorf("cannot start while connector status is %s", status)
	}
	if !remote {
		if err := s.authorize(idTag); err != nil {
			return err
		}
	}
	if status == core.ChargePointStatusAvailable {
		s.mu.Lock()
		s.plugged = true
		s.mu.Unlock()
		if err := s.setStatus(core.ChargePointStatusPreparing, core.NoError); err != nil {
			return err
		}
	}
	conf, err := s.sendStartTransaction(s.connectorID, idTag, int(meter))
	if err != nil {
		return fmt.Errorf("StartTransaction: %w", err)
	}
	if conf == nil || conf.IdTagInfo.Status != types.AuthorizationStatusAccepted || conf.TransactionId <= 0 {
		status := "nil"
		if conf != nil {
			status = string(conf.IdTagInfo.Status)
		}
		return fmt.Errorf("StartTransaction was not accepted: %s", status)
	}
	s.mu.Lock()
	s.transaction = conf.TransactionId
	s.idTag = idTag
	s.remoteStart = nil
	s.mu.Unlock()
	if err := s.setStatus(core.ChargePointStatusCharging, core.NoError); err != nil {
		return err
	}
	fmt.Printf("[OCPP] StartTransaction accepted; transactionId=%d meterStart=%.0fWh idTag=%s\n", conf.TransactionId, meter, idTag)
	return nil
}

func (s *simulator) sendMeter(elapsed time.Duration, powerKW float64) error {
	s.opsMu.Lock()
	defer s.opsMu.Unlock()
	s.mu.Lock()
	if s.transaction == 0 {
		s.mu.Unlock()
		return errors.New("meter values require an active transaction")
	}
	if elapsed < 0 || powerKW < 0 {
		s.mu.Unlock()
		return errors.New("elapsed time and power cannot be negative")
	}
	deltaWh := powerKW * 1000 * elapsed.Hours()
	s.meterWh += deltaWh
	s.powerKW = powerKW
	if deltaWh > 0 && s.soc < 100 {
		// A 60 kWh reference battery keeps SoC progression coherent with energy.
		s.soc += deltaWh / 60000 * 100
		if s.soc > 100 {
			s.soc = 100
		}
	}
	meterWh := s.meterWh
	transaction := s.transaction
	voltage := s.voltage
	soc := s.soc
	connector := s.connectorID
	s.mu.Unlock()

	current := 0.0
	if voltage > 0 {
		current = powerKW * 1000 / voltage
	}
	values := []types.MeterValue{{
		Timestamp: types.Now(),
		SampledValue: []types.SampledValue{
			{Value: fmt.Sprintf("%.0f", meterWh), Context: types.ReadingContextSamplePeriodic, Measurand: types.MeasurandEnergyActiveImportRegister, Unit: types.UnitOfMeasureWh},
			{Value: fmt.Sprintf("%.3f", powerKW), Context: types.ReadingContextSamplePeriodic, Measurand: types.MeasurandPowerActiveImport, Unit: types.UnitOfMeasureKW},
			{Value: fmt.Sprintf("%.2f", current), Context: types.ReadingContextSamplePeriodic, Measurand: types.MeasurandCurrentImport, Unit: types.UnitOfMeasureA},
			{Value: fmt.Sprintf("%.1f", voltage), Context: types.ReadingContextSamplePeriodic, Measurand: types.MeasurandVoltage, Unit: types.UnitOfMeasureV},
			{Value: fmt.Sprintf("%.1f", soc), Context: types.ReadingContextSamplePeriodic, Measurand: types.MeasurandSoC, Unit: types.UnitOfMeasurePercent},
		},
	}}
	_, err := s.cp.MeterValues(connector, values, func(request *core.MeterValuesRequest) {
		request.TransactionId = &transaction
	})
	if err != nil {
		return fmt.Errorf("MeterValues: %w", err)
	}
	fmt.Printf("[OCPP] MeterValues transactionId=%d meter=%.0fWh (+%.1fWh) power=%.3fkW current=%.2fA voltage=%.1fV soc=%.1f%%\n", transaction, meterWh, deltaWh, powerKW, current, voltage, soc)
	return nil
}

func (s *simulator) stopTransaction(reason core.Reason) error {
	s.opsMu.Lock()
	defer s.opsMu.Unlock()
	return s.stopTransactionLocked(reason, 0)
}

// stopTransactionLocked finalizes either a local stop or the one remote stop
// already claimed by OnRemoteStopTransaction. opsMu serializes every sender so
// an accepted remote command can never emit a second StopTransaction.
func (s *simulator) stopTransactionLocked(reason core.Reason, expectedTransaction int) error {
	s.stopAuto()
	s.mu.Lock()
	transaction := s.transaction
	if transaction == 0 {
		s.mu.Unlock()
		return errors.New("no active transaction")
	}
	if expectedTransaction != 0 && transaction != expectedTransaction {
		s.mu.Unlock()
		return fmt.Errorf("active transaction %d does not match remote stop %d", transaction, expectedTransaction)
	}
	if expectedTransaction == 0 && s.stoppingTransaction != 0 {
		s.mu.Unlock()
		return errors.New("an accepted remote stop is already completing")
	}
	if s.stoppingTransaction != 0 && s.stoppingTransaction != transaction {
		s.mu.Unlock()
		return errors.New("another transaction is stopping")
	}
	s.stoppingTransaction = transaction
	meter := s.meterWh
	idTag := s.idTag
	s.mu.Unlock()

	conf, err := s.sendStopTransaction(int(meter), transaction, idTag, reason)
	if err != nil {
		return fmt.Errorf("StopTransaction: %w", err)
	}
	status := "Accepted"
	if conf != nil && conf.IdTagInfo != nil {
		status = string(conf.IdTagInfo.Status)
	}
	s.mu.Lock()
	s.transaction = 0
	s.idTag = ""
	s.powerKW = 0
	s.stoppingTransaction = 0
	s.plugged = false // A software charger has no physical cable to await.
	s.mu.Unlock()
	s.mu.RLock()
	unavailable := s.unavailableAfterStop
	s.mu.RUnlock()
	if unavailable {
		if err := s.setStatus(core.ChargePointStatusUnavailable, core.NoError); err != nil {
			return err
		}
	} else {
		// Finishing is observable but never terminal in this simulator: there is
		// no physical unplug action that can advance it.
		if err := s.setStatus(core.ChargePointStatusFinishing, core.NoError); err != nil {
			fmt.Printf("[SIM] Finishing status failed after StopTransaction: %v; continuing to Available\n", err)
		}
		if err := s.reportAvailableAfterStop(); err != nil {
			return err
		}
	}
	fmt.Printf("[OCPP] StopTransaction status=%s transactionId=%d meterStop=%.0fWh reason=%s\n", status, transaction, meter, reason)
	return nil
}

func (s *simulator) sendStartTransaction(connectorID int, idTag string, meterStart int) (*core.StartTransactionConfirmation, error) {
	if s.startTransactionFunc != nil {
		return s.startTransactionFunc(connectorID, idTag, meterStart)
	}
	return s.cp.StartTransaction(connectorID, idTag, meterStart, types.Now())
}

func (s *simulator) reportAvailableAfterStop() error {
	// A completed dummy transaction must not remain in Finishing merely because
	// the first status request races a transient transport problem. Status
	// notifications are state reports, so retrying Available cannot duplicate a
	// transaction or its business side effects.
	for attempt := 1; ; attempt++ {
		if err := s.setStatus(core.ChargePointStatusAvailable, core.NoError); err == nil {
			return nil
		} else if s.cp == nil || !s.cp.IsConnected() || attempt == 3 {
			s.mu.Lock()
			s.status = core.ChargePointStatusAvailable
			s.errorCode = core.NoError
			s.mu.Unlock()
			fmt.Printf("[SIM] Available notification failed after completed stop: %v; local simulator state is Available and startup will announce it on reconnect\n", err)
			return err
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func (s *simulator) sendStopTransaction(meterStop, transactionID int, idTag string, reason core.Reason) (*core.StopTransactionConfirmation, error) {
	if s.stopTransactionFunc != nil {
		return s.stopTransactionFunc(meterStop, transactionID, idTag, reason)
	}
	return s.cp.StopTransaction(meterStop, types.Now(), transactionID, func(request *core.StopTransactionRequest) {
		request.IdTag = idTag
		request.Reason = reason
	})
}

func (s *simulator) setStatus(status core.ChargePointStatus, code core.ChargePointErrorCode) error {
	var err error
	if s.statusFunc != nil {
		err = s.statusFunc(status, code)
	} else {
		_, err = s.cp.StatusNotification(s.connectorID, code, status, func(request *core.StatusNotificationRequest) {
			request.Timestamp = types.Now()
		})
	}
	if err != nil {
		return fmt.Errorf("StatusNotification %s: %w", status, err)
	}
	s.mu.Lock()
	s.status = status
	s.errorCode = code
	s.mu.Unlock()
	fmt.Printf("[OCPP] StatusNotification connector=%d status=%s errorCode=%s\n", s.connectorID, status, code)
	return nil
}

func (s *simulator) requireTransactionStatus(status core.ChargePointStatus) error {
	if !s.hasTransaction() {
		return errors.New("this status requires an active transaction")
	}
	return s.setStatus(status, core.NoError)
}

func (s *simulator) startAuto(interval time.Duration, powerKW float64) error {
	if !s.hasTransaction() {
		return errors.New("automatic metering requires an active transaction")
	}
	s.stopAuto()
	cancel := make(chan struct{})
	s.mu.Lock()
	s.autoCancel = cancel
	s.mu.Unlock()
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if err := s.sendMeter(interval, powerKW); err != nil {
					fmt.Printf("[SIM] automatic meter stopped: %v\n", err)
					return
				}
			case <-cancel:
				return
			}
		}
	}()
	fmt.Printf("[SIM] automatic metering every %s at %.3fkW\n", interval, powerKW)
	return nil
}

func (s *simulator) stopAuto() {
	s.mu.Lock()
	if s.autoCancel != nil {
		close(s.autoCancel)
		s.autoCancel = nil
	}
	s.mu.Unlock()
}

func (s *simulator) executePendingRemoteStart() error {
	s.mu.RLock()
	request := s.remoteStart
	s.mu.RUnlock()
	if request == nil {
		return errors.New("no accepted remote start is pending")
	}
	connector := s.connectorID
	if request.ConnectorId != nil {
		connector = *request.ConnectorId
	}
	if connector != s.connectorID {
		return fmt.Errorf("remote start requested connector %d, simulator owns connector %d", connector, s.connectorID)
	}
	return s.startTransaction(request.IdTag, true)
}

func (s *simulator) executePendingRemoteStop() error {
	return errors.New("accepted remote stops complete automatically")
}

func (s *simulator) printState() {
	s.mu.RLock()
	defer s.mu.RUnlock()
	fmt.Printf("connected=%t booted=%t id=%s connector=%d status=%s error=%s plugged=%t\n", s.cp.IsConnected(), s.booted, s.clientID, s.connectorID, s.status, s.errorCode, s.plugged)
	fmt.Printf("transactionId=%d stoppingTransactionId=%d idTag=%q meter=%.0fWh power=%.3fkW voltage=%.1fV soc=%.1f%% pendingRemoteStart=%t autoRemote=%t\n", s.transaction, s.stoppingTransaction, s.idTag, s.meterWh, s.powerKW, s.voltage, s.soc, s.remoteStart != nil, s.autoRemote)
}

func (s *simulator) hasTransaction() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.transaction != 0
}

func (s *simulator) currentPower() float64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.powerKW
}

// OCPP Core profile: Central System -> Charge Point handlers.

func (s *simulator) OnChangeAvailability(request *core.ChangeAvailabilityRequest) (*core.ChangeAvailabilityConfirmation, error) {
	fmt.Printf("[REMOTE] ChangeAvailability connector=%d type=%s\n", request.ConnectorId, request.Type)
	if request.ConnectorId != 0 && request.ConnectorId != s.connectorID {
		return core.NewChangeAvailabilityConfirmation(core.AvailabilityStatusRejected), nil
	}
	if request.Type == core.AvailabilityTypeInoperative && s.hasTransaction() {
		s.mu.Lock()
		s.unavailableAfterStop = true
		s.mu.Unlock()
		return core.NewChangeAvailabilityConfirmation(core.AvailabilityStatusScheduled), nil
	}
	target := core.ChargePointStatusAvailable
	if request.Type == core.AvailabilityTypeInoperative {
		target = core.ChargePointStatusUnavailable
	}
	s.mu.Lock()
	s.unavailableAfterStop = request.Type == core.AvailabilityTypeInoperative
	s.mu.Unlock()
	go func() { _ = s.setStatus(target, core.NoError) }()
	return core.NewChangeAvailabilityConfirmation(core.AvailabilityStatusAccepted), nil
}

func (s *simulator) OnChangeConfiguration(request *core.ChangeConfigurationRequest) (*core.ChangeConfigurationConfirmation, error) {
	fmt.Printf("[REMOTE] ChangeConfiguration key=%s value=%s\n", request.Key, request.Value)
	s.mu.Lock()
	s.configuration[request.Key] = request.Value
	s.mu.Unlock()
	return core.NewChangeConfigurationConfirmation(core.ConfigurationStatusAccepted), nil
}

func (s *simulator) OnClearCache(*core.ClearCacheRequest) (*core.ClearCacheConfirmation, error) {
	fmt.Println("[REMOTE] ClearCache")
	return core.NewClearCacheConfirmation(core.ClearCacheStatusAccepted), nil
}

func (s *simulator) OnDataTransfer(*core.DataTransferRequest) (*core.DataTransferConfirmation, error) {
	return core.NewDataTransferConfirmation(core.DataTransferStatusAccepted), nil
}

func (s *simulator) OnGetConfiguration(request *core.GetConfigurationRequest) (*core.GetConfigurationConfirmation, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	keys := request.Key
	if len(keys) == 0 {
		keys = make([]string, 0, len(s.configuration))
		for key := range s.configuration {
			keys = append(keys, key)
		}
	}
	known := make([]core.ConfigurationKey, 0, len(keys))
	unknown := make([]string, 0)
	for _, key := range keys {
		value, ok := s.configuration[key]
		if !ok {
			unknown = append(unknown, key)
			continue
		}
		v := value
		known = append(known, core.ConfigurationKey{Key: key, Value: &v})
	}
	conf := core.NewGetConfigurationConfirmation(known)
	conf.UnknownKey = unknown
	return conf, nil
}

func (s *simulator) OnRemoteStartTransaction(request *core.RemoteStartTransactionRequest) (*core.RemoteStartTransactionConfirmation, error) {
	s.mu.Lock()
	accepted := s.acceptStart && s.transaction == 0 && s.status != core.ChargePointStatusFaulted && s.status != core.ChargePointStatusUnavailable
	if accepted {
		s.remoteStart = request
	}
	auto := s.autoRemote
	s.mu.Unlock()
	status := types.RemoteStartStopStatusRejected
	if accepted {
		status = types.RemoteStartStopStatusAccepted
	}
	fmt.Printf("[REMOTE] RemoteStartTransaction idTag=%s response=%s auto=%t\n", request.IdTag, status, auto)
	if accepted && auto {
		go func() {
			time.Sleep(100 * time.Millisecond)
			if err := s.executePendingRemoteStart(); err != nil {
				fmt.Printf("[SIM] automatic remote start failed: %v\n", err)
			}
		}()
	}
	return core.NewRemoteStartTransactionConfirmation(status), nil
}

func (s *simulator) OnRemoteStopTransaction(request *core.RemoteStopTransactionRequest) (*core.RemoteStopTransactionConfirmation, error) {
	s.mu.Lock()
	accepted := s.acceptStop && (s.transaction == request.TransactionId || s.stoppingTransaction == request.TransactionId)
	startCompletion := accepted && s.stoppingTransaction == 0
	if startCompletion {
		s.stoppingTransaction = request.TransactionId
	}
	s.mu.Unlock()
	status := types.RemoteStartStopStatusRejected
	if accepted {
		status = types.RemoteStartStopStatusAccepted
	}
	fmt.Printf("[REMOTE] RemoteStopTransaction transactionId=%d response=%s completing=%t\n", request.TransactionId, status, startCompletion)
	if startCompletion {
		go func() {
			if err := s.completeAcceptedRemoteStop(request.TransactionId); err != nil {
				fmt.Printf("[SIM] accepted remote stop completion failed: %v\n", err)
			}
		}()
	}
	return core.NewRemoteStopTransactionConfirmation(status), nil
}

func (s *simulator) completeAcceptedRemoteStop(transactionID int) error {
	s.opsMu.Lock()
	defer s.opsMu.Unlock()
	return s.stopTransactionLocked(core.ReasonRemote, transactionID)
}

func (s *simulator) OnReset(request *core.ResetRequest) (*core.ResetConfirmation, error) {
	fmt.Printf("[REMOTE] Reset type=%s\n", request.Type)
	go func() {
		time.Sleep(100 * time.Millisecond)
		if s.hasTransaction() {
			reason := core.ReasonSoftReset
			if request.Type == core.ResetTypeHard {
				reason = core.ReasonHardReset
			}
			if err := s.stopTransaction(reason); err != nil {
				fmt.Printf("[SIM] reset stop failed: %v\n", err)
				return
			}
		}
		if err := s.boot(); err != nil {
			fmt.Printf("[SIM] reset boot failed: %v\n", err)
			return
		}
		_ = s.setStatus(core.ChargePointStatusAvailable, core.NoError)
	}()
	return core.NewResetConfirmation(core.ResetStatusAccepted), nil
}

func (s *simulator) OnUnlockConnector(request *core.UnlockConnectorRequest) (*core.UnlockConnectorConfirmation, error) {
	fmt.Printf("[REMOTE] UnlockConnector connector=%d\n", request.ConnectorId)
	if request.ConnectorId != s.connectorID {
		return core.NewUnlockConnectorConfirmation(core.UnlockStatusNotSupported), nil
	}
	return core.NewUnlockConnectorConfirmation(core.UnlockStatusUnlocked), nil
}

// OCPP Firmware Management and Remote Trigger profile handlers used by HAL REST commands.

func (s *simulator) OnGetDiagnostics(request *firmware.GetDiagnosticsRequest) (*firmware.GetDiagnosticsConfirmation, error) {
	fmt.Printf("[REMOTE] GetDiagnostics location=%s\n", request.Location)
	conf := firmware.NewGetDiagnosticsConfirmation()
	conf.FileName = fmt.Sprintf("%s-diagnostics.log", s.clientID)
	go func() {
		time.Sleep(100 * time.Millisecond)
		_, _ = s.cp.DiagnosticsStatusNotification(firmware.DiagnosticsStatusUploading)
		time.Sleep(200 * time.Millisecond)
		_, _ = s.cp.DiagnosticsStatusNotification(firmware.DiagnosticsStatusUploaded)
	}()
	return conf, nil
}

func (s *simulator) OnUpdateFirmware(request *firmware.UpdateFirmwareRequest) (*firmware.UpdateFirmwareConfirmation, error) {
	fmt.Printf("[REMOTE] UpdateFirmware location=%s retrieveDate=%s\n", request.Location, request.RetrieveDate.String())
	go func() {
		time.Sleep(100 * time.Millisecond)
		for _, status := range []firmware.FirmwareStatus{firmware.FirmwareStatusDownloading, firmware.FirmwareStatusDownloaded, firmware.FirmwareStatusInstalling, firmware.FirmwareStatusInstalled} {
			_, _ = s.cp.FirmwareStatusNotification(status)
			time.Sleep(200 * time.Millisecond)
		}
	}()
	return firmware.NewUpdateFirmwareConfirmation(), nil
}

func (s *simulator) OnTriggerMessage(request *remotetrigger.TriggerMessageRequest) (*remotetrigger.TriggerMessageConfirmation, error) {
	fmt.Printf("[REMOTE] TriggerMessage requestedMessage=%s\n", request.RequestedMessage)
	implemented := request.RequestedMessage == core.BootNotificationFeatureName || request.RequestedMessage == core.HeartbeatFeatureName || request.RequestedMessage == core.StatusNotificationFeatureName || request.RequestedMessage == core.MeterValuesFeatureName
	if !implemented {
		return remotetrigger.NewTriggerMessageConfirmation(remotetrigger.TriggerMessageStatusNotImplemented), nil
	}
	go func() {
		time.Sleep(100 * time.Millisecond)
		var err error
		switch request.RequestedMessage {
		case core.BootNotificationFeatureName:
			err = s.boot()
		case core.HeartbeatFeatureName:
			err = s.heartbeat()
		case core.StatusNotificationFeatureName:
			s.mu.RLock()
			status, code := s.status, s.errorCode
			s.mu.RUnlock()
			err = s.setStatus(status, code)
		case core.MeterValuesFeatureName:
			err = s.sendMeter(0, s.currentPower())
		}
		if err != nil {
			fmt.Printf("[SIM] triggered %s failed: %v\n", request.RequestedMessage, err)
		}
	}()
	return remotetrigger.NewTriggerMessageConfirmation(remotetrigger.TriggerMessageStatusAccepted), nil
}

func parseBoolPolicy(value, truthy, falsy string) (bool, bool) {
	if strings.EqualFold(value, truthy) {
		return true, true
	}
	if strings.EqualFold(value, falsy) {
		return false, true
	}
	return false, false
}

func normalizeWebSocketURL(value string) (string, error) {
	value = strings.TrimRight(strings.TrimSpace(value), "/")
	parsed, err := url.Parse(value)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "ws" && parsed.Scheme != "wss") {
		return "", fmt.Errorf("OCPP URL must be an absolute ws:// or wss:// base URL: %q", value)
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", errors.New("OCPP base URL cannot contain a query string or fragment; the charger ID is appended as the final path segment")
	}
	return value, nil
}

func parseStatus(value string) (core.ChargePointStatus, bool) {
	statuses := []core.ChargePointStatus{core.ChargePointStatusAvailable, core.ChargePointStatusPreparing, core.ChargePointStatusCharging, core.ChargePointStatusSuspendedEVSE, core.ChargePointStatusSuspendedEV, core.ChargePointStatusFinishing, core.ChargePointStatusReserved, core.ChargePointStatusUnavailable, core.ChargePointStatusFaulted}
	for _, status := range statuses {
		if strings.EqualFold(value, string(status)) {
			return status, true
		}
	}
	return "", false
}

func parseErrorCode(value string) (core.ChargePointErrorCode, bool) {
	codes := []core.ChargePointErrorCode{core.ConnectorLockFailure, core.EVCommunicationError, core.GroundFailure, core.HighTemperature, core.InternalError, core.LocalListConflict, core.NoError, core.OtherError, core.OverCurrentFailure, core.OverVoltage, core.PowerMeterFailure, core.PowerSwitchFailure, core.ReaderFailure, core.ResetFailure, core.UnderVoltage, core.WeakSignal}
	for _, code := range codes {
		if strings.EqualFold(value, string(code)) {
			return code, true
		}
	}
	return "", false
}

func parseStopReason(value string) (core.Reason, bool) {
	reasons := []core.Reason{core.ReasonDeAuthorized, core.ReasonEmergencyStop, core.ReasonEVDisconnected, core.ReasonHardReset, core.ReasonLocal, core.ReasonOther, core.ReasonPowerLoss, core.ReasonReboot, core.ReasonRemote, core.ReasonSoftReset, core.ReasonUnlockCommand}
	for _, reason := range reasons {
		if strings.EqualFold(value, string(reason)) {
			return reason, true
		}
	}
	return "", false
}

func env(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func envInt(key string, fallback int) int {
	value, err := strconv.Atoi(env(key, strconv.Itoa(fallback)))
	if err != nil {
		return fallback
	}
	return value
}

func envFloat(key string, fallback float64) float64 {
	value, err := strconv.ParseFloat(env(key, strconv.FormatFloat(fallback, 'f', -1, 64)), 64)
	if err != nil {
		return fallback
	}
	return value
}
