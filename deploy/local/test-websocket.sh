#!/bin/bash

# WebSocket 测试脚本
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
PROXY_PORT=3000
SERVICE_PORT=8090
TEST_URL_PROXY="ws://localhost:$PROXY_PORT/ws/transcription"
TEST_URL_DIRECT="ws://localhost:$SERVICE_PORT/ws/transcription"

print_info "WebSocket 连接测试..."

# 检查依赖
if ! command -v wscat >/dev/null 2>&1; then
    print_error "wscat 未安装，请先安装"
    echo "安装命令: npm install -g wscat"
    exit 1
fi

print_success "依赖检查通过"

# 测试函数
test_websocket_connection() {
    local url=$1
    local description=$2
    
    print_info "测试 $description..."
    
    # 使用 wscat 测试连接
    echo "连接到: $url"
    echo "发送测试消息..."
    
    # 创建临时脚本来自动测试
    cat > /tmp/ws_test.js << 'EOF'
const WebSocket = require('ws');

const url = process.argv[2];
const ws = new WebSocket(url);

ws.on('open', function open() {
    console.log('✅ WebSocket 连接成功');
    
    // 发送测试消息
    const testMessage = {
        type: 'start',
        data: {
            meeting_id: 1,
            user_preferences: {
                language: 'zh-CN',
                model: 'whisper-1'
            }
        },
        timestamp: new Date().toISOString()
    };
    
    console.log('📤 发送消息:', JSON.stringify(testMessage, null, 2));
    ws.send(JSON.stringify(testMessage));
    
    // 5秒后关闭连接
    setTimeout(() => {
        ws.close();
    }, 5000);
});

ws.on('message', function message(data) {
    console.log('📥 收到消息:', data.toString());
});

ws.on('close', function close() {
    console.log('🔌 WebSocket 连接关闭');
    process.exit(0);
});

ws.on('error', function error(err) {
    console.error('❌ WebSocket 错误:', err.message);
    process.exit(1);
});
EOF

    # 检查 Node.js 是否可用
    if command -v node >/dev/null 2>&1; then
        if [ -f "package.json" ] || npm list ws >/dev/null 2>&1; then
            node /tmp/ws_test.js "$url" || print_warning "$description 测试失败"
        else
            print_warning "ws 模块未安装，跳过详细测试"
        fi
    else
        print_warning "Node.js 未安装，使用简单测试"
    fi
    
    # 备用测试：使用 curl (有限)
    print_info "尝试 HTTP 升级测试..."
    if curl -s -H "Connection: Upgrade" -H "Upgrade: websocket" \
           -H "Sec-WebSocket-Key: test" -H "Sec-WebSocket-Version: 13" \
           "http://localhost:${url##*:}/ws/transcription" >/dev/null 2>&1; then
        print_success "WebSocket 升级响应正常"
    else
        print_warning "WebSocket 升级测试失败"
    fi
    
    rm -f /tmp/ws_test.js
}

# 测试代理连接
test_websocket_connection "$TEST_URL_PROXY" "代理连接 (通过 Caddy)"

echo ""
print_info "等待 2 秒..."
sleep 2

echo ""
# 测试直接连接
test_websocket_connection "$TEST_URL_DIRECT" "直接连接 (不通过 Caddy)"

echo ""
print_info "手动测试命令:"
echo "1. 代理连接测试:"
echo "   wscat -c $TEST_URL_PROXY"
echo ""
echo "2. 直接连接测试:"
echo "   wscat -c $TEST_URL_DIRECT"
echo ""
echo "3. 测试消息示例:"
echo '   {"type":"start","data":{"meeting_id":1},"timestamp":"2025-03-16T22:00:00Z"}'
echo ""
echo "4. 暂停转录:"
echo '   {"type":"pause","timestamp":"2025-03-16T22:00:00Z"}'
echo ""
echo "5. 恢复转录:"
echo '   {"type":"resume","timestamp":"2025-03-16T22:00:00Z"}'
echo ""
echo "6. 停止转录:"
echo '   {"type":"stop","timestamp":"2025-03-16T22:00:00Z"}'

# 检查服务状态
print_info "检查服务状态..."
if curl -s "http://localhost:3001" | grep -q "OK"; then
    print_success "健康检查服务正常"
else
    print_warning "健康检查服务异常"
fi

print_success "WebSocket 测试完成！"
