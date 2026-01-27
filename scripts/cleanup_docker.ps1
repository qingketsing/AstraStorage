# Clean up Docker resources
# Encoding: UTF-8
# This script completely removes all containers, volumes, and images related to the project

$ErrorActionPreference = "Stop"

Write-Host "Docker Cleanup Script" -ForegroundColor Cyan
Write-Host "=====================" -ForegroundColor Cyan
Write-Host ""

# Check docker compose command
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

Write-Host "Step 1: Stopping all containers..." -ForegroundColor Yellow
try {
    & $dockerComposeCmd down 2>&1 | Out-Null
} catch {
    Write-Host "  Warning: Some containers may not exist" -ForegroundColor DarkGray
}
Start-Sleep -Seconds 2

Write-Host "Step 2: Removing all volumes..." -ForegroundColor Yellow
try {
    & $dockerComposeCmd down -v 2>&1 | Out-Null
} catch {
    Write-Host "  Warning: Some volumes may not exist" -ForegroundColor DarkGray
}
Start-Sleep -Seconds 2

Write-Host "Step 3: Removing dangling images..." -ForegroundColor Yellow
try {
    docker image prune -f 2>&1 | Out-Null
} catch {
    Write-Host "  Warning: No dangling images found" -ForegroundColor DarkGray
}

Write-Host "Step 4: Removing unused volumes..." -ForegroundColor Yellow
try {
    docker volume prune -f 2>&1 | Out-Null
} catch {
    Write-Host "  Warning: No unused volumes found" -ForegroundColor DarkGray
}

Write-Host ""
Write-Host "Cleanup complete!" -ForegroundColor Green
Write-Host ""
Write-Host "To rebuild and start the cluster:" -ForegroundColor Cyan
Write-Host "  .\scripts\start_docker_cluster.ps1" -ForegroundColor White
Write-Host ""
