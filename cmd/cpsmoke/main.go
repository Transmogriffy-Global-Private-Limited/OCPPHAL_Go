package main

import (
	"fmt"
	"log"
	"os"
	"time"

	ocpp16 "github.com/lorenzodonini/ocpp-go/ocpp1.6"
	"github.com/lorenzodonini/ocpp-go/ocpp1.6/core"
	"github.com/lorenzodonini/ocpp-go/ocpp1.6/types"
)

func main() {
	clientID := env("CLIENT_ID", "CP-LIB-001")
	centralSystemURL := env("CENTRAL_SYSTEM_URL", "ws://127.0.0.1:18081")

	cp := ocpp16.NewChargePoint(clientID, nil, nil)

	if err := cp.Start(centralSystemURL); err != nil {
		log.Fatalf("connect charge point: %v", err)
	}
	defer cp.Stop()

	waitUntilConnected(cp, 5*time.Second)

	fmt.Println("connected:", cp.IsConnected())

	bootConf, err := cp.BootNotification("SmokeModel", "SmokeVendor")
	if err != nil {
		log.Fatalf("BootNotification failed: %v", err)
	}
	fmt.Println("BootNotification:", bootConf.Status, "interval:", bootConf.Interval)

	_, err = cp.StatusNotification(1, core.NoError, core.ChargePointStatusAvailable)
	if err != nil {
		log.Fatalf("StatusNotification Available failed: %v", err)
	}
	fmt.Println("StatusNotification: Available")

	startConf, err := cp.StartTransaction(1, "USER001", 1000, types.Now())
	if err != nil {
		log.Fatalf("StartTransaction failed: %v", err)
	}
	if startConf == nil {
		log.Fatalf("StartTransaction returned nil confirmation")
	}

	transactionID := startConf.TransactionId
	fmt.Println("StartTransaction:", startConf.IdTagInfo.Status, "transactionId:", transactionID)

	_, err = cp.StatusNotification(1, core.NoError, core.ChargePointStatusCharging)
	if err != nil {
		log.Fatalf("StatusNotification Charging failed: %v", err)
	}
	fmt.Println("StatusNotification: Charging")

	meterValues := []types.MeterValue{
		{
			Timestamp: types.Now(),
			SampledValue: []types.SampledValue{
				{
					Value:     "2.500",
					Measurand: types.MeasurandEnergyActiveImportRegister,
					Unit:      types.UnitOfMeasureKWh,
				},
			},
		},
	}

	_, err = cp.MeterValues(
		1,
		meterValues,
		func(request *core.MeterValuesRequest) {
			request.TransactionId = &transactionID
		},
	)
	if err != nil {
		log.Fatalf("MeterValues failed: %v", err)
	}
	fmt.Println("MeterValues: 2.500 kWh")

	stopConf, err := cp.StopTransaction(3500, types.Now(), transactionID)
	if err != nil {
		log.Fatalf("StopTransaction failed: %v", err)
	}

	stopStatus := "Accepted"
	if stopConf != nil && stopConf.IdTagInfo != nil {
		stopStatus = string(stopConf.IdTagInfo.Status)
	}
	fmt.Println("StopTransaction:", stopStatus)

	_, err = cp.StatusNotification(1, core.NoError, core.ChargePointStatusAvailable)
	if err != nil {
		log.Fatalf("StatusNotification Available after stop failed: %v", err)
	}
	fmt.Println("StatusNotification: Available after stop")

	fmt.Println("smoke complete")
}

func waitUntilConnected(cp ocpp16.ChargePoint, timeout time.Duration) {
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		if cp.IsConnected() {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}

	log.Fatalf("charge point did not connect within %s", timeout)
}

func env(key string, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}
