# KRADCHAT build script (Windows)
# Backend + WASM frontend'ni yig'adi.
param(
    [switch]$Release
)

$ErrorActionPreference = 'Stop'

Write-Host "==> Backend build..."
go build -o kradchat.exe ./cmd/server
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }

Write-Host "==> WASM frontend build..."
$env:GOOS = 'js'
$env:GOARCH = 'wasm'
if ($Release) {
    go build -ldflags "-s -w" -o web/wasm/app.wasm ./web/src
} else {
    go build -o web/wasm/app.wasm ./web/src
}
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }

Write-Host "==> wasm_exec.js..."
Copy-Item "$(go env GOROOT)\lib\wasm\wasm_exec.js" web\wasm\wasm_exec.js -Force

# Fayl papkalari
New-Item -ItemType Directory -Force -Path web\static\uploads | Out-Null
New-Item -ItemType Directory -Force -Path web\static\backgrounds | Out-Null

Write-Host "==> Tayyor! Ishga tushirish: .\kradchat.exe"
