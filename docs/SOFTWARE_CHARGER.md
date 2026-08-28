# Terminal-controlled OCPP 1.6J virtual charger

## Purpose and boundary

`cpconsole` is a standalone software EV charger. It behaves as an OCPP 1.6J
Charge Point and connects outbound to an OCPP Central System over WebSocket. It
does not run inside the HAL server and does not call the HAL database or CMS
directly.

```text
terminal commands or HAL remote commands
→ cpconsole charge-point state machine
→ OCPP 1.6J WebSocket messages
→ OCPPHAL (or another OCPP 1.6J Central System)
→ durable HAL transaction and callback flow
→ CMS and frontend consumers
```

The simulator is useful only when connected to a Central System. Running it
without one would create local pretend state but would not test any real
integration, so connection and an accepted `BootNotification` are startup
requirements.

## Charger identity and target URL

Set the charge point identity with `-id`. `ocpp-go` appends that identity to the
base URL, matching the HAL's `/{charger_id}` WebSocket route.

```powershell
.\builds\cpconsole.exe -id CP-SIM-001 -url ws://127.0.0.1:18081
```

For a hosted TLS endpoint:

```powershell
.\builds\cpconsole.exe -id CP-SIM-001 -url wss://ocpp.example.com
```

The ID must be present in the target HAL's charger directory. Local regression
may instead include it in `MOCK_CHARGER_IDS`. A directory-rejected identity will
fail during the WebSocket handshake, exactly as unknown hardware would.

The URL must be an absolute `ws://` or `wss://` base URL and may include a base
path. Do not include the charger ID, query string, or fragment; the selected
`-id` is appended as the final path segment. A trailing slash is normalized.

Supported flags:

| Flag | Environment fallback | Default | Meaning |
| --- | --- | --- | --- |
| `-url` | `CP_SIM_URL` | `ws://127.0.0.1:18081` | Central System WebSocket base URL. |
| `-id` | `CP_SIM_ID` | `CP-SIM-001` | OCPP charge point identity. |
| `-model` | `CP_SIM_MODEL` | `TransEV-Simulator` | Boot model (1–20 characters). |
| `-vendor` | `CP_SIM_VENDOR` | `TransEV` | Boot vendor (1–20 characters). |
| `-connector` | `CP_SIM_CONNECTOR` | `1` | Simulated connector ID. |
| `-meter-start-wh` | `CP_SIM_METER_START_WH` | `100000` | Initial cumulative energy register. |
| `-voltage` | `CP_SIM_VOLTAGE` | `230` | Voltage used for coherent current calculation. |
| `-soc` | `CP_SIM_SOC` | `35` | Initial EV state of charge. |

Flags take precedence over environment values. These variables are simulator
client settings; they are not consumed by the HAL server.

## Build and run on Windows

```powershell
Set-Location C:\path\to\OCPPHAL_Go
.\scripts\build-all.ps1
.\builds\cpconsole.exe -id CP-SIM-001 -url ws://127.0.0.1:18081
```

## Build for Linux VPS2 from Windows

For the usual x86-64 VPS:

```powershell
Set-Location C:\path\to\OCPPHAL_Go
.\scripts\build-cpconsole-linux.ps1 -Architecture amd64
```

For an ARM64 VPS, use `-Architecture arm64`. The output is ignored by Git:

```text
builds/cpconsole-linux-amd64
builds/cpconsole-linux-arm64
```

Copy the applicable file to VPS2, then run:

```text
chmod 700 ./cpconsole-linux-amd64
./cpconsole-linux-amd64 -id CP-SIM-001 -url wss://ocpp.example.com
```

Alternatively, build directly on Linux from the repository:

```text
go build -trimpath -o ./builds/cpconsole ./cmd/cpconsole
./builds/cpconsole -id CP-SIM-001 -url wss://ocpp.example.com
```

No database, CMS credentials, or inbound public listener is required by the
simulator. VPS2 only needs outbound access to the selected WebSocket endpoint.

## Normal charging flow

Startup automatically performs:

```text
WebSocket connect
→ BootNotification (must be Accepted)
→ StatusNotification Available
→ terminal prompt
```

A realistic locally initiated session is:

