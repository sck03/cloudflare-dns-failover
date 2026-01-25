#!/bin/bash

# CFGuard Installation Script

# Define colors for output
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m' # No Color

echo -e "${GREEN}=========================================${NC}"
echo -e "${GREEN}   CFGuard Installer (Python Version)    ${NC}"
echo -e "${GREEN}=========================================${NC}"

# 1. Check Python Version
echo -e "\n${YELLOW}[1/4] Checking Python environment...${NC}"
if ! command -v python3 &> /dev/null; then
    echo -e "${RED}Error: Python 3 is not installed. Please install Python 3.8+ first.${NC}"
    exit 1
fi

python_version=$(python3 -c 'import sys; print(".".join(map(str, sys.version_info[:2])))
')
echo -e "Detected Python version: $python_version"

# 2. Create Virtual Environment
echo -e "\n${YELLOW}[2/4] Setting up virtual environment...${NC}"
if [ -d "venv" ]; then
    echo "Virtual environment 'venv' already exists. Skipping creation."
else
    python3 -m venv venv
    if [ $? -eq 0 ]; then
        echo -e "${GREEN}Virtual environment created successfully.${NC}"
    else
        echo -e "${RED}Failed to create virtual environment.${NC}"
        exit 1
    fi
fi

# Activate virtual environment for installation
source venv/bin/activate

# 3. Install Dependencies
echo -e "\n${YELLOW}[3/4] Installing dependencies...${NC}"
pip install --upgrade pip

if [ -f "requirements.txt" ]; then
    pip install -r requirements.txt
    if [ $? -ne 0 ]; then
        echo -e "${RED}Failed to install dependencies.${NC}"
        exit 1
    fi
else
    echo -e "${RED}Error: requirements.txt not found!${NC}"
    exit 1
fi

# Install recommended production server
echo "Installing waitress (production server)..."
pip install waitress

# 4. Final Setup & Instructions
echo -e "\n${YELLOW}[4/4] Installation Complete!${NC}"

echo -e "\n${GREEN}=========================================${NC}"
echo -e "${GREEN}             Setup Finished              ${NC}"
echo -e "${GREEN}=========================================${NC}"

echo -e "\nTo start the service, run:"
echo -e "  ${YELLOW}./venv/bin/waitress-serve --port=8081 --call \"app:create_app\"${NC}"
echo -e "  (Or simply: ${YELLOW}./start.sh${NC} if you created one)"

echo -e "\nOr for development:"
echo -e "  ${YELLOW}source venv/bin/activate${NC}"
echo -e "  ${YELLOW}python run.py${NC}"

echo -e "\n${YELLOW}⚠️  Important Note for Linux Users:${NC}"
echo "If running as a non-root user, you may need to grant permissions for ICMP Ping:"
echo -e "  ${YELLOW}sudo setcap cap_net_raw+ep $(readlink -f venv/bin/python3)${NC}"

# Create a handy start script
echo -e "\nGenerating start.sh helper script..."
cat > start.sh << 'EOF'
#!/bin/bash
cd "$(dirname "$0")"

# Activate venv
if [ -f "venv/bin/activate" ]; then
    source venv/bin/activate
else
    echo "Virtual environment not found. Please run install.sh first."
    exit 1
fi

# Run with Waitress (Production)
echo "Starting CFGuard with Waitress on port 8081..."
waitress-serve --port=8081 --call "app:create_app"
EOF

chmod +x start.sh
echo -e "${GREEN}Created start.sh${NC}"
