# gonotic-transcribe

独立的转录 WebSocket 服务，从 gonotic 主项目中拆分出来。

## 功能特性

- 仅提供 WebSocket 转录服务
- 支持实时语音转录
- JWT 认证
- 与原 gonotic 项目协议兼容
- 无 HTTP API 接口（纯 WebSocket 服务）

## 技术栈

- Go 1.24.4+
- Gin (HTTP 框架，仅用于 WebSocket 升级)
- gorilla/websocket (WebSocket 实现)
- zap (结构化日志)
- Viper (配置管理)

## 快速开始

### 1. 环境准备

```bash
# 复制环境变量配置
cp .env.example .env

# 编辑配置文件，设置必要的环境变量
vim .env
```

### 2. 安装依赖

```bash
go mod download
```

### 3. 启动服务

```bash
go run cmd/transcribe_ws/main.go
```

服务将在 `127.0.0.1:8090` 启动。

## 配置说明

### 必要配置

- `JWT_SECRET`: JWT 签名密钥（必须设置）
- `REMOTE_WEBSOCKET_URL`: 远程转录服务地址
- `ENTERPRISE_API_KEYS`: 企业级 API 密钥列表

### 可选配置

- `PORT`: 服务端口（默认 8090）
- `ENVIRONMENT`: 运行环境（development/production）
- `LOG_LEVEL`: 日志级别（debug/info/warn/error）

## WebSocket 接口

### 连接地址

```
ws://127.0.0.1:8090/ws/transcription
```

### 认证

需要在连接时提供有效的 JWT token：

1. **Authorization Header**（推荐）:
   ```
   Authorization: Bearer <your-jwt-token>
   ```

2. **Query Parameter**:
   ```
   ws://127.0.0.1:8090/ws/transcription?token=<your-jwt-token>
   ```

### 消息协议

与原 gonotic 项目完全兼容，支持以下消息类型：

#### 客户端 -> 服务器

- `start`: 开始转录
- `pause`: 暂停转录
- `resume`: 恢复转录
- `stop`: 停止转录
- `keepalive`: 保持连接

#### 服务器 -> 客户端

- `started`: 转录已开始
- `transcription`: 转录结果
- `stopped`: 转录已停止
- `time_warning`: 时间警告
- `time_exhausted`: 时间耗尽
- `error`: 错误消息

## 部署

### 使用 Caddy 反向代理

在生产环境中，建议使用 Caddy 作为反向代理：

```caddyfile
gonotic.com:3000 {
    @ws path /ws/transcription
    reverse_proxy @ws 127.0.0.1:8090

    respond "not found" 404
}
```

这样可以将转录服务统一暴露在 `gonotic.com:3000/ws/transcription`。

### 🧪 开发和测试

### 快速开始

```bash
# 克隆项目
git clone https://github.com/chinaxxren/gonotic-transcribe.git
cd gonotic-transcribe

# 安装依赖
./scripts.sh deps

# 运行测试
./scripts.sh test

# 构建项目
./scripts.sh build

# 运行服务
./scripts.sh run
```

### 开发模式

```bash
# 自动重启的开发模式
./scripts.sh dev
```

### 测试

#### 单元测试

```bash
# 运行所有单元测试
go test ./internal/service/... -v

# 运行特定测试
go test ./internal/service/... -run TestWebSocketSession -v
```

#### 集成测试

```bash
# 运行集成测试（需要完整环境）
go test ./test/integration/... -v -tags=integration
```

#### 使用脚本

```bash
# 安装依赖
./scripts.sh deps

# 运行所有测试
./scripts.sh test

# 只运行单元测试
./scripts.sh test-unit

# 只运行集成测试
./scripts.sh test-integration

# 构建项目
./scripts.sh build

# 代码检查
./scripts.sh check
```

## 🏗️ 项目结构

```
gonotic-transcribe/
├── cmd/
│   └── transcribe_ws/
│       └── main.go          # 服务入口
├── internal/
│   ├── api/
│   │   ├── handlers/
│   │   │   └── websocket.go  # WebSocket HTTP 处理器
│   │   └── middleware/
│   │       └── auth.go       # 认证中间件
│   ├── config/
│   │   └── config.go         # 配置管理
│   ├── pkg/
│   │   ├── errors/          # 错误处理
│   │   ├── jwt/             # JWT 管理
│   │   └── logger/          # 日志管理
│   └── service/
│       ├── websocket_*.go   # WebSocket 服务实现
│       └── transcription_types.go
├── .env.example             # 环境变量示例
├── go.mod                   # Go 模块定义
└── README.md               # 项目文档
```

### 代码规范

- 遵循 Go 官方代码规范
- 使用结构化日志
- 所有公共函数必须有注释
- 错误处理要完整

## 注意事项

1. **纯 WebSocket 服务**: 本服务不提供任何 HTTP API 接口
2. **认证依赖**: 需要有效的 JWT token 才能连接
3. **无状态**: 服务本身不存储状态，依赖外部存储
4. **协议兼容**: 与原 gonotic 项目 WebSocket 协议完全兼容

## 故障排除

### 连接失败

1. 检查 JWT token 是否有效
2. 确认服务已正常启动
3. 检查网络连接

### 认证失败

1. 检查 `JWT_SECRET` 配置
2. 确认 token 格式正确
3. 检查 token 是否过期

### 转录失败

1. 检查 `REMOTE_WEBSOCKET_URL` 配置
2. 确认远程转录服务可用
3. 检查 `ENTERPRISE_API_KEYS` 配置

## 许可证

与原 gonotic 项目保持一致的许可证。
# gonotic-transcribe