```text
plug
→ StatusNotification Preparing

start USER001
→ Authorize USER001
→ StartTransaction with cumulative meterStart in Wh
→ server-assigned transactionId retained exactly
→ StatusNotification Charging

tick 60 7.2
→ add 120 Wh (7.2 kW × 60 seconds)
→ MeterValues with the exact transactionId
→ cumulative energy, power, current, voltage and SoC samples

stop Local
→ StopTransaction with the same transactionId and cumulative meterStop
→ StatusNotification Finishing

unplug
→ StatusNotification Available
```

`tick` never invents unrelated readings. The simulator calculates:

- energy delta from power multiplied by elapsed time;
- current from power divided by configured voltage;
- SoC from a documented 60 kWh reference battery;
- consumed session energy from cumulative `meterStop - meterStart`.

The energy register is monotonic and is transmitted in Wh. Transaction IDs are
assigned by the Central System and never fabricated by the simulator.

## Remote CMS flow

By default, valid `RemoteStartTransaction` requests are accepted and executed
after their confirmation is returned. A valid `RemoteStopTransaction` is always
self-completing once it is accepted: cpconsole sends exactly one
`StopTransaction`, clears its active state after the confirmation, then reports
`Finishing` followed immediately by `Available`. It never waits for a manual
simulated unplug action, so the connector can admit the next session.

For manual failure and timing tests:

```text
policy auto-remote off
policy remote-start accept
```

The next valid remote start is acknowledged and retained as pending. Inspect it
with `state`, then run `remote-start`. Accepted remote stops are intentionally
not deferrable.
Future remote commands may be rejected deliberately with:

```text
policy remote-start reject
policy remote-stop reject
```

Remote start is rejected when a transaction is already active or the connector
is `Faulted`/`Unavailable`. Remote stop is rejected unless its transaction ID
matches the active server-assigned transaction.

## Terminal command reference

Run `help` inside the program for the canonical compact command list.

- `state` prints connection, boot, connector, meter, transaction and policy state.
- `heartbeat` sends an on-demand OCPP heartbeat.
- `plug`, `unplug`, `finish`, `suspend ev|evse`, and `resume` model normal states.
- `authorize`, `start`, `tick`, `meter`, and `stop` drive the transaction lifecycle.
- `auto <seconds> <power-kW>` sends periodic coherent readings; `auto off` stops it.
- `fault <error-code>` and `clear-fault` test valid OCPP fault behavior.
- `status <ocpp-status>` is an explicit escape hatch for protocol edge testing.
- `policy ...` controls responses and automatic execution of remote starts;
  accepted remote stops always complete.
- `quit` closes the client cleanly.

Invalid state transitions are rejected for normal commands. The explicit
`status` command intentionally permits raw valid OCPP statuses so a tester can
exercise unusual Central System behavior.

## Other HAL commands

The virtual charger handles the active HAL remote-command surface:

- ChangeAvailability, including `Scheduled` while a transaction is active;
- ChangeConfiguration and GetConfiguration using in-memory charger configuration;
- ClearCache, UnlockConnector, Reset and DataTransfer;
- GetDiagnostics with coherent Uploading then Uploaded notifications;
- UpdateFirmware with Downloading, Downloaded, Installing and Installed notifications;
- TriggerMessage for BootNotification, Heartbeat, StatusNotification and MeterValues.

Configuration and pending actions intentionally live only for the lifetime of
the simulator process; durable business truth remains in the Central System.

## Failure, restart, and recovery semantics

- If the WebSocket or BootNotification fails, startup exits non-zero.
- OCPP request errors are printed and do not fabricate success.
- The underlying library does not automatically reconnect. Restart `cpconsole`
  to model charger reconnection and boot recovery.
- An open transaction is not locally persisted across simulator process exit.
  This is deliberate: HAL recovery behavior should be tested by keeping the
  durable transaction in HAL and reconnecting the same charger ID, then using
  the HAL's recovery commands.
- `Ctrl+C` or `quit` stops the client. Use an explicit `stop` first when the test
  requires a normal completed transaction and CMS callback.

## Verification

Focused compile/test:

```powershell
go test .\cmd\cpconsole
go build -o .\builds\cpconsole.exe .\cmd\cpconsole
.\scripts\build-cpconsole-linux.ps1 -Architecture amd64
```

Full repository verification:

```powershell
.\scripts\build-all.ps1
.\scripts\regression-local.ps1 -SkipBuild
```

For an end-to-end manual check, register the selected charger ID, run the HAL,
start `cpconsole`, execute the normal charging flow, then verify the HAL
transaction row, callback delivery, CMS session, and frontend live snapshot.
