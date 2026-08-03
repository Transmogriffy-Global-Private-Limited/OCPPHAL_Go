param(
    [ValidateSet("amd64", "arm64")]
    [string]$Architecture = "amd64"
)

$ErrorActionPreference = "Stop"
$PSNativeCommandUseErrorActionPreference = $true

$repoRoot = Split-Path -Parent $PSScriptRoot
Set-Location $repoRoot

New-Item -ItemType Directory -Force -Path ".\builds" | Out-Null

$previousGOOS = $env:GOOS
$previousGOARCH = $env:GOARCH
$previousCGO = $env:CGO_ENABLED

try {
    $env:GOOS = "linux"
    $env:GOARCH = $Architecture
    $env:CGO_ENABLED = "0"

    $output = ".\builds\cpconsole-linux-$Architecture"
    go build -trimpath -o $output .\cmd\cpconsole
    Get-Item $output | Select-Object FullName, Length, LastWriteTime
}
finally {
    $env:GOOS = $previousGOOS
    $env:GOARCH = $previousGOARCH
    $env:CGO_ENABLED = $previousCGO
}
