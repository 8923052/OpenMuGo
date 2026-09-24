# 协议版本升级 / 生成物刷新脚本。
# 前置：Go 1.23+、.NET 10 SDK（仅刷新 golden 向量时需要）。
# 用法：
#   ./tools/regen.ps1                 # 仅按 spec 重新生成 Go 代码并测试
#   ./tools/regen.ps1 -Golden         # 同时用官方 NuGet 包重刷协议 golden 向量
#   ./tools/regen.ps1 -Crypto         # 同时重刷 SimpleModulus/Xor32 交叉向量
#   ./tools/regen.ps1 -Items          # 同时重刷物品12B/外观18B 交叉向量（含 SmallAxe 锚点自校验）
#   ./tools/regen.ps1 -Version 0.9.11 # 升级到新版本（需先把新版 XML 放入 spec/<版本>）
param(
    [string]$Version = "",
    [switch]$Golden,
    [switch]$Crypto,
    [switch]$Items
)

$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot
Set-Location $root

if ($Version -ne "") {
    $cfg = Get-Content protocol.gen.json -Raw | ConvertFrom-Json
    $cfg.packets_version = $Version
    $cfg.xml_root = "spec/$Version"
    $cfg | ConvertTo-Json -Depth 8 | Set-Content protocol.gen.json -Encoding utf8NoBOM
    Write-Host "已将 protocol.gen.json 切换到 $Version；请先把该版本 NuGet contentFiles 的 XML 放入 spec/$Version" -ForegroundColor Yellow
}

go run ./cmd/muprotogen
if ($LASTEXITCODE -ne 0) { exit 1 }

if ($Golden) {
    dotnet run --project tools/golden -- "$root/internal/proto/s2c/testdata/golden" s2c
    if ($LASTEXITCODE -ne 0) { exit 1 }
    dotnet run --project tools/golden -- "$root/internal/proto/c2s/testdata/golden" c2s
    if ($LASTEXITCODE -ne 0) { exit 1 }
}

if ($Crypto) {
    dotnet run --project tools/goldencrypto -- "$root/internal/transport/crypto/testdata"
    if ($LASTEXITCODE -ne 0) { exit 1 }
}

if ($Items) {
    dotnet run --project tools/goldenitems -- "$root/internal/view/remote/testdata"
    if ($LASTEXITCODE -ne 0) { exit 1 }
}

go test ./...
if ($LASTEXITCODE -ne 0) { exit 1 }
Write-Host "完成。请检查 git diff 确认协议变更面。" -ForegroundColor Green
