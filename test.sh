#!/bin/bash

# 测试脚本
echo "🧪 运行 gonotic-transcribe 测试..."

# 运行单元测试
echo "📋 运行单元测试..."
go test ./internal/service/... -v

# 运行集成测试（如果存在）
if [ -d "test/integration" ]; then
    echo "🔗 运行集成测试..."
    go test ./test/integration/... -v -tags=integration
fi

# 构建检查
echo "🔨 检查构建..."
go build ./cmd/transcribe_ws

echo "✅ 所有测试完成！"
