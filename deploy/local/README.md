# 本地测试环境

## 🎯 概述

本地测试环境用于验证 `gonotic-transcribe` 服务的功能，包括：
- 直接 WebSocket 连接测试
- 通过 Caddy 反向代理的连接测试
- 协议兼容性验证

## 🏗️ 环境架构

```
客户端 → Caddy (3000) → gonotic-transcribe (8090)
        ↓
    健康检查 (3001)
```

- **gonotic-transcribe**: 端口 8090，提供 WebSocket 服务
- **Caddy**: 端口 3000，反向代理 WebSocket 连接
- **健康检查**: 端口 3001，简单的 HTTP 健康检查

## 📋 前置要求

### 必需软件
1. **Go 1.24+** - 运行转录服务
2. **Caddy 2.7+** - 反向代理
3. **curl** - HTTP 测试

### 可选软件
1. **wscat** - WebSocket 客户端测试
   ```bash
   npm install -g wscat
   ```
2. **Node.js + ws** - 高级 WebSocket 测试
   ```bash
   npm install ws
   ```

## 🚀 快速开始

### 1. 启动本地测试环境

```bash
cd gonotic-transcribe
./deploy/local/test-local.sh
```

这个脚本会：
- 构建项目
- 启动 gonotic-transcribe 服务 (端口 8090)
- 启动 Caddy 反向代理 (端口 3000)
- 启动健康检查服务 (端口 3001)
- 显示服务状态和访问信息

### 2. 测试 WebSocket 连接

```bash
# 在新终端中运行
./deploy/local/test-websocket.sh
```

### 3. 手动测试

#### 通过 Caddy 代理
```bash
wscat -c ws://localhost:3000/ws/transcription
```

#### 直接连接
```bash
wscat -c ws://localhost:8090/ws/transcription
```

#### 发送测试消息
```json
// 开始转录
{
  "type": "start",
  "data": {
    "meeting_id": 1,
    "user_preferences": {
      "language": "zh-CN",
      "model": "whisper-1"
    }
  },
  "timestamp": "2025-03-16T22:00:00Z"
}

// 暂停转录
{
  "type": "pause",
  "timestamp": "2025-03-16T22:00:00Z"
}

// 恢复转录
{
  "type": "resume",
  "timestamp": "2025-03-16T22:00:00Z"
}

// 停止转录
{
  "type": "stop",
  "timestamp": "2025-03-16T22:00:00Z"
}

// 保持连接
{
  "type": "keepalive",
  "timestamp": "2025-03-16T22:00:00Z"
}
```

## 🔧 配置说明

### Caddy 配置 (Caddyfile)
- 监听端口 3000，代理 `/ws/transcription` 到 `gonotic-transcribe:8090`
- 监听端口 3001，提供健康检查
- 其他请求返回 404

### 环境变量
```bash
GIN_MODE=development
SERVER_HOST=127.0.0.1
SERVER_PORT=8090
LOG_LEVEL=debug
JWT_SECRET=test-secret-key-for-local-development
JWT_EXPIRATION=24h
```

## 🧪 测试验证

### 基本连接测试
1. ✅ 服务启动成功
2. ✅ Caddy 代理正常
3. ✅ WebSocket 连接建立
4. ✅ 消息发送和接收

### 协议兼容性测试
1. ✅ start 命令处理
2. ✅ pause/resume 命令处理
3. ✅ stop 命令处理
4. ✅ keepalive 命令处理
5. ✅ 错误消息处理

### 代理功能测试
1. ✅ 通过 Caddy 连接正常
2. ✅ 直接连接正常
3. ✅ 路由转发正确
4. ✅ 404 响应正确

## 📊 监控和日志

### 服务状态
```bash
# 查看进程
ps aux | grep -E 'transcribe_ws|caddy'

# 查看端口占用
lsof -i :8090 -i :3000 -i :3001
```

### 日志文件
- **服务日志**: `./logs/`
- **Caddy 日志**: `./logs/caddy.log`

### 健康检查
```bash
# 健康检查
curl http://localhost:3001

# 服务状态
curl http://localhost:8090/health
```

## 🛠️ 故障排除

### 常见问题

1. **端口占用**
   ```bash
   # 查找占用端口的进程
   lsof -i :8090
   lsof -i :3000
   
   # 停止占用进程
   kill -9 <PID>
   ```

2. **Caddy 启动失败**
   ```bash
   # 检查 Caddy 配置
   caddy validate --config deploy/local/Caddyfile
   
   # 查看 Caddy 日志
   tail -f logs/caddy.log
   ```

3. **WebSocket 连接失败**
   ```bash
   # 检查服务状态
   curl http://localhost:8090/health
   
   # 测试直接连接
   wscat -c ws://localhost:8090/ws/transcription
   ```

4. **权限问题**
   ```bash
   # 确保脚本可执行
   chmod +x deploy/local/*.sh
   ```

### 清理环境
```bash
# 停止所有服务
pkill -f transcribe_ws
pkill -f caddy

# 清理临时文件
rm -f /tmp/gonotic-transcribe.pid
rm -f /tmp/caddy.pid
```

## 📝 测试清单

- [ ] 服务启动成功
- [ ] Caddy 代理正常
- [ ] WebSocket 连接建立
- [ ] start 命令响应正确
- [ ] pause 命令响应正确
- [ ] resume 命令响应正确
- [ ] stop 命令响应正确
- [ ] keepalive 命令响应正确
- [ ] 错误处理正常
- [ ] 代理转发正确
- [ ] 404 响应正确
- [ ] 日志记录正常
- [ ] 健康检查正常

## 🔄 下一步

本地测试通过后，可以进行：
1. **性能测试** - 并发连接和负载测试
2. **集成测试** - 与客户端应用的完整测试
3. **部署测试** - 生产环境部署验证
