# Test cluster status script
# Encoding: UTF-8

$nodes = @(
    @{id="node-0"; port=9081},
    @{id="node-1"; port=9082},
    @{id="node-2"; port=9083},
    @{id="node-3"; port=9084},
    @{id="node-4"; port=9085}
)

Write-Host "Checking cluster status...`n" -ForegroundColor Cyan

$leaderInfo = $null
$onlineCount = 0

foreach ($node in $nodes) {
    Write-Host "[$($node.id)]" -ForegroundColor Yellow -NoNewline
    
    try {
        $response = Invoke-RestMethod -Uri "http://localhost:$($node.port)/status" -Method Get -TimeoutSec 2
        
        $onlineCount++
        if ($response.isLeader) {
            $leaderInfo = @{
                id = $response.id
                term = $response.term
                address = $response.address
            }
        }
        
        $leaderBadge = if ($response.isLeader) { " [LEADER]" } else { "" }
        Write-Host "$leaderBadge" -ForegroundColor $(if ($response.isLeader) { "Green" } else { "Gray" })
        Write-Host "  Term:    $($response.term)" -ForegroundColor Gray
        Write-Host "  Address: $($response.address)" -ForegroundColor Gray
        $timestamp = [DateTimeOffset]::FromUnixTimeSeconds($response.timestamp).LocalDateTime
        Write-Host "  Time:    $($timestamp.ToString('yyyy-MM-dd HH:mm:ss'))" -ForegroundColor Gray
    }
    catch {
        Write-Host " [OFFLINE]" -ForegroundColor Red
        Write-Host "  Error: $($_.Exception.Message)" -ForegroundColor DarkRed
    }
    
    Write-Host ""
}

# Display cluster summary
Write-Host "========================================" -ForegroundColor Cyan
if ($leaderInfo) {
    Write-Host "Current Leader: $($leaderInfo.id) (Term: $($leaderInfo.term))" -ForegroundColor Green
    Write-Host "Leader Address: $($leaderInfo.address)" -ForegroundColor Gray
} else {
    Write-Host "No Leader elected or detected" -ForegroundColor Red
}
Write-Host "Online Nodes: $onlineCount / $($nodes.Count)" -ForegroundColor $(if ($onlineCount -eq $nodes.Count) { "Green" } else { "Yellow" })
Write-Host "========================================" -ForegroundColor Cyan

# Display metrics
Write-Host "`nFetching node metrics..." -ForegroundColor Cyan
foreach ($node in $nodes) {
    try {
        $metrics = Invoke-RestMethod -Uri "http://localhost:$($node.port)/metrics" -Method Get -TimeoutSec 2
        
        Write-Host "[$($node.id)] System Metrics:" -ForegroundColor Yellow
        Write-Host "  CPU:         $([math]::Round($metrics.cpu_percent, 2))%" -ForegroundColor Gray
        Write-Host "  Memory Used: $([math]::Round($metrics.memory_used_mb, 2)) MB" -ForegroundColor Gray
        Write-Host "  Memory %:    $([math]::Round($metrics.memory_percent, 2))%" -ForegroundColor Gray
        Write-Host "  Disk Used:   $([math]::Round($metrics.disk_used_gb, 2)) GB" -ForegroundColor Gray
        Write-Host "  Disk %:      $([math]::Round($metrics.disk_percent, 2))%" -ForegroundColor Gray
        
        if ($metrics.bandwidth_mbps -ne $null) {
            Write-Host "  Bandwidth:   $([math]::Round($metrics.bandwidth_mbps, 2)) Mbps" -ForegroundColor Gray
        }
        
        Write-Host ""
    }
    catch {
        Write-Host "[$($node.id)] Cannot fetch metrics: $($_.Exception.Message)" -ForegroundColor Red
    }
}
