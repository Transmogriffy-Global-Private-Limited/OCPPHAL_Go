param(
    [switch] $SkipBuild
)

$ErrorActionPreference = "Stop"
$PSNativeCommandUseErrorActionPreference = $true

Set-Location "C:\Users\AnubhabDey\Programs\My_Programs\OCPPHAL_Go"

function Start-RepoJob {
    param(
        [string] $Name,
        [string] $CommandPath,
        [hashtable] $EnvMap
    )

    $repo = (Get-Location).Path

    $job = Start-Job -Name $Name -ScriptBlock {
        param($RepoPath, $EnvMap, $CommandPath)

        Set-Location $RepoPath

        foreach ($key in $EnvMap.Keys) {
            Set-Item -Path "Env:$key" -Value $EnvMap[$key]
        }

        & $CommandPath
    } -ArgumentList $repo, $EnvMap, $CommandPath

    Start-Sleep -Milliseconds 1200

    if ($job.State -ne "Running") {
        Write-Host ""
        Write-Host "===== $Name exited early ====="
        Receive-Job $job
        throw "$Name did not stay running"
    }

    return $job
}

function Stop-RepoJob {
    param(
        [System.Management.Automation.Job] $Job
    )

    if ($null -eq $Job) {
        return
    }

    Write-Host ""
    Write-Host "===== stopping job: $($Job.Name) ====="

    Stop-Job $Job -ErrorAction SilentlyContinue | Out-Null

    Write-Host ""
    Write-Host "===== logs from job: $($Job.Name) ====="
    Receive-Job $Job -ErrorAction SilentlyContinue

    Remove-Job $Job -Force -ErrorAction SilentlyContinue
}

function Wait-HttpOk {
    param(
        [string] $Url,
        [int] $TimeoutSeconds = 20
    )

    $deadline = (Get-Date).AddSeconds($TimeoutSeconds)

    while ((Get-Date) -lt $deadline) {
        try {
            $resp = Invoke-WebRequest -Uri $Url -UseBasicParsing -TimeoutSec 2
            if ($resp.StatusCode -ge 200 -and $resp.StatusCode -lt 300) {
                return
            }
        }
        catch {
            Start-Sleep -Milliseconds 500
        }
    }

    throw "Timed out waiting for $Url"
}

