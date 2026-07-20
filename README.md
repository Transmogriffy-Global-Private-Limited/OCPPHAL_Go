# OCPPHAL Go

OCPPHAL Go is a Go-based OCPP 1.6 compatibility service for charger communication, CMS-facing REST APIs, transaction persistence, callback delivery, charger status visibility, and operational smoke/regression testing.

The service replaces the legacy OCPP compatibility layer while preserving the active REST and WebSocket behavior required by the existing CMS/frontend integration.

## Core capabilities

- OCPP 1.6 central system using github.com/lorenzodonini/ocpp-go.
- Charger WebSocket support through OCPP charge point IDs.
- CMS REST compatibility endpoints for charger control and status.
- PostgreSQL-backed transaction persistence.
- Durable callback outbox for start/completed transaction hooks.
- Main-session and single-session hook routing.
- Max-kWh limit enforcement with automatic RemoteStopTransaction.
- Charger directory validation from an external charger-data endpoint.
- Remote-only authorization configuration sync for chargers.
- Duplicate charger connection generation guard to prevent stale disconnects from marking a reconnected charger offline.
- Boot-time open transaction recovery for charger reconnect/reboot scenarios.
- Frontend WebSocket status snapshots.
- Local smoke clients and regression scripts.

## Repository layout

| Path | Purpose |
| --- | --- |
| cmd/ocpphal | Main OCPPHAL server binary. |
| cmd/mockhooks | Local mock backend for CMS callback and charger-data testing. |
| cmd/cpsmoke | General charge point smoke client. |
| cmd/cplimitsmoke | Pure max-kWh auto-stop smoke client. |
| cmd/cpsinglesmoke | Single-session smoke client. |
| cmd/frontendwssmoke | Frontend WebSocket smoke client. |
| internal/ocpp16hal | OCPP 1.6 HAL, central system handlers, and outbound charger commands. |
| internal/httpapi | CMS-facing REST API and frontend WebSocket compatibility layer. |
| internal/store | PostgreSQL and memory stores for transactions, analytics, and callbacks. |
| internal/hooks | Durable callback outbox worker. |
| internal/chargerdir | Charger directory client and cache. |
| internal/config | Environment-based configuration. |
| migrations | PostgreSQL schema migrations. |
| scripts | Build and local regression scripts. |
| docs | Operational documentation. |

## REST API compatibility

The following active compatibility endpoints are implemented:

- GET /api/hello
- POST /api/status
- POST /api/start_transaction
- POST /api/stop_transaction
- POST /api/change_availability
- POST /api/change_configuration
- POST /api/clear_cache
- POST /api/unlock_connector
- POST /api/get_diagnostics
- POST /api/update_firmware
- POST /api/reset
- POST /api/get_configuration
- POST /api/trigger_message
- POST /api/check_charger_inactivity
- POST /api/charger_analytics

All protected REST endpoints expect the configured API key in the x-api-key header.

## WebSocket endpoints

- ws://host:{OCPP_LISTEN_PORT}/{charger_id} for OCPP 1.6 charger connection.
- ws://host:{OCPP_LISTEN_PORT}/{charger_id}/{serialnumber} for OCPP 1.6 charger connection with serial suffix compatibility.
- ws://host:{F_SERVER_PORT}/frontend/ws/{uid} for frontend status snapshots.

## Configuration

Use .env.example as the reference template.

Important variables:

- F_SERVER_HOST: REST bind host.
- F_SERVER_PORT: REST bind port.
- OCPP_LISTEN_PORT: OCPP central system port.
- OCPP_LISTEN_PATH: OCPP WebSocket path pattern.
- API_KEY: REST API key expected in x-api-key.
- APIAUTHKEY: API auth key sent to CMS hook/charger-data endpoints.
- DB_HOST, DB_PORT, DB_NAME, DB_USER, DB_PASSWORD, DB_SSLMODE: PostgreSQL connection settings.
- DATABASE_URL: Optional PostgreSQL connection URL alternative.
- MAIN_CMS_START_TXN_HOOK_URL: Main transaction start callback URL.
- MAIN_CMS_COMPLETED_TXN_URL: Main transaction completion callback URL.
- SINGLE_SESSION_START_TXN_HOOK_URL: Single-session start callback URL.
- SINGLE_SESSION_COMPLETED_TXN_URL: Single-session completion callback URL.
- APICHARGERDATA: External charger directory endpoint.
- CHARGER_DATA_CACHE_TTL_SECONDS: Charger directory cache TTL.
- LOG_LEVEL: debug, info, warn, or error.

## Database

PostgreSQL is used for transaction persistence and callback outbox durability.

Run migrations before starting the service in a new environment:

- migrations/001_create_transactions_postgres.sql
- migrations/002_create_callback_outbox.sql
- migrations/003_add_limit_stop_requested.sql
- migrations/004_require_transaction_id.sql

Example:

    $env:PGPASSWORD = Read-Host "PostgreSQL password"
    psql -h 127.0.0.1 -p 5432 -U ocppgodbadmin -d ocppgo -f ./migrations/001_create_transactions_postgres.sql
    psql -h 127.0.0.1 -p 5432 -U ocppgodbadmin -d ocppgo -f ./migrations/002_create_callback_outbox.sql
    psql -h 127.0.0.1 -p 5432 -U ocppgodbadmin -d ocppgo -f ./migrations/003_add_limit_stop_requested.sql
    psql -h 127.0.0.1 -p 5432 -U ocppgodbadmin -d ocppgo -f ./migrations/004_require_transaction_id.sql

## Build

    ./scripts/build-all.ps1

This builds:

- builds/ocpphal.exe
- builds/mockhooks.exe
- builds/cpsmoke.exe
- builds/cplimitsmoke.exe
- builds/cpsinglesmoke.exe
- builds/frontendwssmoke.exe

## Local regression

    ./scripts/regression-local.ps1

Skip build if binaries are already current:

    ./scripts/regression-local.ps1 -SkipBuild

The regression script starts mockhooks and OCPPHAL locally, then verifies charger status, charger validation, core smoke flow, single-session flow, frontend WebSocket snapshots, unknown charger rejection, max-kWh auto-stop, and PostgreSQL visibility.

## Local development notes

- Do not commit generated binaries from builds/.
- Do not commit local review/audit workspaces such as _review/, _parity/, or _git_review/.
- Keep .env and real secrets out of Git.
- Use .env.example for configuration reference only.
- Run ./scripts/build-all.ps1 and ./scripts/regression-local.ps1 -SkipBuild before merging or deploying.

## Deployment notes

Deployment should be done with an explicit environment file, PostgreSQL migrations, a systemd service, and a rollback plan to the previous production service.

Recommended deployment order:

- Build Linux binary or build directly on the VPS.
- Apply PostgreSQL migrations.
- Create production environment file.
- Install systemd service.
- Start service on a non-conflicting port first.
- Run smoke checks.
- Switch reverse proxy only after verification.
- Keep rollback path to the previous service until production is confirmed stable.

## Operational status

The current implementation has passed local parity and regression checks for the active old REST/WebSocket surface.

Active parity covered:

- Charger status and frontend status snapshots.
- Remote start/stop.
- Core charger commands.
- Firmware, diagnostics, reset, and trigger message commands.
- Inactivity and analytics routes.
- Main-session and single-session callbacks.
- Max-kWh automatic remote stop.
- Charger directory validation.
- Remote-only/local-auth configuration enforcement.
- Duplicate connection stale-disconnect protection.
- Boot recovery for open transaction hydration, ghost-session force-close, and RemoteStop/Unlock retry.
