# Start 5-node cluster using Docker Compose
# Encoding: UTF-8

$ErrorActionPreference = "Stop"

Write-Host "Starting Docker cluster..." -ForegroundColor Cyan
Write-Host ""

# Check if Docker is running
$dockerRunning = $false
try {
    $dockerInfo = docker info 2>&1
    if ($LASTEXITCODE -eq 0) {
        $dockerRunning = $true
    }
}
catch {}

if (-not $dockerRunning) {
    Write-Host "Error: Docker Desktop is not running!" -ForegroundColor Red
    Write-Host ""
    Write-Host "Please:" -ForegroundColor Yellow
    Write-Host "  1. Start Docker Desktop" -ForegroundColor White
    Write-Host "  2. Wait for it to fully start" -ForegroundColor White
    Write-Host "  3. Run this script again" -ForegroundColor White
    Write-Host ""
    exit 1
}

# Check docker compose command (support both old and new syntax)
$dockerComposeCmd = ""
if (Get-Command docker-compose -ErrorAction SilentlyContinue) {
    $dockerComposeCmd = "docker-compose"
} elseif (docker compose version 2>$null) {
    $dockerComposeCmd = "docker compose"
} else {
    Write-Host "Error: Docker Compose not found!" -ForegroundColor Red
    Write-Host "Please install Docker Compose and try again." -ForegroundColor Yellow
    exit 1
}

Write-Host "Using Docker Compose command: $dockerComposeCmd" -ForegroundColor Gray

# Navigate to project root (parent of scripts directory)
$scriptDir = $PSScriptRoot
if (-not $scriptDir) {
    $scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
}
$projectRoot = Split-Path -Parent $scriptDir
Write-Host "Changing to project root: $projectRoot" -ForegroundColor Gray
Set-Location $projectRoot

# Verify docker-compose.yml exists
if (-not (Test-Path "docker-compose.yml")) {
    Write-Host "Error: docker-compose.yml not found in $projectRoot" -ForegroundColor Red
    Write-Host "Current directory: $(Get-Location)" -ForegroundColor Yellow
    exit 1
}

# Stop and remove existing containers
Write-Host "Cleaning up existing containers..." -ForegroundColor Yellow
try {
    & $dockerComposeCmd down -v 2>&1 | Out-Null
} catch {
    Write-Host "Warning: Cleanup encountered issues (this is usually ok)" -ForegroundColor Yellow
}

# Build and start containers
Write-Host "Building Docker images..." -ForegroundColor Cyan
& $dockerComposeCmd build

Write-Host "`nStarting cluster nodes..." -ForegroundColor Cyan
& $dockerComposeCmd up -d

# Wait for containers to start
Write-Host "`nWaiting for nodes to start..." -ForegroundColor Yellow
Start-Sleep -Seconds 8

# Check container status
Write-Host "`nContainer Status:" -ForegroundColor Cyan
& $dockerComposeCmd ps

Write-Host "`n========================================" -ForegroundColor Green
Write-Host "Cluster started successfully!" -ForegroundColor Green
Write-Host "========================================" -ForegroundColor Green

Write-Host "`nHealth check endpoints:" -ForegroundColor Cyan
Write-Host "  node-0: http://localhost:9081/status" -ForegroundColor Gray
Write-Host "  node-1: http://localhost:9082/status" -ForegroundColor Gray
Write-Host "  node-2: http://localhost:9083/status" -ForegroundColor Gray
Write-Host "  node-3: http://localhost:9084/status" -ForegroundColor Gray
Write-Host "  node-4: http://localhost:9085/status" -ForegroundColor Gray

Write-Host "`nUseful commands:" -ForegroundColor Cyan
Write-Host "  Test cluster:      .\scripts\test_cluster.ps1" -ForegroundColor White
Write-Host "  View logs:         docker-compose logs -f" -ForegroundColor White
Write-Host "  View node logs:    docker-compose logs -f node-0" -ForegroundColor White
Write-Host "  Stop cluster:      .\scripts\stop_docker_cluster.ps1" -ForegroundColor White
Write-Host "  Restart node:      docker-compose restart node-0" -ForegroundColor White

Write-Host "`nWait a few seconds for leader election, then run:" -ForegroundColor Yellow
Write-Host "  .\scripts\test_cluster.ps1" -ForegroundColor White
Write-Host ""
