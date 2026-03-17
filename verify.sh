#!/bin/bash

# 项目验证脚本
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

print_header() {
    echo -e "${YELLOW}🔍 $1${NC}"
}

echo -e "${YELLOW}🔍 gonotic-transcribe 项目验证${NC}"
echo ""

# 检查项目结构
print_header "检查项目结构..."
required_files=(
    "go.mod"
    "go.sum"
    ".env.example"
    "README.md"
    "MIGRATION_SUMMARY.md"
    "scripts.sh"
    "cmd/transcribe_ws/main.go"
)

missing_files=()
for file in "${required_files[@]}"; do
    if [ ! -f "$file" ]; then
        missing_files+=("$file")
    fi
done

if [ ${#missing_files[@]} -eq 0 ]; then
    print_success "所有必需文件都存在"
else
    print_error "缺失文件: ${missing_files[*]}"
    exit 1
fi

# 检查 Go 文件数量
go_files=$(find . -name "*.go" | wc -l)
print_header "检查 Go 源文件..."
echo "发现 $go_files 个 Go 文件"

if [ $go_files -lt 20 ]; then
    print_error "Go 文件数量不足，期望至少 20 个"
    exit 1
else
    print_success "Go 文件数量充足 ($go_files 个)"
fi

# 检查测试文件
print_header "检查测试文件..."
unit_tests=$(find ./internal/service -name "*_test.go" | wc -l)
integration_tests=$(find ./test -name "*_test.go" | wc -l)

echo "单元测试文件: $unit_tests 个"
echo "集成测试文件: $integration_tests 个"

if [ $unit_tests -lt 1 ] || [ $integration_tests -lt 1 ]; then
    print_error "测试文件不足"
    exit 1
else
    print_success "测试文件充足 (单元: $unit_tests, 集成: $integration_tests)"
fi

# 检查构建产物
print_header "检查构建产物..."
if [ -f "bin/transcribe_ws" ]; then
    print_success "可执行文件已存在"
else
    print_error "可执行文件不存在，请先运行构建"
    exit 1
fi

# 检查依赖
print_header "检查依赖..."
if command -v go >/dev/null 2>&1; then
    echo "Go 版本: $(go version)"
else
    print_error "Go 未安装"
    exit 1
fi

if go mod tidy >/dev/null 2>&1; then
    print_success "依赖管理正常"
else
    print_error "依赖管理失败"
    exit 1
fi

# 运行快速测试
print_header "运行快速验证测试..."
if go test ./internal/service/... -v -run TestWebSocketSessionBasicCreation >/dev/null 2>&1; then
    print_success "基本功能测试通过"
else
    print_error "基本功能测试失败"
    exit 1
fi

echo ""
print_success "🎉 项目验证完成！所有检查都通过了"
echo ""
echo -e "${BLUE}项目已准备就绪，可以进行下一步操作：${NC}"
echo "1. 部署到服务器"
echo "2. 配置 Caddy 反向代理"
echo "3. 运行集成测试"
echo "4. 性能测试"
