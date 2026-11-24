$ErrorActionPreference = "Stop"

New-Item -ItemType Directory -Force -Path "bin" | Out-Null

Write-Host "Compilation de attack-shark-battery.exe (mode tray sans console)..." -ForegroundColor Cyan
go build -trimpath -ldflags="-H=windowsgui -s -w" -o "bin\attack-shark-battery.exe" .\cmd\tray

Write-Host "Compilation de attack-shark-battery-cli.exe (mode terminal)..." -ForegroundColor Cyan
go build -trimpath -ldflags="-s -w" -o "bin\attack-shark-battery-cli.exe" .\cmd\tray

$size = (Get-Item "bin\attack-shark-battery.exe").Length / 1MB
Write-Host ("OK -> bin\attack-shark-battery.exe ({0:N2} Mo)" -f $size) -ForegroundColor Green
