$ErrorActionPreference = "Stop"
$PSNativeCommandUseErrorActionPreference = $true

$goFiles = @(
    "cmd/ocpphal/main.go",
    "cmd/cpsmoke/main.go",
    "internal/config/config.go",
    "internal/state/registry.go",
    "internal/ocpp16hal/hal.go",
    "internal/httpapi/server.go"
) + (Get-ChildItem "internal/store" -Filter "*.go" | ForEach-Object { $_.FullName })

gofmt -w $goFiles
go mod tidy
go test ./...
go build -o ".\bin\ocpphal.exe" ".\cmd\ocpphal"
go build -o ".\bin\cpsmoke.exe" ".\cmd\cpsmoke"

Get-Item ".\bin\ocpphal.exe", ".\bin\cpsmoke.exe" |
    Select-Object FullName, Length, LastWriteTime
