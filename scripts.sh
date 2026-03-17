#!/bin/bash

# 开发和测试脚本
set -e

# 颜色定义
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# 函数：打印带颜色的消息
print_msg() {
    echo -e "${2}${1}${NC}"
}

print_success() {
    print_msg "${GREEN}✅ $1"
}

print_error() {
    print_msg "${RED}❌ $1"
}

print_info() {
    print_msg "${BLUE}ℹ️  $1"
}

print_warning() {
    print_msg "${YELLOW}⚠️  $1"
}

# 显示帮助
show_help() {
    echo "🔧 gonotic-transcribe 开发脚本"
    echo ""
    echo "用法: $0 [命令]"
    echo ""
    echo "命令:"
    echo "  deps     - 安装依赖"
    echo "  test     - 运行测试"
    echo "  build    - 构建项目"
    echo "  run      - 运行服务"
    echo "  dev      - 开发模式（自动重启）"
    echo "  clean    - 清理构建文件"
    echo "  fmt      - 格式化代码"
    echo "  vet      - 代码检查"
    echo "  check    - 运行所有检查"
    echo "  help     - 显示帮助"
}

# 安装依赖
deps() {
    print_info "安装依赖..."
    go mod download
    go mod tidy
    print_success "依赖安装完成"
}

# 运行测试
test() {
    print_info "运行单元测试..."
    go test ./internal/service/... -v
    
    if [ -d "test/integration" ]; then
        print_info "运行集成测试..."
        go test ./test/integration/... -v -tags=integration
    fi
    
    print_success "测试完成"
}

# 构建项目
build() {
    print_info "构建项目..."
    mkdir -p bin
    go build -o bin/transcribe_ws ./cmd/transcribe_ws
    print_success "构建完成"
}

# 运行服务
run() {
    build
    print_info "启动服务..."
    ./bin/transcribe_ws
}

# 开发模式
dev() {
    print_info "开发模式（自动重启）..."
    while true; do
        print_info "启动开发服务..."
        go run ./cmd/transcribe_ws || print_warning "服务重启..."
        sleep 2
    done
}

# 清理
clean() {
    print_info "清理构建文件..."
    rm -rf bin/
    go clean -cache
    print_success "清理完成"
}

# 格式化代码
fmt() {
    print_info "格式化代码..."
    go fmt ./...
    print_success "格式化完成"
}

# 代码检查
vet() {
    print_info "代码检查..."
    go vet ./...
    print_success "检查完成"
}

# 运行所有检查
check() {
    fmt
    vet
    print_success "所有检查完成"
}

# 主逻辑
case "${1:-help}" in
    deps)
        deps
        ;;
    test)
        test
        ;;
    build)
        build
        ;;
    run)
        run
        ;;
    dev)
        dev
        ;;
    clean)
        clean
        ;;
    fmt)
        fmt
        ;;
    vet)
        vet
        ;;
    check)
        check
        ;;
    help|--help|-h)
        show_help
        ;;
    *)
        print_error "未知命令: $1"
        show_help
        exit 1
        ;;
esac
