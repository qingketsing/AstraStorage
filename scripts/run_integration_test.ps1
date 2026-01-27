# 鏂囦欢涓婁紶闆嗘垚娴嬭瘯鑴氭湰
# 鑷姩鍚姩闆嗙兢銆佽繍琛屾祴璇曘€佹竻鐞嗙幆澧?
param(
    [switch]$SkipClusterStart,  # 璺宠繃闆嗙兢鍚姩锛堝鏋滃凡缁忚繍琛岋級
    [switch]$KeepCluster,       # 娴嬭瘯鍚庝繚鎸侀泦缇よ繍琛?    [switch]$Verbose            # 鏄剧ず璇︾粏鏃ュ織
)

$ErrorActionPreference = "Stop"

# 棰滆壊杈撳嚭鍑芥暟
function Write-ColorOutput {
    param(
        [string]$Message,
        [string]$Color = "White"
    )
    Write-Host $Message -ForegroundColor $Color
}

function Write-Success { param([string]$Message) Write-ColorOutput "鉁?$Message" "Green" }
function Write-Error { param([string]$Message) Write-ColorOutput "鉁?$Message" "Red" }
function Write-Info { param([string]$Message) Write-ColorOutput "鈩?$Message" "Cyan" }
function Write-Step { param([string]$Message) Write-ColorOutput "`n=== $Message ===" "Yellow" }

# 妫€鏌?Docker 鏄惁杩愯
function Test-DockerRunning {
    try {
        docker ps | Out-Null
        return $true
    } catch {
        return $false
    }
}

# 妫€鏌ラ泦缇ゆ槸鍚﹁繍琛?function Test-ClusterRunning {
    try {
        $containers = docker-compose ps -q
        if ($containers.Count -ge 5) {
            return $true
        }
        return $false
    } catch {
        return $false
    }
}

# 涓绘祦绋?try {
    Write-ColorOutput @"

鈺斺晲鈺愨晲鈺愨晲鈺愨晲鈺愨晲鈺愨晲鈺愨晲鈺愨晲鈺愨晲鈺愨晲鈺愨晲鈺愨晲鈺愨晲鈺愨晲鈺愨晲鈺愨晲鈺愨晲鈺愨晲鈺愨晲鈺愨晲鈺愨晲鈺愨晲鈺愨晲鈺愨晲鈺愨晲鈺愨晲鈺愨晲鈺愨晲鈺愨晽
鈺?    鏂囦欢涓婁紶闆嗘垚娴嬭瘯 - 鑷姩鍖栨祴璇曡剼鏈?                  鈺?鈺氣晲鈺愨晲鈺愨晲鈺愨晲鈺愨晲鈺愨晲鈺愨晲鈺愨晲鈺愨晲鈺愨晲鈺愨晲鈺愨晲鈺愨晲鈺愨晲鈺愨晲鈺愨晲鈺愨晲鈺愨晲鈺愨晲鈺愨晲鈺愨晲鈺愨晲鈺愨晲鈺愨晲鈺愨晲鈺愨晲鈺愨晲鈺愨晲鈺愨暆

