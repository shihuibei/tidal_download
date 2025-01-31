#!/bin/bash

# 创建 build 目录（如果不存在）
mkdir -p build

# 构建不同平台的二进制文件
echo "开始构建..."

# Linux AMD64
echo "构建 Linux AMD64 版本"
GOOS=linux GOARCH=amd64 go build -o build/tidal-download-linux-amd64 main.go

# Windows AMD64
echo "构建 Windows AMD64 版本"
GOOS=windows GOARCH=amd64 go build -o build/tidal-download-windows-amd64.exe main.go

# macOS AMD64
echo "构建 macOS AMD64 版本"
GOOS=darwin GOARCH=amd64 go build -o build/tidal-download-darwin-amd64 main.go

# macOS ARM64
echo "构建 macOS ARM64 版本"
GOOS=darwin GOARCH=arm64 go build -o build/tidal-download-darwin-arm64 main.go

echo "构建完成！"