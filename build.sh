#!/usr/bin/env bash
# KRADCHAT build script (Linux/macOS)
set -e

echo "==> Backend build..."
go build -o kradchat ./cmd/server

echo "==> WASM frontend build..."
GOOS=js GOARCH=wasm go build -ldflags "-s -w" -o web/wasm/app.wasm ./web/src

echo "==> wasm_exec.js..."
cp "$(go env GOROOT)/lib/wasm/wasm_exec.js" web/wasm/wasm_exec.js

mkdir -p web/static/uploads web/static/backgrounds

echo "==> Tayyor! Ishga tushirish: ./kradchat"
