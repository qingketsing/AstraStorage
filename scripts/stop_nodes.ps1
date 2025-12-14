# Stop all node processes
# Encoding: UTF-8

Write-Host "Stopping all node processes..." -ForegroundColor Yellow

$nodes = Get-Process -Name "node" -ErrorAction SilentlyContinue

if ($nodes) {
    $nodes | Stop-Process -Force
    Write-Host "Stopped $($nodes.Count) node(s)" -ForegroundColor Green
} else {
    Write-Host "No node processes found" -ForegroundColor Gray
}
