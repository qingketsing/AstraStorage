# Stop Docker cluster
# Encoding: UTF-8

Write-Host "Stopping Docker cluster..." -ForegroundColor Yellow

# Check docker compose command (support both old and new syntax)
$dockerComposeCmd = ""
if (Get-Command docker-compose -ErrorAction SilentlyContinue) {
    $dockerComposeCmd = "docker-compose"
} elseif (docker compose version 2>$null) {
    $dockerComposeCmd = "docker compose"
} else {
    Write-Host "Error: Docker Compose not found!" -ForegroundColor Red
    exit 1
}

# Navigate to project root
$scriptDir = $PSScriptRoot
if (-not $scriptDir) {
    $scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
}
$projectRoot = Split-Path -Parent $scriptDir
Set-Location $projectRoot

# Stop all containers
& $dockerComposeCmd down

Write-Host "Cluster stopped" -ForegroundColor Green

Write-Host "`nTo remove volumes (delete all data):" -ForegroundColor Cyan
Write-Host "  $dockerComposeCmd down -v" -ForegroundColor White
