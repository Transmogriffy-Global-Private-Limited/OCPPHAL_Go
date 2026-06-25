$ErrorActionPreference = "Stop"
$PSNativeCommandUseErrorActionPreference = $true

$env:CLIENT_ID = "CP-LIB-001"
$env:CENTRAL_SYSTEM_URL = "ws://127.0.0.1:18081"

.\bin\cpsmoke.exe
