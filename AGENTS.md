# AI Agent Instructions for OCPPHAL Go

This repository is a work codebase. Keep all changes professional, production-oriented, and compatibility-focused.

## Project purpose

OCPPHAL Go is a Go-based OCPP 1.6 compatibility service. It preserves the active behavior of the previous OCPP compatibility layer while using github.com/lorenzodonini/ocpp-go for protocol handling.

The service owns charger communication, CMS-facing REST compatibility APIs, transaction persistence, callback delivery, charger directory validation, frontend status snapshots, and local regression testing.

## Non-negotiable rules

1. Do not manually reimplement OCPP protocol framing when ocpp-go already provides the capability.
2. Preserve compatibility with the active old REST and WebSocket surface.
3. Do not add speculative endpoints just because a library supports them.
4. Do not commit generated binaries from bin/.
5. Do not commit local workspaces such as _review/, _parity/, or _git_review/.
6. Do not commit .env or real secrets.
7. Do not change callback payload shapes casually. They are compatibility contracts.
8. Do not break the local regression script.
9. Keep code boring, explicit, observable, and rollback-friendly.
10. Prefer small, reviewable changes over broad rewrites.

## Required validation before completing changes

Run these from the repository root before considering a change complete:

    .\scripts\build-all.ps1
    .\scripts\regression-local.ps1 -SkipBuild

If PostgreSQL or local services are unavailable, state that clearly and provide the exact command that failed.

## Architecture map

- cmd/ocpphal: main server binary.
- cmd/mockhooks: local mock backend for CMS hooks and charger directory.
- cmd/cpsmoke: general charger smoke client.
- cmd/cplimitsmoke: max-kWh auto-stop smoke client.
- cmd/cpsinglesmoke: single-session smoke client.
- cmd/frontendwssmoke: frontend WebSocket smoke client.
- internal/ocpp16hal: OCPP 1.6 HAL, central handlers, outbound charger commands.
- internal/httpapi: CMS REST compatibility API and frontend WebSocket route.
- internal/store: memory and PostgreSQL stores.
- internal/hooks: durable callback outbox worker.
- internal/chargerdir: external charger directory client and cache.
- internal/config: environment-based configuration.
- migrations: PostgreSQL schema migrations.
- scripts: build and regression scripts.
- docs: operational docs.

## Active compatibility surface

The following REST routes are active compatibility routes:

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

The following WebSocket routes are active:

- OCPP charger connection on /{charger_id}
- OCPP charger connection on /{charger_id}/{serialnumber}
- Frontend status snapshots on /frontend/ws/{uid}

## Intentionally not implemented unless requested

- Message persistence history for every charger-to-CMS or CMS-to-charger frame.
- DataTransfer as a REST endpoint.
- SendLocalList and GetLocalListVersion based local-auth-list synchronization.
- Reservation and smart charging workflows.

These may exist in OCPP or ocpp-go, but they were not part of the confirmed active compatibility surface.

## Compatibility behavior that must be preserved

- /api/status must support specific charger IDs, all, and all_online.
- /api/status must use charger directory validation when APICHARGERDATA is configured.
- Known but offline chargers return Offline status, not 404.
- Unknown chargers return not found for status and are rejected at OCPP connection validation.
- Start transaction callbacks must store max_kwh when returned by CMS.
- MeterValues must trigger automatic RemoteStopTransaction when max_kwh is crossed.
- Single-session start and completed callbacks must use single-session hook URLs when configured.
- Callback delivery must go through the durable outbox.
- Remote-only/local-auth config enforcement must remain best-effort and non-fatal.

## Database expectations

PostgreSQL migrations live in migrations/. Apply them in order for new environments.

- 001_create_transactions.sql
- 002_create_callback_outbox.sql
- 003_add_limit_stop_requested.sql

The memory store is for local/no-database fallback only. Production should use PostgreSQL.

## Environment

Use .env.example as the reference. Do not invent new env names unless necessary.

Important env keys include API_KEY, APIAUTHKEY, DB_HOST, DB_PORT, DB_NAME, DB_USER, DB_PASSWORD, DB_SSLMODE, APICHARGERDATA, CHARGER_DATA_CACHE_TTL_SECONDS, MAIN_CMS_START_TXN_HOOK_URL, MAIN_CMS_COMPLETED_TXN_URL, SINGLE_SESSION_START_TXN_HOOK_URL, and SINGLE_SESSION_COMPLETED_TXN_URL.

## Development workflow for agents

1. Inspect relevant code before editing.
2. Make the smallest compatible change.
3. Run gofmt via scripts/build-all.ps1.
4. Run regression-local.ps1 -SkipBuild.
5. Show exact failures if validation cannot complete.
6. Keep commits focused by feature or fix.

## PowerShell command guidance

When providing PowerShell commands for this repo, avoid here-strings and fragile multiline quoting. Prefer simple commands, script files, or line-by-line file creation. Backticks in markdown can break double-quoted PowerShell strings.

## Git hygiene

- main is the deployable branch.
- rewrite/ocpp-go-clean-20260625_141148 is the preserved parity rewrite branch.
- Keep main and the parity rewrite branch synchronized when requested.
- Do not push without explicit user approval.
- Do not delete branches without explicit confirmation.

## Operational posture

This service should be deployed with an explicit env file, PostgreSQL migrations, a systemd unit, smoke checks, reverse proxy switch-over, and rollback path to the previous production service.
