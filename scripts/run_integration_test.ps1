# 文件上传集成测试脚本
# 自动启动集群、运行测试、清理环境

param(
    [switch]$SkipClusterStart,  # 跳过集群启动（如果已经运行）
    [switch]$KeepCluster,       # 测试后保持集群运行
    [switch]$Verbose            # 显示详细日志
)

$ErrorActionPreference = "Stop"

# 颜色输出函数
function Write-ColorOutput {
    param(
        [string]$Message,
        [string]$Color = "White"
    )
    Write-Host $Message -ForegroundColor $Color
}

function Write-Success { param([string]$Message) Write-ColorOutput "✓ $Message" "Green" }
function Write-Error { param([string]$Message) Write-ColorOutput "✗ $Message" "Red" }
function Write-Info { param([string]$Message) Write-ColorOutput "ℹ $Message" "Cyan" }
function Write-Step { param([string]$Message) Write-ColorOutput "`n=== $Message ===" "Yellow" }

# 检查 Docker 是否运行
function Test-DockerRunning {
    try {
        docker ps | Out-Null
        return $true
    } catch {
        return $false
    }
}

# 检查集群是否运行
function Test-ClusterRunning {
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

# 主流程
try {
    Write-ColorOutput @"

╔════════════════════════════════════════════════════════╗
║     文件上传集成测试 - 自动化测试脚本                   ║
╚════════════════════════════════════════════════════════╝

"@ "Cyan"

    # 步骤1：检查 Docker
    Write-Step "检查 Docker 环境"
    if (-not (Test-DockerRunning)) {
        Write-Error "Docker Desktop 未运行"
        Write-Info "请启动 Docker Desktop 后重试"
        exit 1
    }
    Write-Success "Docker Desktop 正在运行"

    # 步骤2：启动集群
    if (-not $SkipClusterStart) {
        Write-Step "启动 Docker 集群"
        
        if (Test-ClusterRunning) {
            Write-Info "检测到集群已在运行"
            $response = Read-Host "是否重启集群？(y/N)"
            if ($response -eq 'y' -or $response -eq 'Y') {
                Write-Info "停止现有集群..."
                & ".\scripts\stop_docker_cluster.ps1"
                Start-Sleep -Seconds 3
            } else {
                Write-Success "使用现有集群"
            }
        }
        
        if (-not (Test-ClusterRunning)) {
            Write-Info "启动集群容器..."
            & ".\scripts\start_docker_cluster.ps1"
            
            if ($LASTEXITCODE -ne 0) {
                Write-Error "集群启动失败"
                exit 1
            }
            
            Write-Success "集群启动成功"
            
            # 等待集群就绪
            Write-Info "等待集群初始化和 Leader 选举..."
            for ($i = 30; $i -gt 0; $i--) {
                Write-Host -NoNewline "`r  剩余时间: $i 秒   "
                Start-Sleep -Seconds 1
            }
            Write-Host ""
            Write-Success "集群初始化完成"
        }
    } else {
        Write-Step "跳过集群启动"
        if (-not (Test-ClusterRunning)) {
            Write-Error "集群未运行，请先启动集群或移除 -SkipClusterStart 参数"
            exit 1
        }
        Write-Success "检测到集群正在运行"
    }

    # 步骤3：验证集群状态
    Write-Step "验证集群状态"
    Write-Info "检查容器健康状态..."
    
    $containers = @{
        "redis" = "Redis"
        "rabbitmq" = "RabbitMQ"
        "postgres-0" = "PostgreSQL Node 0"
        "multi-driver-node-0" = "Storage Node 0"
    }
    
    foreach ($container in $containers.Keys) {
        $status = docker inspect -f '{{.State.Status}}' $container 2>$null
        if ($status -eq "running") {
            Write-Success "$($containers[$container]): 运行中"
        } else {
            Write-Error "$($containers[$container]): $status"
        }
    }

    # 步骤4：运行测试
    Write-Step "运行集成测试"
    Write-Info "测试文件: tests/file_upload_test.go"
    Write-Info "测试函数: TestFileUploadIntegration"
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
        Write-Step "测试结果"
        Write-Success "所有测试通过！ 🎉"
    } else {
        Write-Step "测试结果"
        Write-Error "测试失败，请查看上面的错误信息"
    }

    # 步骤5：清理或保持集群
    if ($testExitCode -eq 0 -and -not $KeepCluster) {
        Write-Host ""
        $response = Read-Host "是否停止集群？(Y/n)"
        if ($response -ne 'n' -and $response -ne 'N') {
            Write-Step "清理环境"
            Write-Info "停止集群..."
            & ".\scripts\stop_docker_cluster.ps1"
            Write-Success "集群已停止"
        } else {
            Write-Info "集群保持运行状态"
            Write-Info "手动停止: .\scripts\stop_docker_cluster.ps1"
        }
    } elseif ($KeepCluster) {
        Write-Host ""
        Write-Info "集群保持运行状态（-KeepCluster）"
        Write-Info "查看日志: .\scripts\view_docker_logs.ps1"
        Write-Info "停止集群: .\scripts\stop_docker_cluster.ps1"
    }

    Write-Host ""
    Write-ColorOutput "════════════════════════════════════════════════════════" "Cyan"
    
    exit $testExitCode

} catch {
    Write-Host ""
    Write-Error "发生错误: $_"
    Write-Info "详细错误信息："
    Write-Host $_.Exception.Message -ForegroundColor Red
    Write-Host $_.ScriptStackTrace -ForegroundColor DarkGray
    exit 1
}
