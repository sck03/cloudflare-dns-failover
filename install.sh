#!/bin/bash

# CFGuard 安装脚本 (Python 版)

# 定义输出颜色
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m' # No Color

echo -e "${GREEN}=========================================${NC}"
echo -e "${GREEN}   CFGuard 安装程序 (Python 版)          ${NC}"
echo -e "${GREEN}=========================================${NC}"

# 1. 检查 Python 版本
echo -e "\n${YELLOW}[1/4] 正在检查 Python 环境...${NC}"
if ! command -v python3 &> /dev/null; then
    echo -e "${RED}错误: 未检测到 Python 3。请先安装 Python 3.8+。${NC}"
    exit 1
fi

python_version=$(python3 -c 'import sys; print(".".join(map(str, sys.version_info[:2])))
')
echo -e "检测到 Python 版本: $python_version"

# 2. 创建虚拟环境
echo -e "\n${YELLOW}[2/4] 正在设置虚拟环境...${NC}"
if [ -d "venv" ]; then
    echo "虚拟环境 'venv' 已存在。跳过创建。"
else
    python3 -m venv venv
    if [ $? -eq 0 ]; then
        echo -e "${GREEN}虚拟环境创建成功。${NC}"
    else
        echo -e "${RED}创建虚拟环境失败。${NC}"
        exit 1
    fi
fi

# 激活虚拟环境进行安装
source venv/bin/activate

# 3. 安装依赖
echo -e "\n${YELLOW}[3/4] 正在安装依赖...${NC}"
pip install --upgrade pip

if [ -f "requirements.txt" ]; then
    pip install -r requirements.txt
    if [ $? -ne 0 ]; then
        echo -e "${RED}依赖安装失败。${NC}"
        exit 1
    fi
else
    echo -e "${RED}错误: 未找到 requirements.txt！${NC}"
    exit 1
fi

# 安装推荐的生产服务器
echo "正在安装 waitress (生产环境服务器)..."
pip install waitress

# 4. 最终设置与说明
echo -e "\n${YELLOW}[4/4] 安装完成！${NC}"

echo -e "\n${GREEN}=========================================${NC}"
echo -e "${GREEN}             设置已完成                  ${NC}"
echo -e "${GREEN}=========================================${NC}"

echo -e "\n启动服务，请运行:"
echo -e "  ${YELLOW}./venv/bin/waitress-serve --port=8081 --call \"app:create_app\"${NC}"
echo -e "  (或者直接运行: ${YELLOW}./start.sh${NC} 如果已创建)"

echo -e "\n开发环境下运行:"
echo -e "  ${YELLOW}source venv/bin/activate${NC}"
echo -e "  ${YELLOW}python run.py${NC}"

echo -e "\n${YELLOW}⚠️  Linux 用户重要提示:${NC}"
echo "如果以非 root 用户运行，您可能需要授予 ICMP Ping 权限:"
echo -e "  ${YELLOW}sudo setcap cap_net_raw+ep $(readlink -f venv/bin/python3)${NC}"

# 创建便捷启动脚本
echo -e "\n正在生成 start.sh 辅助脚本..."
cat > start.sh << 'EOF'
#!/bin/bash
cd "$(dirname "$0")"

# 激活虚拟环境
if [ -f "venv/bin/activate" ]; then
    source venv/bin/activate
else
    echo "未找到虚拟环境。请先运行 install.sh。"
    exit 1
fi

# 使用 Waitress 运行 (生产模式)
echo "正在启动 CFGuard (Waitress 端口 8081)..."
waitress-serve --port=8081 --call "app:create_app"
EOF

chmod +x start.sh
echo -e "${GREEN}已创建 start.sh${NC}"