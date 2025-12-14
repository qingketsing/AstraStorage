# Start 3 nodes in separate windows
# Encoding: UTF-8

$ErrorActionPreference = "Stop"

# Build latest version
Write-Host "Building node program..." -ForegroundColor Cyan
$env:GOFLAGS = '-buildvcs=false'
go build -o bin/node.exe ./cmd/node
if ($LASTEXITCODE -ne 0) {
    Write-Host "Build failed" -ForegroundColor Red
    exit 1
}

# Node configuration
$nodes = @(
    @{id="node-0"; addr="127.0.0.1:29001"; me=0; peers="127.0.0.1:29002,127.0.0.1:29003,127.0.0.1:29004,127.0.0.1:29005"; healthPort=9081},
    @{id="node-1"; addr="127.0.0.1:29002"; me=1; peers="127.0.0.1:29001,127.0.0.1:29003,127.0.0.1:29004,127.0.0.1:29005"; healthPort=9082},
    @{id="node-2"; addr="127.0.0.1:29003"; me=2; peers="127.0.0.1:29001,127.0.0.1:29002,127.0.0.1:29004,127.0.0.1:29005"; healthPort=9083},
    @{id="node-3"; addr="127.0.0.1:29004"; me=3; peers="127.0.0.1:29001,127.0.0.1:29002,127.0.0.1:29003,127.0.0.1:29005"; healthPort=9084},
    @{id="node-4"; addr="127.0.0.1:29005"; me=4; peers="127.0.0.1:29001,127.0.0.1:29002,127.0.0.1:29003,127.0.0.1:29004"; healthPort=9085}
)

Write-Host "`nStarting cluster nodes in separate windows..." -ForegroundColor Cyan

foreach ($node in $nodes) {
    $cmd = "cd '$PWD'; .\bin\node.exe -id $($node.id) -addr $($node.addr) -me $($node.me) -peers '$($node.peers)' -health-port $($node.healthPort)"
    
    Start-Process powershell -ArgumentList "-NoExit", "-Command", $cmd
    
    Write-Host "  Started $($node.id) (Health: http://localhost:$($node.healthPort))" -ForegroundColor Green
    Start-Sleep -Milliseconds 500
}

Write-Host "\nCluster started in 5 separate windows!" -ForegroundColor Green
Write-Host "`nHealth check endpoints:" -ForegroundColor Cyan
foreach ($node in $nodes) {
    Write-Host "  $($node.id): http://localhost:$($node.healthPort)/status" -ForegroundColor Gray
}

Write-Host "`nWait a few seconds for leader election, then run:" -ForegroundColor Yellow
Write-Host "  .\scripts\test_cluster.ps1" -ForegroundColor White

Write-Host "`nTo stop all nodes:" -ForegroundColor Yellow
Write-Host "  Get-Process node | Stop-Process" -ForegroundColor White
