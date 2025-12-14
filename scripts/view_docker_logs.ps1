# View real-time logs from all nodes
# Encoding: UTF-8

param(
    [string]$Node = ""
)

if ($Node) {
    Write-Host "Viewing logs for node-$Node..." -ForegroundColor Cyan
    docker-compose logs -f "node-$Node"
}
else {
    Write-Host "Viewing logs for all nodes..." -ForegroundColor Cyan
    Write-Host "Press Ctrl+C to stop" -ForegroundColor Yellow
    Write-Host ""
    docker-compose logs -f
}
