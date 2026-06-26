param()

$ErrorActionPreference = "Stop"
$PSNativeCommandUseErrorActionPreference = $true

Set-Location "C:\Users\AnubhabDey\Programs\My_Programs\OCPPHAL_Go"

Write-Host ""
Write-Host "===== gofmt ====="

$goFiles = (Get-ChildItem -Recurse -Filter "*.go" |
    Where-Object {
        $_.FullName -notmatch "\\_parity\\" -and
        $_.FullName -notmatch "\\.git\\"
    } |
    ForEach-Object { $_.FullName }) |
    Sort-Object -Unique

gofmt -w $goFiles

Write-Host ""
Write-Host "===== go test ./... ====="

go test ./...

Write-Host ""
Write-Host "===== build binaries ====="

New-Item -ItemType Directory -Force -Path ".\bin" | Out-Null

$targets = @(
    @{ Name = "ocpphal";         Path = ".\cmd\ocpphal" },
    @{ Name = "cpsmoke";         Path = ".\cmd\cpsmoke" },
    @{ Name = "cplimitsmoke";    Path = ".\cmd\cplimitsmoke" },
    @{ Name = "cpsinglesmoke";   Path = ".\cmd\cpsinglesmoke" },
    @{ Name = "frontendwssmoke"; Path = ".\cmd\frontendwssmoke" },
    @{ Name = "mockhooks";       Path = ".\cmd\mockhooks" }
)

foreach ($target in $targets) {
    $out = ".\bin\$($target.Name).exe"
    Write-Host "Building $out"
    go build -o $out $target.Path
}

Write-Host ""
Write-Host "===== build complete ====="

Get-ChildItem ".\bin" -Filter "*.exe" |
    Select-Object FullName, Length, LastWriteTime |
    Format-Table -AutoSize
