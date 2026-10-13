# Cloudflare DNS Failover / CFGuard

[![Build and publish](https://github.com/sck03/cloudflare-dns-failover/actions/workflows/docker-publish.yml/badge.svg)](https://github.com/sck03/cloudflare-dns-failover/actions/workflows/docker-publish.yml)

用 Go 监控主、备源站，并在故障时更新 Cloudflare DNS。提供 Web 管理、计划切换、多账号、IPv4 / IPv6 / CNAME，以及钉钉、Telegram、邮件通知。

当前工具链为 **Go 1.27.2**；GitHub Actions 版本于 **2026-10-13** 对照官方稳定发行核实，固定到对应提交。项目使用 Go 标准库 HTTP 路由、SQLite 和独立的检测 / 决策 / DNS 模块。

**网站监控应选 HTTP/HTTPS。** Ping 只能证明网络可达，不能发现网站返回 500、502、503 等应用故障。请填写真实源站地址，使用能反映业务状态的健康检查 URL。

## 切换行为

1. 同时检测主、备源站。HTTP 保留 URL 中的 Host 和 TLS SNI，连接指定源站，不跟随已经切换的业务 DNS。
2. 当前源站连续失败达到 `retries`，且另一端健康时，才发起切换。主备都失败时保留 DNS，并显示故障。
3. Cloudflare 必须同时返回成功状态和符合请求的记录内容、代理状态，程序才提交本地状态和成功事件。失败会保留原状态，在后续检测中重试。
4. 优先源站连续恢复达到 `success_threshold` 后切回。若当前源站也已达到故障阈值，可提前切回本次检测健康的优先源站。
5. 手动和定时切换同样先检查目标；成功后将目标保存为优先地址，继续自动故障保护。默认优先地址为主地址。

启动后和每五分钟核对一次 Cloudflare 实际记录；修改策略或切换失败会触发重新核对。配置 `dns_ttl` 后，会在当前源站健康时提前更新 TTL。未知记录、重复记录无法唯一定位、账号缺失等情况会显示具体错误。

**DNS 切换有传播时间。** 检测间隔、连续失败阈值、API 耗时、旧 TTL、客户端缓存及已有连接都会影响恢复速度。本项目不承诺零中断；降低 TTL 也不能撤回客户端已缓存的旧记录。详见[故障排查](docs/OPERATIONS.md)。

## 使用 GitHub 构建的镜像

首次安装：

```bash
git clone https://github.com/sck03/cloudflare-dns-failover.git
cd cloudflare-dns-failover
mkdir -p config instance
cp config.example.yaml config/config.yaml
```

编辑 `config/config.yaml`：设置至少 32 字符的独立 `server.jwt_secret`（同时也是 Web 登录密码），填写 Cloudflare API Token。可用 `openssl rand -hex 32` 生成密码。Token 需要目标 Zone 的 **Zone Read** 和 **DNS Edit** 权限。

```bash
docker compose pull
docker compose up -d
```

访问 `http://服务器地址:8099`。通过反向代理提供 HTTPS 后再开放公网管理入口。配置目录和 `instance` 目录必须可写。默认持久化 `instance/cfguard.db`，支持 `linux/amd64`、`linux/arm64`、`linux/arm/v7`。

已有实例请先阅读[升级说明](docs/UPGRADE.md)，尤其是目录挂载、HTTPS 证书校验和首次初始化行为的变化。

## 配置

完整示例见 [config.example.yaml](config.example.yaml)。默认只从 YAML 初始化监控一次；随后由 Web UI 和 SQLite 管理，重启不会覆盖界面修改或重新创建已删除的监控。

| 字段 | 默认值 | 说明 |
| --- | --- | --- |
| `server.port` | `8099` | Web 服务端口 |
| `server.max_concurrent_checks` | `8` | 同时执行的监控操作上限，每项最多并行探测两个源站 |
| `database.history_retention_days` | `30` | 切换记录保留天数，启动和每小时清理过期记录 |
| `type` | `https` | `http`、`https`、`ping`；已有策略不会自动改变类型 |
| `target` | 必填（HTTP） | 完整健康检查 URL；省略协议时使用 `type`，显式 URL 的协议优先 |
| `original_ip` / `backup_ip` | 必填 | 不同的真实源站；A 填 IPv4，AAAA 填 IPv6，CNAME 填独立源站域名 |
| `interval` | `60` | 调度间隔，秒；长任务不会堆积为重复检查 |
| `timeout` | `5` | 每个源站一次检测的总时间预算，秒，最大 120 |
| `retries` | `3` | 当前地址连续失败阈值 |
| `success_threshold` | `2` | 优先地址连续成功恢复阈值 |
| `ping_count` | `3` | 一次 Ping 检测最多尝试次数，共享 `timeout` 预算 |
| `expected_status` | `0` | HTTP 接受任意 2xx；也可指定 200、204 等 2xx 状态 |
| `insecure_skip_verify` | `false` | 显式允许自签名 / Cloudflare Origin CA 源站证书 |
| `dns_ttl` | `0` | 0 保留；1 自动；60–86400 为秒数。界面新建默认 60；代理记录固定自动 TTL |
| `original_ip_cdn_enabled` / `backup_ip_cdn_enabled` | `false` | 切换至对应地址时使用的 Cloudflare 代理状态 |
| `cf_record_id` | 可省略 | 同名同类型多条记录时必须明确填写；单条记录可自动发现或修复失效 ID |

HTTP 检测最多跟随五次同主机重定向，并判断最终响应；跨主机重定向会报错，请直接配置健康检查路径。响应读取限制为 32 KiB。源站防火墙应允许监控程序访问。

同一个 DNS 名称 / 类型只允许一个策略管理，避免策略互相覆盖。有 A 和 AAAA 两种记录时，需要分别配置对应策略。

### YAML 首次初始化示例

```yaml
monitors:
  - name: 网站
    account: default
    domain: www.example.com
    zone_id: your_zone_id
    dns_type: A
    type: https
    target: https://www.example.com/health
    original_ip: 192.0.2.10
    backup_ip: 192.0.2.20
    interval: 15
    timeout: 5
    retries: 3
    success_threshold: 2
    expected_status: 200
    dns_ttl: 60
    schedules:
      - cron: "0 8 * * *"
        target_ip: 192.0.2.10
      - cron: "0 20 * * *"
        target_ip: 192.0.2.20
```

示例地址是文档保留地址，部署时必须替换。CNAME 源站域名应直接解析到源站，不能是当前管理的业务域名或另一层 CDN。

Web 界面的简化计划使用 `0 */N * * *`（当地时区整点，每天重新计数）；任意五字段 Cron 可通过 YAML 首次初始化或 API `schedules` 数组配置。目标必须是该策略的主地址或备用地址。

## 编译、测试与发布

GitHub 上的 `Build and publish` 工作流在 `main` 推送、版本标签或手动触发时执行：

- 格式、`go vet`、竞态检测、回归测试、依赖漏洞扫描、前端测试和工作流校验。
- 测试通过后交叉编译 Linux 三种架构和 Windows amd64；可在 Actions 的 `cfguard-binaries` 附件下载程序、示例配置和 SHA256 校验文件。
- 验证容器启动、自定义端口健康检查、Ping 和正常停机，再推送多架构 GHCR 镜像，提供 `latest`、提交 SHA 和版本标签。测试失败时不进入发布步骤。

本地使用 Go 1.27.2：

```bash
go test ./...
go vet ./...
CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o cfguard .
./cfguard -version
./cfguard
```

无配置时会生成 `config/config.yaml` 并退出。检查已启动服务可执行 `./cfguard -healthcheck`，会使用配置中的实际端口。完整 CI 检查见 [scripts/check.sh](scripts/check.sh)，跨平台构建见 [scripts/build.sh](scripts/build.sh)。

## 架构与审阅

- [架构、状态模型与扩展边界](docs/ARCHITECTURE.md)
- [完成进度与验证记录](docs/STATUS.md)
- [部署、故障排查与 API](docs/OPERATIONS.md)
- [已有实例升级说明](docs/UPGRADE.md)

后端和前端静态资源嵌入一个程序。资源占用取决于策略数量、超时、通知和数据库规模；项目不再使用未经测量的内存、CPU 或零停机保证。
