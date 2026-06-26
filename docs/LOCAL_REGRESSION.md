# Local Regression

This repo has local smoke coverage for:

- OCPP core lifecycle
- REST outbound commands
- frontend WebSocket status snapshots
- durable callback outbox
- max kWh auto-stop
- single-session hooks
- charger directory validation
- unknown charger rejection

## Build everything

Run from repo root:

.\scripts\build-all.ps1

## Run local regression

.\scripts\regression-local.ps1

The script prompts for the PostgreSQL password.

## Run local regression without rebuilding

.\scripts\regression-local.ps1 -SkipBuild

## Expected local services

The regression script starts and stops these itself:

- mockhooks on 127.0.0.1:19090
- ocpphal REST API on 127.0.0.1:18080
- ocpphal OCPP server on 127.0.0.1:18081

## PostgreSQL used by local regression

host: 127.0.0.1
port: 5432
db: ocppgo
user: ocppgodbadmin

## Important env keys

API_KEY
APIAUTHKEY
APICHARGERDATA
CHARGER_DATA_CACHE_TTL_SECONDS
MAIN_CMS_START_TXN_HOOK_URL
MAIN_CMS_COMPLETED_TXN_URL
SINGLE_SESSION_START_TXN_HOOK_URL
SINGLE_SESSION_COMPLETED_TXN_URL