"@ "Cyan"

    # 姝ラ1锛氭鏌?Docker
    Write-Step "妫€鏌?Docker 鐜"
    if (-not (Test-DockerRunning)) {
        Write-Error "Docker Desktop 鏈繍琛?
        Write-Info "璇峰惎鍔?Docker Desktop 鍚庨噸璇?
        exit 1
    }
    Write-Success "Docker Desktop 姝ｅ湪杩愯"

    # 姝ラ2锛氬惎鍔ㄩ泦缇?    if (-not $SkipClusterStart) {
        Write-Step "鍚姩 Docker 闆嗙兢"
        
        if (Test-ClusterRunning) {
            Write-Info "妫€娴嬪埌闆嗙兢宸插湪杩愯"
            $response = Read-Host "鏄惁閲嶅惎闆嗙兢锛?y/N)"
            if ($response -eq 'y' -or $response -eq 'Y') {
                Write-Info "鍋滄鐜版湁闆嗙兢..."
                & ".\scripts\stop_docker_cluster.ps1"
                Start-Sleep -Seconds 3
            } else {
                Write-Success "浣跨敤鐜版湁闆嗙兢"
            }
        }
        
        if (-not (Test-ClusterRunning)) {
            Write-Info "鍚姩闆嗙兢瀹瑰櫒..."
            & ".\scripts\start_docker_cluster.ps1"
            
            if ($LASTEXITCODE -ne 0) {
                Write-Error "闆嗙兢鍚姩澶辫触"
                exit 1
            }
            
            Write-Success "闆嗙兢鍚姩鎴愬姛"
            
            # 绛夊緟闆嗙兢灏辩华
            Write-Info "绛夊緟闆嗙兢鍒濆鍖栧拰 Leader 閫変妇..."
            for ($i = 30; $i -gt 0; $i--) {
                Write-Host -NoNewline "`r  鍓╀綑鏃堕棿: $i 绉?  "
                Start-Sleep -Seconds 1
            }
            Write-Host ""
            Write-Success "闆嗙兢鍒濆鍖栧畬鎴?
        }
    } else {
        Write-Step "璺宠繃闆嗙兢鍚姩"
        if (-not (Test-ClusterRunning)) {
            Write-Error "闆嗙兢鏈繍琛岋紝璇峰厛鍚姩闆嗙兢鎴栫Щ闄?-SkipClusterStart 鍙傛暟"
            exit 1
        }
        Write-Success "妫€娴嬪埌闆嗙兢姝ｅ湪杩愯"
    }

    # 姝ラ3锛氶獙璇侀泦缇ょ姸鎬?    Write-Step "楠岃瘉闆嗙兢鐘舵€?
    Write-Info "妫€鏌ュ鍣ㄥ仴搴风姸鎬?.."
    
    $containers = @{
        "redis" = "Redis"
        "rabbitmq" = "RabbitMQ"
        "postgres-0" = "PostgreSQL Node 0"
        "multi-driver-node-0" = "Storage Node 0"
    }
    
    foreach ($container in $containers.Keys) {
        $status = docker inspect -f '{{.State.Status}}' $container 2>$null
        if ($status -eq "running") {
            Write-Success "$($containers[$container]): 杩愯涓?
        } else {
            Write-Error "$($containers[$container]): $status"
        }
    }

    # 姝ラ4锛氳繍琛屾祴璇?    Write-Step "杩愯闆嗘垚娴嬭瘯"
    Write-Info "娴嬭瘯鏂囦欢: tests/file_upload_test.go"
    Write-Info "娴嬭瘯鍑芥暟: TestFileUploadIntegration"
    Write-Host ""
    
    $testArgs = @(
        "test",
        "-v",
        "./tests",
        "-run",
        "TestFileUploadIntegration",
        "-timeout",
        "60s"
    )
    
    if ($Verbose) {
        $testArgs += "-test.v"
    }
    
    & go @testArgs
    $testExitCode = $LASTEXITCODE

    Write-Host ""
    
    if ($testExitCode -eq 0) {
        Write-Step "娴嬭瘯缁撴灉"
        Write-Success "鎵€鏈夋祴璇曢€氳繃锛?馃帀"
    } else {
        Write-Step "娴嬭瘯缁撴灉"
        Write-Error "娴嬭瘯澶辫触锛岃鏌ョ湅涓婇潰鐨勯敊璇俊鎭?
    }

    # 姝ラ5锛氭竻鐞嗘垨淇濇寔闆嗙兢
    if ($testExitCode -eq 0 -and -not $KeepCluster) {
        Write-Host ""
        $response = Read-Host "鏄惁鍋滄闆嗙兢锛?Y/n)"
        if ($response -ne 'n' -and $response -ne 'N') {
            Write-Step "娓呯悊鐜"
            Write-Info "鍋滄闆嗙兢..."
            & ".\scripts\stop_docker_cluster.ps1"
            Write-Success "闆嗙兢宸插仠姝?
        } else {
            Write-Info "闆嗙兢淇濇寔杩愯鐘舵€?
            Write-Info "鎵嬪姩鍋滄: .\scripts\stop_docker_cluster.ps1"
        }
    } elseif ($KeepCluster) {
        Write-Host ""
        Write-Info "闆嗙兢淇濇寔杩愯鐘舵€侊紙-KeepCluster锛?
        Write-Info "鏌ョ湅鏃ュ織: .\scripts\view_docker_logs.ps1"
        Write-Info "鍋滄闆嗙兢: .\scripts\stop_docker_cluster.ps1"
    }

    Write-Host ""
    Write-ColorOutput "鈺愨晲鈺愨晲鈺愨晲鈺愨晲鈺愨晲鈺愨晲鈺愨晲鈺愨晲鈺愨晲鈺愨晲鈺愨晲鈺愨晲鈺愨晲鈺愨晲鈺愨晲鈺愨晲鈺愨晲鈺愨晲鈺愨晲鈺愨晲鈺愨晲鈺愨晲鈺愨晲鈺愨晲鈺愨晲鈺愨晲鈺愨晲鈺愨晲" "Cyan"
    
    exit $testExitCode

} catch {
    Write-Host ""
    Write-Error "鍙戠敓閿欒: $_"
    Write-Info "璇︾粏閿欒淇℃伅锛?
    Write-Host $_.Exception.Message -ForegroundColor Red
    Write-Host $_.ScriptStackTrace -ForegroundColor DarkGray
    exit 1
}
