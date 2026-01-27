# Restart Docker cluster with full cleanup
# Encoding: UTF-8
# This script performs a complete restart with cleanup

param(
    [switch]$Quick  # Skip full cleanup and rebuild
)

$ErrorActionPreference = "Stop"

Write-Host ""
Write-Host "╔══════════════════════════════════════════╗" -ForegroundColor Cyan
Write-Host "║   Docker Cluster Restart Script         ║" -ForegroundColor Cyan
Write-Host "╚══════════════════════════════════════════╝" -ForegroundColor Cyan
Write-Host ""

# Navigate to scripts directory
$scriptDir = $PSScriptRoot
if (-not $scriptDir) {
    $scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
}

if (-not $Quick) {
    Write-Host "Performing full cleanup and rebuild..." -ForegroundColor Yellow
    Write-Host ""
    
    # Step 1: Full cleanup
    Write-Host "Step 1/2: Cleaning up Docker resources..." -ForegroundColor Cyan
    & "$scriptDir\cleanup_docker.ps1"
    
    if ($LASTEXITCODE -ne 0) {
        Write-Host "Cleanup failed!" -ForegroundColor Red
        exit 1
    }
    
    Start-Sleep -Seconds 3
    
    # Step 2: Start cluster
    Write-Host "Step 2/2: Starting cluster..." -ForegroundColor Cyan
    & "$scriptDir\start_docker_cluster.ps1"
    
    if ($LASTEXITCODE -ne 0) {
        Write-Host "Cluster start failed!" -ForegroundColor Red
        exit 1
    }
} else {
    Write-Host "Performing quick restart..." -ForegroundColor Yellow
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
    $projectRoot = Split-Path -Parent $scriptDir
    Set-Location $projectRoot
    
    Write-Host "Restarting containers..." -ForegroundColor Cyan
    & $dockerComposeCmd restart
    
    Write-Host ""
    Write-Host "Waiting for services to stabilize..." -ForegroundColor Yellow
    Start-Sleep -Seconds 15
    
    Write-Host ""
    Write-Host "Container Status:" -ForegroundColor Cyan
    & $dockerComposeCmd ps
}

Write-Host ""
Write-Host "══════════════════════════════════════════" -ForegroundColor Green
Write-Host "Cluster restart complete!" -ForegroundColor Green
Write-Host "══════════════════════════════════════════" -ForegroundColor Green
Write-Host ""
