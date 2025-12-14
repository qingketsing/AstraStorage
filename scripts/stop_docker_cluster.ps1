# Stop Docker cluster
# Encoding: UTF-8

Write-Host "Stopping Docker cluster..." -ForegroundColor Yellow

docker-compose down

Write-Host "Cluster stopped" -ForegroundColor Green

Write-Host "`nTo remove volumes (delete all data):" -ForegroundColor Cyan
Write-Host "  docker-compose down -v" -ForegroundColor White
