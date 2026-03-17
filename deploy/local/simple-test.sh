#!/bin/bash

# 简化测试脚本
set -e

# 颜色定义
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

print_success() {
    echo -e "${GREEN}✅ $1${NC}"
}

print_error() {
    echo -e "${RED}❌ $1${NC}"
}

print_info() {
    echo -e "${BLUE}ℹ️  $1${NC}"
}

print_warning() {
    echo -e "${YELLOW}⚠️  $1${NC}"
}

print_info "开始简化本地测试..."

# 清理函数
cleanup() {
    print_info "清理测试环境..."
    pkill -f transcribe_ws 2>/dev/null || true
    rm -f /tmp/gonotic-transcribe.pid
}

# 捕获退出信号
trap cleanup EXIT

# 构建项目
print_info "构建项目..."
cd "$(dirname "$0")/../.."
./scripts.sh build
print_success "项目构建完成"

# 准备环境变量
export GIN_MODE=debug
export SERVER_HOST=127.0.0.1
export SERVER_PORT=8090
export LOG_LEVEL=debug
export JWT_SECRET=test-secret-key-for-local-development

# 启动 gonotic-transcribe 服务
print_info "启动 gonotic-transcribe 服务 (端口 8090)..."
./bin/transcribe_ws &
SERVICE_PID=$!
echo $SERVICE_PID > /tmp/gonotic-transcribe.pid

# 等待服务启动
print_info "等待服务启动..."
sleep 3

# 检查服务是否启动成功
if kill -0 "$SERVICE_PID" 2>/dev/null; then
    print_success "gonotic-transcribe 服务启动成功 (PID: $SERVICE_PID)"
else
    print_error "gonotic-transcribe 服务启动失败"
    exit 1
fi

# 测试服务
print_info "测试服务..."

# 测试 HTTP 请求
if curl -s "http://localhost:8090/ws/transcription" 2>/dev/null | grep -q "Upgrade"; then
    print_success "WebSocket 升级响应正常"
else
    print_warning "WebSocket 升级测试可能失败，但服务可能仍然正常"
fi

# 显示服务信息
echo ""
print_success "🎉 本地服务启动完成！"
echo ""
echo "服务访问信息:"
echo "  🔗 WebSocket: ws://localhost:8090/ws/transcription"
echo "  📊 服务状态: http://localhost:8090/"
echo ""
echo "服务进程:"
echo "  📝 gonotic-transcribe: PID $SERVICE_PID"
echo ""
echo "测试命令:"
echo "  🧪 WebSocket 测试: wscat -c ws://localhost:8090/ws/transcription"
echo "  🔍 查看进程: ps aux | grep transcribe_ws"
echo "  📋 停止服务: kill $SERVICE_PID"
echo ""
echo "测试消息示例:"
echo '  {"type":"start","data":{"meeting_id":1},"timestamp":"2025-03-16T22:00:00Z"}'
echo ""

# 保持脚本运行，等待用户中断
print_info "服务运行中... 按 Ctrl+C 停止"
while true; do
    # 检查服务是否还在运行
    if ! kill -0 "$SERVICE_PID" 2>/dev/null; then
        print_error "服务意外停止"
        break
    fi
    sleep 5
done
