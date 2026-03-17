#!/bin/bash

# 带认证的 WebSocket 测试脚本
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
JWT_SECRET="test-secret-key-for-local-development"
USER_ID=123
SESSION_UUID="test-session-$(date +%s)"

print_info "开始带认证的 WebSocket 测试..."

# 生成测试 JWT Token
print_info "生成测试 JWT Token..."
if command -v python3 >/dev/null 2>&1 && python3 -c "import jwt" 2>/dev/null; then
    # 使用 Python 生成 JWT
    TOKEN=$(python3 -c "
import jwt
import time
payload = {
    'user_id': $USER_ID,
    'session_uuid': '$SESSION_UUID',
    'exp': int(time.time()) + 3600
}
token = jwt.encode(payload, '$JWT_SECRET', algorithm='HS256')
print(token)
" 2>/dev/null || echo "")
elif command -v node >/dev/null 2>&1; then
    # 使用 Node.js 生成 JWT
    TOKEN=$(node -e "
const jwt = require('jsonwebtoken');
const payload = {
  user_id: $USER_ID,
  session_uuid: '$SESSION_UUID',
  exp: Math.floor(Date.now() / 1000) + 3600
};
const token = jwt.sign(payload, '$JWT_SECRET');
console.log(token);
" 2>/dev/null || echo "")
else
    print_warning "无法生成 JWT Token，需要 Python 或 Node.js"
    TOKEN="test-token-placeholder"
fi

if [ -z "$TOKEN" ]; then
    print_error "JWT Token 生成失败"
    exit 1
fi

print_success "JWT Token 生成成功"
echo "Token: $TOKEN"
echo ""

# 测试函数
test_websocket_with_auth() {
    local url=$1
    local description=$2
    
    print_info "测试 $description..."
    
    if command -v wscat >/dev/null 2>&1; then
        echo "连接到: $url"
        echo "使用 JWT Token: ${TOKEN:0:50}..."
        echo ""
        
        # 创建测试脚本
        cat > /tmp/ws_auth_test.js << 'EOF'
const WebSocket = require('ws');

const url = process.argv[2];
const token = process.argv[3];

const ws = new WebSocket(url, {
    headers: {
        'Authorization': 'Bearer ' + token
    }
});

ws.on('open', function open() {
    console.log('✅ WebSocket 连接成功 (带认证)');
    
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
    
    // 10秒后关闭连接
    setTimeout(() => {
        ws.close();
    }, 10000);
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

        # 运行测试
        if [ -f "package.json" ] || npm list ws >/dev/null 2>&1; then
            node /tmp/ws_auth_test.js "$url" "$TOKEN" || print_warning "$description 测试失败"
        else
            print_warning "ws 模块未安装，跳过详细测试"
            echo "安装命令: npm install ws"
        fi
        
        rm -f /tmp/ws_auth_test.js
    else
        print_warning "wscat 未安装，跳过 WebSocket 测试"
        echo "安装命令: npm install -g wscat"
    fi
}

# 测试直接连接
print_info "测试直接连接 (端口 $SERVICE_PORT)..."
test_websocket_with_auth "ws://localhost:$SERVICE_PORT/ws/transcription" "直接连接"

echo ""
print_info "等待 2 秒..."
sleep 2

echo ""
# 测试代理连接 (如果 Caddy 在运行)
if curl -s "http://localhost:$PROXY_PORT" >/dev/null 2>&1; then
    print_info "检测到 Caddy 代理运行，测试代理连接..."
    test_websocket_with_auth "ws://localhost:$PROXY_PORT/ws/transcription" "代理连接"
else
    print_info "Caddy 代理未运行，跳过代理测试"
    echo "启动代理命令: caddy run --config deploy/local/Caddyfile"
fi

echo ""
print_info "手动测试命令:"
echo "1. 直接连接 (需要认证):"
echo "   wscat -c ws://localhost:$SERVICE_PORT/ws/transcription -H 'Authorization: Bearer $TOKEN'"
echo ""
echo "2. 代理连接 (如果 Caddy 运行):"
echo "   wscat -c ws://localhost:$PROXY_PORT/ws/transcription -H 'Authorization: Bearer $TOKEN'"
echo ""
echo "3. 测试消息:"
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

# 显示 Token 信息
echo ""
print_info "认证信息:"
echo "  🔑 JWT Token: $TOKEN"
echo "  👤 User ID: $USER_ID"
echo "  🆔 Session UUID: $SESSION_UUID"
echo "  ⏰ Expiration: 1 小时"

print_success "带认证的 WebSocket 测试完成！"
