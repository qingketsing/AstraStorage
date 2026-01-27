# Docker Environment Diagnostic Script
# Encoding: UTF-8
# This script checks Docker environment and reports any potential issues

$ErrorActionPreference = "Continue"

function Write-Success { param([string]$Message) Write-Host "✓ $Message" -ForegroundColor Green }
function Write-Error { param([string]$Message) Write-Host "✗ $Message" -ForegroundColor Red }
function Write-Warning { param([string]$Message) Write-Host "⚠ $Message" -ForegroundColor Yellow }
function Write-Info { param([string]$Message) Write-Host "ℹ $Message" -ForegroundColor Cyan }
function Write-Section { param([string]$Message) Write-Host "`n=== $Message ===" -ForegroundColor Cyan }

Write-Host ""
Write-Host "╔══════════════════════════════════════════╗" -ForegroundColor Cyan
Write-Host "║   Docker Environment Diagnostics        ║" -ForegroundColor Cyan
Write-Host "╚══════════════════════════════════════════╝" -ForegroundColor Cyan

# Check Docker Desktop
Write-Section "Docker Desktop Status"
try {
    $dockerInfo = docker info 2>&1
    if ($LASTEXITCODE -eq 0) {
        Write-Success "Docker Desktop is running"
        
        # Get Docker version
        $dockerVersion = docker --version
        Write-Info "Version: $dockerVersion"
    } else {
        Write-Error "Docker Desktop is not running"
        Write-Warning "Please start Docker Desktop and try again"
        exit 1
    }
} catch {
    Write-Error "Docker is not installed or not accessible"
    exit 1
}

# Check Docker Compose
Write-Section "Docker Compose"
$composeFound = $false
if (Get-Command docker-compose -ErrorAction SilentlyContinue) {
    $composeVersion = docker-compose --version
    Write-Success "docker-compose command available: $composeVersion"
    $composeFound = $true
}
if (docker compose version 2>$null) {
    $composeVersion = docker compose version
    Write-Success "docker compose (plugin) available: $composeVersion"
    $composeFound = $true
}
if (-not $composeFound) {
    Write-Error "Docker Compose not found"
    exit 1
}

# Check existing containers
Write-Section "Existing Containers"
$containers = docker ps -a --filter "name=multi-driver" --format "{{.Names}}: {{.Status}}"
if ($containers) {
    Write-Info "Found existing containers:"
    foreach ($container in $containers) {
        Write-Host "  $container" -ForegroundColor Gray
    }
} else {
    Write-Info "No existing containers found"
}

# Check volumes
Write-Section "Docker Volumes"
$volumes = docker volume ls --filter "name=postgres" --format "{{.Name}}"
if ($volumes) {
    Write-Info "Found existing volumes:"
    foreach ($volume in $volumes) {
        $size = docker volume inspect $volume --format "{{.CreatedAt}}"
        Write-Host "  $volume (Created: $size)" -ForegroundColor Gray
    }
} else {
    Write-Info "No existing volumes found"
}

# Check networks
Write-Section "Docker Networks"
$network = docker network ls --filter "name=multi-driver-network" --format "{{.Name}}: {{.Driver}}"
if ($network) {
    Write-Info "Network: $network"
} else {
    Write-Info "Network not created yet"
}

# Check port availability
Write-Section "Port Availability"
$portsToCheck = @(
    @{Port=6379; Service="Redis"},
    @{Port=5672; Service="RabbitMQ"},
    @{Port=15672; Service="RabbitMQ Management"},
    @{Port=20000; Service="PostgreSQL-0"},
    @{Port=20001; Service="PostgreSQL-1"},
    @{Port=20002; Service="PostgreSQL-2"},
    @{Port=20003; Service="PostgreSQL-3"},
    @{Port=20004; Service="PostgreSQL-4"},
    @{Port=29001; Service="Node-0 RPC"},
    @{Port=29002; Service="Node-1 RPC"},
    @{Port=29003; Service="Node-2 RPC"},
    @{Port=29004; Service="Node-3 RPC"},
    @{Port=29005; Service="Node-4 RPC"}
)

$portsInUse = @()
foreach ($portInfo in $portsToCheck) {
    $port = $portInfo.Port
    $service = $portInfo.Service
    
    try {
        $connection = Test-NetConnection -ComputerName localhost -Port $port -WarningAction SilentlyContinue -InformationLevel Quiet
        if ($connection) {
            $portsInUse += $portInfo
            Write-Warning "Port $port ($service) is already in use"
        }
    } catch {
        # Port is free
    }
}

if ($portsInUse.Count -eq 0) {
    Write-Success "All required ports are available"
} else {
    Write-Warning "Some ports are in use. This might cause issues."
}

# Check disk space
Write-Section "Disk Space"
try {
    $drive = (Get-Location).Drive.Name + ":"
    $disk = Get-PSDrive $drive.TrimEnd(':')
    $freeGB = [math]::Round($disk.Free / 1GB, 2)
    $usedGB = [math]::Round($disk.Used / 1GB, 2)
    $totalGB = [math]::Round(($disk.Free + $disk.Used) / 1GB, 2)
    
    Write-Info "Drive $drive - Free: ${freeGB}GB / Used: ${usedGB}GB / Total: ${totalGB}GB"
    
    if ($freeGB -lt 5) {
        Write-Warning "Low disk space! Less than 5GB free"
    } else {
        Write-Success "Sufficient disk space available"
    }
} catch {
    Write-Warning "Could not check disk space"
}

# Check Docker resources
Write-Section "Docker Resources"
try {
    $cpus = (docker info --format '{{.NCPU}}' 2>$null)
    $memory = (docker info --format '{{.MemTotal}}' 2>$null)
    
    if ($cpus) {
        Write-Info "CPUs available to Docker: $cpus"
    }
    if ($memory) {
        $memoryGB = [math]::Round($memory / 1GB, 2)
        Write-Info "Memory available to Docker: ${memoryGB}GB"
        
        if ($memoryGB -lt 4) {
            Write-Warning "Low memory! Recommend at least 4GB for Docker"
        }
    }
} catch {
    Write-Warning "Could not check Docker resources"
}

# Summary
Write-Section "Summary"
Write-Info "Diagnostic complete"
Write-Host ""
Write-Host "Recommended actions:" -ForegroundColor Cyan
Write-Host "  1. If ports are in use, stop conflicting services" -ForegroundColor White
Write-Host "  2. Ensure at least 5GB free disk space" -ForegroundColor White
Write-Host "  3. Allocate at least 4GB RAM to Docker Desktop" -ForegroundColor White
Write-Host "  4. To clean up old resources: .\scripts\cleanup_docker.ps1" -ForegroundColor White
Write-Host "  5. To start cluster: .\scripts\start_docker_cluster.ps1" -ForegroundColor White
Write-Host ""
