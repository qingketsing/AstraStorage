# 快速测试脚本 - 一键运行完整测试

Write-Host @"

╔════════════════════════════════════════════════════════╗
║          文件上传集成测试 - 快速运行                     ║
╚════════════════════════════════════════════════════════╝

测试流程：
  1. 启动 Docker 集群
  2. 等待集群就绪
  3. 运行文件上传测试
  4. 显示测试结果
  5. 停止集群

按 Ctrl+C 可随时取消...

"@ -ForegroundColor Cyan

Start-Sleep -Seconds 2

# 运行完整测试
& ".\scripts\run_integration_test.ps1"

exit $LASTEXITCODE