function Invoke-Status {
    param(
        [string] $Uid
    )

    Invoke-RestMethod `
        -Method Post `
        -Uri "http://127.0.0.1:18080/api/status" `
        -Headers @{ "x-api-key" = "testkey" } `
        -ContentType "application/json" `
        -Body (@{ uid = $Uid } | ConvertTo-Json -Compress)
}

function Run-SmokeExe {
    param(
        [string] $Name,
        [string] $ClientID,
        [string] $Exe
    )

    Write-Host ""
    Write-Host "===== running ${Name}: ${ClientID} ====="

    $env:CLIENT_ID = $ClientID
    $env:CENTRAL_SYSTEM_URL = "ws://127.0.0.1:18081"
    $env:REST_BASE_URL = "http://127.0.0.1:18080"
    $env:API_KEY = "testkey"

    & $Exe
}

if (-not $SkipBuild) {
    & ".\scripts\build-all.ps1"
}

$pgPassword = Read-Host "PostgreSQL password"

$mockJob = $null
$serverJob = $null

try {
    Write-Host ""
    Write-Host "===== starting mockhooks with normal max_kwh ====="

    $chargerIDs = @(
        "CP-REG-CORE-001",
        "CP-REG-SINGLE-001",
        "CP-REG-LIMIT-001",
        "CP-REG-OFFLINE-001",
        "CP-LIMIT-AUTO-001",
        "CP-SINGLE-001",
        "CP-LIMIT-002",
        "CP-HOOKS-002",
        "CP-ANALYTICS-001"
    ) -join ","

    $mockJob = Start-RepoJob -Name "mockhooks-normal" -EnvMap @{
        MOCK_HOOK_ADDR      = "127.0.0.1:19090"
        MOCK_START_MAX_KWH  = "7.5"
        MOCK_CHARGER_IDS    = $chargerIDs
    } -CommandPath ".\bin\mockhooks.exe"

    Wait-HttpOk -Url "http://127.0.0.1:19090/health" -TimeoutSeconds 20

    Write-Host ""
    Write-Host "===== starting ocpphal ====="

    $serverJob = Start-RepoJob -Name "ocpphal" -EnvMap @{
        F_SERVER_HOST = "127.0.0.1"
        F_SERVER_PORT = "18080"

        OCPP_LISTEN_PORT = "18081"
        OCPP_LISTEN_PATH = "/{ws}"

        API_KEY     = "testkey"
        APIAUTHKEY  = "mock-api-auth-key"
        LOG_LEVEL   = "debug"

        DB_HOST     = "127.0.0.1"
        DB_PORT     = "5432"
        DB_NAME     = "ocppgo"
        DB_USER     = "ocppgodbadmin"
        DB_PASSWORD = $pgPassword
        DB_SSLMODE  = "disable"

        APICHARGERDATA                 = "http://127.0.0.1:19090/chargers"
        CHARGER_DATA_CACHE_TTL_SECONDS = "30"

        MAIN_CMS_START_TXN_HOOK_URL = "http://127.0.0.1:19090/users/checkstartresponse"
        MAIN_CMS_COMPLETED_TXN_URL  = "http://127.0.0.1:19090/users/deductcalculate"

        SINGLE_SESSION_START_TXN_HOOK_URL = "http://127.0.0.1:19090/single/checkstartresponse"
        SINGLE_SESSION_COMPLETED_TXN_URL  = "http://127.0.0.1:19090/single/deductcalculate"
    } -CommandPath ".\bin\ocpphal.exe"

    Wait-HttpOk -Url "http://127.0.0.1:18080/api/hello" -TimeoutSeconds 20

    Write-Host ""
    Write-Host "===== status: known offline charger should return Offline ====="

    $knownOffline = Invoke-Status -Uid "CP-REG-OFFLINE-001"
    $knownOffline | ConvertTo-Json -Depth 30

    if ($knownOffline.online -ne "Offline") {
        throw "Expected CP-REG-OFFLINE-001 to be Offline"
    }

    Write-Host ""
    Write-Host "===== status: unknown charger should 404 ====="

    $unknownFailed = $false
    try {
        Invoke-Status -Uid "CP-NOT-IN-ENDPOINT" | ConvertTo-Json -Depth 30
    }
    catch {
        $unknownFailed = $true
        Write-Host "Expected unknown charger failure:"
        Write-Host $_.Exception.Message
        if ($_.ErrorDetails.Message) {
            Write-Host $_.ErrorDetails.Message
        }
    }

    if (-not $unknownFailed) {
        throw "Unknown charger status unexpectedly succeeded"
    }

    Run-SmokeExe -Name "core smoke" -ClientID "CP-REG-CORE-001" -Exe ".\bin\cpsmoke.exe"

    Run-SmokeExe -Name "single-session smoke" -ClientID "CP-REG-SINGLE-001" -Exe ".\bin\cpsinglesmoke.exe"

    Write-Host ""
    Write-Host "===== frontend websocket smoke ====="

    $env:FRONTEND_WS_URL = "ws://127.0.0.1:18080/frontend/ws/CP-REG-SINGLE-001"
    & ".\bin\frontendwssmoke.exe"

    Write-Host ""
    Write-Host "===== unknown charger WS rejection smoke ====="

    $unknownWsFailed = $false
    try {
        Run-SmokeExe -Name "unknown charger smoke" -ClientID "CP-NOT-IN-ENDPOINT" -Exe ".\bin\cpsinglesmoke.exe"
    }
    catch {
        $unknownWsFailed = $true
        Write-Host "Expected unknown charger WS failure:"
        Write-Host $_.Exception.Message
    }

    if (-not $unknownWsFailed) {
        throw "Unknown charger unexpectedly connected over OCPP WS"
    }

    Write-Host ""
    Write-Host "===== switching mockhooks to low max_kwh for auto-stop ====="

    Stop-RepoJob -Job $mockJob
    $mockJob = $null

    $mockJob = Start-RepoJob -Name "mockhooks-limit" -EnvMap @{
        MOCK_HOOK_ADDR      = "127.0.0.1:19090"
        MOCK_START_MAX_KWH  = "1.0"
        MOCK_CHARGER_IDS    = $chargerIDs
    } -CommandPath ".\bin\mockhooks.exe"

    Wait-HttpOk -Url "http://127.0.0.1:19090/health" -TimeoutSeconds 20

    Run-SmokeExe -Name "pure auto-stop smoke" -ClientID "CP-REG-LIMIT-001" -Exe ".\bin\cplimitsmoke.exe"

    Write-Host ""
    Write-Host "===== waiting for outbox worker ====="
    Start-Sleep -Seconds 8

    Write-Host ""
    Write-Host "===== database verification ====="

    $env:PGPASSWORD = $pgPassword

    psql `
      -h 127.0.0.1 `
      -p 5432 `
      -U ocppgodbadmin `
      -d ocppgo `
      -c "SELECT id, kind, transaction_id, status, retries, last_error, sent_at FROM callback_outbox ORDER BY id DESC LIMIT 12;"

    psql `
      -h 127.0.0.1 `
      -p 5432 `
      -U ocppgodbadmin `
      -d ocppgo `
      -c "SELECT id, charger_id, transaction_id, is_single_session, total_consumption, max_kwh, limit_stop_requested, stop_time FROM transactions ORDER BY id DESC LIMIT 12;"

    Write-Host ""
    Write-Host "===== local regression passed ====="
}
finally {
    Stop-RepoJob -Job $serverJob
    Stop-RepoJob -Job $mockJob
}


