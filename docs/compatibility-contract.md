# Compatibility Contract

The Go service must preserve the old Python OCPP HAL external surface unless intentionally changed.

## HTTP

- GET /api/hello
- POST /api/change_availability
- POST /api/start_transaction
- POST /api/stop_transaction
- POST /api/change_configuration
- POST /api/clear_cache
- POST /api/unlock_connector
- POST /api/get_diagnostics
- POST /api/update_firmware
- POST /api/reset
- POST /api/get_configuration
- POST /api/status
- POST /api/trigger_message
- POST /api/charger_analytics
- POST /api/check_charger_inactivity

Protected /api routes require x-api-key, except /api/hello.

## WebSocket

- /{charger_id}
- /{charger_id}/{serialnumber}
- /frontend/ws/{uid}
