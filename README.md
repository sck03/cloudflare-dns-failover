# CFGuard (Go 版)

**CFGuard** 是一个企业级、高性能的 Cloudflare DNS 故障转移与监控解决方案，基于 **Go 1.23+** 重构。
核心代码经过深度优化，内置 **HTTP 连接池**、**SQLite WAL 模式**、**智能防堆积调度 (Skip-if-busy)**、**优雅停机 (Graceful Shutdown)** 与 **事务级数据一致性保护**，确保在高并发监控下的极致性能与稳定性。


## ✨ 核心优势

| 功能特性 | 旧版 Python | **CFGuard (Go)** |
| :--- | :--- | :--- |
| **内存占用** | ~80MB+ | **< 15MB** (极轻量) |
| **CPU 占用** | 高 (解释器开销) | **~0%** (编译型二进制) |
| **部署方式** | 复杂环境依赖 | **单文件 / 极小 Docker 镜像** |
| **多账号支持** | ❌ 单账号 | **✅ 支持多个 Cloudflare 账号** |
| **IPv6 支持** | ❌ 仅 IPv4 | **✅ A (IPv4), AAAA (IPv6), CNAME** |
| **安全管理** | ❌ 无 | **✅ JWT 登录认证 (Web 界面)** |
| **消息通知** | ❌ 基础 | **✅ 钉钉, Telegram, 邮件 (SSL/TLS)** |
| **计划任务** | ✅ 简单 | **✅ Cron 表达式精准调度 (防重叠)** |
| **防抖动机制** | ❌ 无 | **✅ 成功阈值 (恢复重试次数)** |
| **架构支持** | ❌ 仅 x86 | **✅ amd64, arm64, arm/v7 (树莓派)** |

## 🌟 主要功能

1.  **自动故障转移 (Failover)**
    *   通过 **ICMP Ping** (L3) 或 **HTTP/HTTPS** (L7) 监控您的服务器。
    *   **智能 Ping**: 自动处理 URL 前缀，支持域名与 IP 直连检测。
    *   一旦检测到故障（如 500/502 错误或 Ping 不通），自动将 Cloudflare DNS 解析切换到备用 IP/域名。
    *   **零停机**: 极速响应，确保服务高可用。

2.  **智能恢复 (Failback)**
    *   当主服务器恢复正常后，自动切回主 IP。
    *   **防抖动保护**: 可配置 `recovery_retries`，要求连续 N 次检测成功才恢复，避免网络波动导致频繁切换。
    *   **精准检测**: 即使 DNS 已切换到备用 IP，系统仍会强制解析并监控**主 IP**，确保只有主服务真正恢复时才切回，避免 DNS 缓存导致的误判。

3.  **计划任务轮换 (Scheduled Rotation)**
    *   使用 Cron 表达式在特定时间自动切换 IP（例如：夜间切换到低成本服务器）。
    *   **优雅停机**: 服务重启或关闭时，自动等待所有正在运行的检测任务完成，防止数据不一致。
    *   示例: `0 8 * * *` (每天早上 8:00 切换)。

4.  **全功能管理**
    *   **多账号**: 在一个地方管理无限个 Cloudflare 账号和域名。
    *   **自动发现**: 输入域名即可自动获取 `Record ID`。
    *   **Web 控制台**: 现代化的响应式 UI，实时查看状态和修改配置。

## � 最佳实践 (Best Practices)

1.  **配置 `original_ip` (强烈推荐)**
    *   虽然 `original_ip` 是可选的，但**强烈建议**您填写主服务器的真实 IP。
    *   如果不填写，系统在 DNS 切换到备用 IP 后，可能会继续监控备用 IP（因为它变成了域名解析的目标），导致误判主服务器已恢复（Flapping）。
    *   填写 `original_ip` 后，系统会强制直接连接该 IP 进行检测，确保监控结果的准确性。

2.  **合理设置 `interval` 与 `timeout`**
    *   建议 `interval` (检测间隔) 大于 `timeout` (超时时间) + 重试耗时。
    *   例如：`interval: 60`, `timeout: 5`, `retries: 3` 是一个稳健的配置。

3.  **使用 HTTPS 监控**
    *   对于 Web 服务，优先使用 `type: https`，它不仅能检测网络连通性，还能验证 Web 服务器（Nginx/Apache）是否正常响应。

## 📦 项目结构

*   `api.go`: RESTful API 路由与控制器 (Gin)
*   `monitor.go`: 核心监控逻辑、调度器与 HTTP 连接池
*   `cloudflare.go`: Cloudflare API 交互封装
*   `notification.go`: 异步消息通知服务 (DingTalk, Telegram, Email)
*   `database.go`: SQLite 数据库初始化与 WAL 模式配置
*   `models.go`: 数据模型定义与默认值处理
*   `main.go`: 程序入口与优雅停机处理

## 🚀 快速开始

### 方式 1: Docker (推荐)

支持 **PC (amd64)**, **Mac M1/M2 (arm64)**, 和 **树莓派 (arm/v7)**。

```bash
docker run -d \
  --name cfguard \
  --restart always \
  -p 8099:8099 \
  -v $(pwd)/config.yaml:/app/config.yaml \
  -v $(pwd)/instance:/app/instance \
  ghcr.io/sck03/cloudflare-dns-failover:latest
```

*(注意: 如果使用 Ping 监控，建议添加 `--cap-add=NET_ADMIN` 权限)*

### 方式 2: Docker Compose

1.  克隆仓库:
    ```bash
    git clone https://github.com/sck03/cloudflare-dns-failover.git
    cd cloudflare-dns-failover
    ```
2.  创建配置 (复制示例文件):
    *   复制 `config.example.yaml` 为 `config.yaml` 并修改配置。
3.  运行:
    ```bash
    docker-compose up -d --build
    ```

### 方式 3: 手动编译运行

需要 Go 1.23+:

```bash
go mod tidy
CGO_ENABLED=0 go build -ldflags="-s -w" -o cfguard .
./cfguard
```

> **注意**: 由于静态资源已嵌入到二进制文件中，您无需再复制 `static` 目录。

## ⚙️ 配置说明

编辑 `config.yaml` 设置您的监控项。

```yaml
server:
  port: 8099
  auth_enabled: true
  jwt_secret: "CHANGE_ME_IN_PRODUCTION" # Web UI 登录密码

monitors:
  - name: "生产环境 Web"
    account: "default"
    domain: "www.example.com"
    type: "https"           # Web 服务推荐使用 https
    target: "https://www.example.com"
    original_ip: "1.1.1.1"
    backup_ip: "2.2.2.2"
    retries: 3              # 连续失败 3 次切换
    recovery_retries: 2     # 连续成功 2 次恢复
    interval: 60            # 每 60 秒检测一次
```

## 🔒 安全性

*   **JWT 认证**: Web 管理界面受保护。使用 `config.yaml` 中的 `jwt_secret` 作为登录密码。
*   **内网模式**: 如果在受信任的内网运行，可设置 `auth_enabled: false` 关闭登录验证。

## 🔄 自动化构建

本项目使用 GitHub Actions 自动构建并发布多架构 Docker 镜像。
只需创建一个新的 Release (例如 `v1.0.0`)，工作流就会自动推送到 GHCR。

## 🤝 贡献

欢迎提交 Issue 和 Pull Request！

---
*Built with ❤️ using Go.*
