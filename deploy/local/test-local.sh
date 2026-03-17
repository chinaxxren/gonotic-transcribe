#!/bin/bash

# 本地测试脚本
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

# 配置变量
SERVICE_PORT=8090
PROXY_PORT=3000
HEALTH_PORT=3001
SERVICE_PID_FILE="/tmp/gonotic-transcribe.pid"
CADDY_PID_FILE="/tmp/caddy.pid"

print_info "开始本地测试环境..."

# 清理函数
cleanup() {
    print_info "清理测试环境..."
    
    # 停止 gonotic-transcribe
    if [ -f "$SERVICE_PID_FILE" ]; then
        SERVICE_PID=$(cat "$SERVICE_PID_FILE")
        if kill -0 "$SERVICE_PID" 2>/dev/null; then
            kill "$SERVICE_PID"
            print_success "gonotic-transcribe 已停止"
        fi
        rm -f "$SERVICE_PID_FILE"
    fi
    
    # 停止 Caddy
    if [ -f "$CADDY_PID_FILE" ]; then
        CADDY_PID=$(cat "$CADDY_PID_FILE")
        if kill -0 "$CADDY_PID" 2>/dev/null; then
            kill "$CADDY_PID"
            print_success "Caddy 已停止"
        fi
        rm -f "$CADDY_PID_FILE"
    fi
    
    # 清理端口占用
    pkill -f "transcribe_ws" 2>/dev/null || true
    pkill -f "caddy run" 2>/dev/null || true
}

# 捕获退出信号
trap cleanup EXIT

# 检查依赖
print_info "检查依赖..."
if ! command -v go >/dev/null 2>&1; then
    print_error "Go 未安装"
    exit 1
fi

if ! command -v caddy >/dev/null 2>&1; then
    print_error "Caddy 未安装，请先安装 Caddy"
    echo "安装命令: brew install caddy"
    exit 1
fi

print_success "依赖检查通过"

# 构建项目
print_info "构建项目..."
cd "$(dirname "$0")/../.."
./scripts.sh build
print_success "项目构建完成"

# 准备环境变量
print_info "准备环境变量..."
export GIN_MODE=development
export SERVER_HOST=127.0.0.1
export SERVER_PORT=$SERVICE_PORT
export LOG_LEVEL=debug
export JWT_SECRET=test-secret-key-for-local-development
export JWT_EXPIRATION=24h

# 创建日志目录
mkdir -p logs

# 启动 gonotic-transcribe 服务
print_info "启动 gonotic-transcribe 服务 (端口 $SERVICE_PORT)..."
./bin/transcribe_ws &
SERVICE_PID=$!
echo $SERVICE_PID > "$SERVICE_PID_FILE"

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

# 启动 Caddy
print_info "启动 Caddy 反向代理..."
cd "$(dirname "$0")"
caddy run --config Caddyfile --adapter caddyfile &
CADDY_PID=$!
echo $CADDY_PID > "$CADDY_PID_FILE"

# 等待 Caddy 启动
sleep 3

# 检查 Caddy 是否启动成功
if kill -0 "$CADDY_PID" 2>/dev/null; then
    print_success "Caddy 启动成功 (PID: $CADDY_PID)"
else
    print_error "Caddy 启动失败"
    exit 1
fi

# 测试服务
print_info "测试服务..."

# 测试健康检查
if curl -s "http://localhost:$HEALTH_PORT" | grep -q "OK"; then
    print_success "健康检查通过 (端口 $HEALTH_PORT)"
else
    print_warning "健康检查失败，但 WebSocket 服务可能仍然正常"
fi

# 测试直接连接到转录服务
print_info "测试直接连接到转录服务..."
if curl -s "http://localhost:$SERVICE_PORT/health" >/dev/null 2>&1; then
    print_success "转录服务直接连接正常 (端口 $SERVICE_PORT)"
else
    print_warning "转录服务直接连接失败，检查服务状态"
fi

# 显示服务信息
echo ""
print_success "🎉 本地测试环境启动完成！"
echo ""
echo "服务访问信息:"
echo "  🔗 WebSocket: ws://localhost:$PROXY_PORT/ws/transcription"
echo "  🏥 健康检查: http://localhost:$HEALTH_PORT"
echo "  📊 直接服务: http://localhost:$SERVICE_PORT"
echo ""
echo "服务进程:"
echo "  📝 gonotic-transcribe: PID $SERVICE_PID"
echo "  🌐 Caddy: PID $CADDY_PID"
echo ""
echo "日志文件:"
echo "  📄 服务日志: ./logs/"
echo "  📄 Caddy 日志: ./logs/caddy.log"
echo ""
echo "测试命令:"
echo "  🧪 WebSocket 测试: wscat -c ws://localhost:$PROXY_PORT/ws/transcription"
echo "  🔍 健康检查: curl http://localhost:$HEALTH_PORT"
echo "  📋 查看进程: ps aux | grep -E 'transcribe_ws|caddy'"
echo ""
echo "停止服务: Ctrl+C 或运行 cleanup"
echo ""

# 保持脚本运行，等待用户中断
print_info "测试环境运行中... 按 Ctrl+C 停止"
while true; do
    # 检查服务是否还在运行
    if ! kill -0 "$SERVICE_PID" 2>/dev/null; then
        print_error "gonotic-transcribe 服务意外停止"
        break
    fi
    if ! kill -0 "$CADDY_PID" 2>/dev/null; then
        print_error "Caddy 服务意外停止"
        break
    fi
    sleep 5
done
