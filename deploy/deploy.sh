#!/bin/bash

# gonotic-transcribe 部署脚本
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
SERVICE_NAME="gonotic-transcribe"
SERVICE_PORT=8090
PROXY_PORT=3000
HEALTH_PORT=3001
DEPLOY_DIR="/opt/${SERVICE_NAME}"
BACKUP_DIR="/opt/${SERVICE_NAME}_backup"

print_info "开始部署 ${SERVICE_NAME}..."

# 检查权限
if [ "$EUID" -ne 0 ]; then
    print_error "请使用 root 权限运行此脚本"
    exit 1
fi

# 创建备份
if [ -d "$DEPLOY_DIR" ]; then
    print_info "创建备份..."
    rm -rf "$BACKUP_DIR"
    cp -r "$DEPLOY_DIR" "$BACKUP_DIR"
    print_success "备份完成: $BACKUP_DIR"
fi

# 创建部署目录
print_info "创建部署目录..."
mkdir -p "$DEPLOY_DIR"
mkdir -p /var/log/caddy
mkdir -p /var/log/${SERVICE_NAME}

# 停止旧服务
print_info "停止旧服务..."
if systemctl is-active --quiet ${SERVICE_NAME} 2>/dev/null; then
    systemctl stop ${SERVICE_NAME}
    print_success "旧服务已停止"
fi

# 停止 Caddy (如果需要)
if systemctl is-active --quiet caddy 2>/dev/null; then
    systemctl stop caddy
    print_success "Caddy 已停止"
fi

# 复制文件 (假设脚本在项目根目录运行)
print_info "复制项目文件..."
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"

cp -r "$PROJECT_DIR"/* "$DEPLOY_DIR/"
print_success "项目文件复制完成"

# 构建项目
print_info "构建项目..."
cd "$DEPLOY_DIR"
chmod +x scripts.sh
./scripts.sh build
print_success "项目构建完成"

# 创建 systemd 服务文件
print_info "创建 systemd 服务..."
cat > /etc/systemd/system/${SERVICE_NAME}.service << EOF
[Unit]
Description=gonotic-transcribe WebSocket Service
After=network.target

[Service]
Type=simple
User=gonotic
Group=gonotic
WorkingDirectory=${DEPLOY_DIR}
ExecStart=${DEPLOY_DIR}/bin/transcribe_ws
Restart=always
RestartSec=5
Environment=GIN_MODE=release
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=multi-user.target
EOF

# 创建用户 (如果不存在)
if ! id "gonotic" &>/dev/null; then
    useradd -r -s /bin/false gonotic
    print_success "用户 gonotic 已创建"
fi

# 设置权限
print_info "设置文件权限..."
chown -R gonotic:gonotic "$DEPLOY_DIR"
chown -R gonotic:gonotic /var/log/${SERVICE_NAME}
chmod +x "${DEPLOY_DIR}/bin/transcribe_ws"
print_success "权限设置完成"

# 复制 Caddy 配置
print_info "配置 Caddy..."
cp "$DEPLOY_DIR/deploy/Caddyfile" /etc/caddy/Caddyfile
print_success "Caddy 配置已更新"

# 启动服务
print_info "启动服务..."
systemctl daemon-reload
systemctl enable ${SERVICE_NAME}
systemctl start ${SERVICE_NAME}

# 等待服务启动
print_info "等待服务启动..."
sleep 5

# 检查服务状态
if systemctl is-active --quiet ${SERVICE_NAME}; then
    print_success "${SERVICE_NAME} 服务启动成功"
else
    print_error "${SERVICE_NAME} 服务启动失败"
    journalctl -u ${SERVICE_NAME} --no-pager -l
    exit 1
fi

# 启动 Caddy
print_info "启动 Caddy..."
systemctl enable caddy
systemctl start caddy

# 等待 Caddy 启动
sleep 3

if systemctl is-active --quiet caddy; then
    print_success "Caddy 启动成功"
else
    print_error "Caddy 启动失败"
    journalctl -u caddy --no-pager -l
    exit 1
fi

# 测试服务
print_info "测试服务..."
if curl -s "http://localhost:${HEALTH_PORT}" | grep -q "OK"; then
    print_success "健康检查通过"
else
    print_warning "健康检查失败，但 WebSocket 服务可能仍然正常"
fi

# 测试 WebSocket 连接 (可选)
print_info "测试 WebSocket 连接..."
if command -v wscat >/dev/null 2>&1; then
    echo "尝试连接 WebSocket..."
    timeout 5 wscat -c "ws://localhost:${PROXY_PORT}/ws/transcription" || true
    print_info "WebSocket 连接测试完成"
else
    print_warning "wscat 未安装，跳过 WebSocket 测试"
fi

# 显示服务状态
print_info "服务状态:"
echo ""
echo "gonotic-transcribe:"
systemctl status ${SERVICE_NAME} --no-pager -l
echo ""
echo "Caddy:"
systemctl status caddy --no-pager -l

# 显示访问信息
echo ""
print_success "🎉 部署完成！"
echo ""
echo "服务访问信息:"
echo "  WebSocket: ws://gonotic.com:${PROXY_PORT}/ws/transcription"
echo "  健康检查: http://gonotic.com:${HEALTH_PORT}"
echo ""
echo "管理命令:"
echo "  查看日志: journalctl -u ${SERVICE_NAME} -f"
echo "  重启服务: systemctl restart ${SERVICE_NAME}"
echo "  查看状态: systemctl status ${SERVICE_NAME}"
echo ""
echo "备份位置: $BACKUP_DIR"
echo "部署位置: $DEPLOY_DIR"
