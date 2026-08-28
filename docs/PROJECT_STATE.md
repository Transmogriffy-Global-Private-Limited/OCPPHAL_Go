# Project state

OCPPHAL Go is an implemented OCPP 1.6 compatibility service with PostgreSQL
transaction persistence, durable callback delivery, charger-directory validation,
remote commands, frontend status snapshots, recovery behavior and local regression.

## Virtual charger

`cmd/cpconsole` is a standalone interactive OCPP 1.6J charge point client. It:

- accepts a configurable Central System URL and charger ID;
- performs BootNotification and Available status during startup;
- supports realistic local and remote charging flows;
- advances a cumulative Wh register using elapsed time and power;
- publishes energy, power, current, voltage and SoC samples;
- supports automatic periodic metering, faults and remote-response policies;
- handles the HAL's active Core, Firmware Management and Remote Trigger commands;
- builds for Windows and Linux amd64/arm64.

It does not embed the HAL, listen for inbound traffic, persist its own local
business state, or fabricate Central System transaction IDs. The selected ID
must be recognized by the connected HAL.

## Known documentation limitation

Operational and focused integration documentation exists. Complete authoritative
OpenAPI/Swagger coverage for the established HAL REST compatibility routes is not
yet implemented and is tracked in `docs/DEVELOPMENT_PLAN.md`.
