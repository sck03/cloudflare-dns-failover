# CFGuard (Python Version)

CFGuard 是一个轻量级、高可用的 Cloudflare DNS 故障转移与监控系统。
它通过持续监控您的服务器 IP（支持 Ping 和 HTTP/HTTPS），在主服务器宕机时自动切换 Cloudflare DNS 解析记录到备用 IP，并在服务恢复后自动切回。

此版本已从 Go 重构为 Python (Flask) 架构，更易于二次开发和部署。

## ✨ 核心功能

*   **多协议监控**: 支持 ICMP Ping 和 HTTP/HTTPS 状态码检测。
*   **自动故障转移 (Failover)**: 主 IP 故障时自动修改 Cloudflare DNS 记录指向备用 IP。
*   **自动恢复 (Failback)**: 主 IP 恢复正常后自动切回。
*   **计划任务切换**: 支持按时间计划轮换 IP（例如白天用 A，晚上用 B）。
*   **多账号管理**: 支持管理多个 Cloudflare 账号和域名。
*   **多渠道告警**: 支持 钉钉 (DingTalk)、Telegram、Email 告警通知。
*   **可视化大屏**: 提供实时状态监控、历史切换记录和故障统计。

## 🛠️ 技术栈

*   **语言**: Python 3.8+
*   **Web 框架**: Flask
*   **数据库**: SQLite (通过 SQLAlchemy ORM)
*   **调度器**: APScheduler (后台并发检测)
*   **前端**: 原生 HTML/JS/CSS (无构建步骤)

## 🚀 快速开始

### 1. 环境要求
*   Python 3.8 或更高版本
*   pip 包管理器
*   Git

### 2. 获取代码

```bash
git clone https://github.com/woniu336/cloudflare-dns-failover.git
cd cloudflare-dns-failover
```

### 3. 安装

**方式 A：自动化安装 (推荐)**
```bash
chmod +x install.sh
./install.sh
```
该脚本会自动创建虚拟环境、安装依赖并生成 `start.sh` 启动脚本。

**方式 B：手动安装**
```bash
# 创建并激活虚拟环境
python3 -m venv venv
source venv/bin/activate  # Windows 使用 venv\Scripts\activate

# 安装依赖
pip install -r requirements.txt
```

### 4. 运行服务

**生产模式 (推荐):**
```bash
./start.sh
```
start.sh文件修改,增加8081端口检测
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
#!/bin/bash
if lsof -i :8081 >/dev/null; then
    echo "端口 8081 占用中，正在释放..."
    sudo fuser -k 8081/tcp
    sleep 2
fi
echo "正在启动 CFGuard (Waitress 端口 8081)..."
# 下面是原 waitress-serve 命令
waitress-serve --port=8081 --call "app:create_app"

推荐后台跑，避免重复
nohup ./start.sh > cfguard.log 2>&1 &
# 或简单：
./start.sh &

**开发模式:**
```bash
python run.py
```

服务默认运行在 `http://0.0.0.0:8081`。

### 5. 首次登录
访问 Web 界面时，您需要配置一个管理令牌（Token）。
*   首次访问 API 或界面时，系统会提示您输入或设置 Token。
*   或者直接在数据库/配置文件中查看自动生成的配置。

## 📂 目录结构

```text
CFGuard/
├── app/
│   ├── api/            # API 接口路由
│   ├── core/           # 监控核心引擎 & 调度逻辑
│   ├── models.py       # 数据库模型
│   └── ...
├── instance/
│   └── cfguard.db      # SQLite 数据库 (自动生成)
├── static/             # 前端静态资源
├── config.py           # 项目配置
├── requirements.txt    # 依赖列表
└── run.py              # 启动入口
```

## ⚙️ 配置说明

所有配置均可通过 Web 界面进行管理：
1.  **Cloudflare 账号**: 添加 API Token 或 API Key/Email。
2.  **监控策略**: 添加需要监控的域名、IP 和阈值。
3.  **全局告警**: 配置钉钉、TG 或 邮件通知。

## ⚠️ 部署注意
*   **Ping 权限**: 在 Linux 非 root 用户下运行可能需要额外权限才能发送 ICMP 包。建议使用 root 运行或配置 `setcap`。
*   **生产环境**: 建议使用 `gunicorn` 或 `waitress` 进行部署，配合 Nginx 反向代理。

```bash
# 使用 waitress (Windows/Linux)
pip install waitress
waitress-serve --port=8081 --call "app:create_app"
```

## 📄 License
MIT
