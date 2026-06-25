$ErrorActionPreference = "Stop"

$env:F_SERVER_HOST = "127.0.0.1"
$env:F_SERVER_PORT = "18080"
$env:API_KEY = "testkey"
$env:LOG_LEVEL = "debug"

go run ./cmd/ocpphal
