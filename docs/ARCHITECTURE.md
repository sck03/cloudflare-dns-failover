# 架构与扩展边界

## 当前结构

```mermaid
flowchart LR
    UI[Web 控制台] --> API[标准库 HTTP API]
    API --> Config[配置快照 / 原子写入]
    API --> Store[SQLite / GORM]
    API --> Engine[监控引擎 / 串行操作]
    Cron[调度器] --> Engine
    Engine --> Health[internal/health]
    Engine --> Policy[internal/failover]
    Engine --> DNS[DNSProvider 接口]
    DNS --> CF[internal/cloudflare]
    Engine --> Store
    Engine --> Notify[有界通知队列]
```

| 模块 | 职责 | 约束 |
| --- | --- | --- |
| `main.go`、`http.go`、`auth.go` | 启动 / 停机、路由、JSON、JWT | 不实现 DNS 决策；限制请求体、请求头和网络超时 |
| `api*.go` | 监控、计划、账号、DNS 和状态接口 | 校验参数和引用，配置变更不能覆盖正在执行的切换 |
| `config.go` | YAML、不可变读快照、原子保存 | 文件写入成功后再替换内存配置；未知账号不回退到其他凭证 |
| `models.go`、`database.go` | 校验、迁移、初始化、状态及事件事务 | 兼容既有 SQLite 表；配置字段与运行字段分开写入 |
| `monitor.go` | 调度、限流、主备探测、DNS 核对、协调提交 | 通过 `HealthChecker` / `DNSProvider` 注入外部行为 |
| `internal/failover` | 连续计数和目标选择 | 纯计算；不修改已确认的当前地址，不访问数据库或网络 |
| `internal/health` | HTTP(S) / Ping 探测 | 源站直连、保留 Host/SNI、总超时、限制响应读取 |
| `internal/cloudflare` | 请求签名、响应校验、分页、退避 | 不依赖数据库、Web 框架或全局配置 |
| `notification.go` | 通知发送 | 2 个工作协程、64 条有界队列，网络调用有截止时间 |
| `static/js` | API、状态、组件和事件 | 共用错误处理和转义；后台页暂停轮询，同一轮询不重叠 |

为减少依赖和二进制体积，HTTP 层采用 `net/http.ServeMux`，无需 Gin 及其额外 JSON、表单、HTTP/3 等依赖。保留 GORM 负责已有表的兼容迁移与事务，SQLite 驱动使用纯 Go 实现。

## 状态与一致性

三个事实独立保存：

- `current_ip`：最近从 Cloudflare 读取或确认写入的地址。
- `preferred_ip`：恢复后的优先地址。默认主地址；成功的计划或手动切换会更新它。
- `primary_healthy` / `backup_healthy`：最近一次主、备探测结果；尚未检测为 `null`。

`status` 是展示状态：`Unknown`、`Normal`、`Failover`、`Degraded`、`Down`、`Error`。它不再承担“DNS 已经切换”的状态机条件。切换判断依赖实际地址、主备健康和连续计数。

| 条件 | 行为 |
| --- | --- |
| 当前健康且已在优先地址 | 保持；另一端故障显示降级 |
| 当前连续失败达到阈值，另一端健康 | 切换至健康地址 |
| 两端均失败 | 保留记录，显示故障 |
| 当前处于备用路径，优先地址连续恢复 | 切回优先地址 |
| 当前也已达到失败阈值，优先地址本次健康 | 允许提前恢复，避免等待期间继续使用失败源站 |
| Cloudflare 拒绝、超时或确认不匹配 | 不记录成功，保留可重试计数，重新核对远端 |
| 远端地址不属于配置的主备对 | 报错，等待修正配置，避免覆盖未知地址 |

配置同一策略的自动、手动、计划及编辑 / 删除操作共享锁。使用固定数量的锁条带限制长期删除 / 重建带来的内存增长；不同策略可能短暂共享条带。调度器只替换变化的计划，刷新配置不会重置其他策略的检测周期。

Cloudflare 与 SQLite 无法形成跨系统事务。DNS 确认后，状态和成功事件在同一个 SQLite 事务中提交；若本地保存失败，则标记需要重新核对，下一轮读取真实记录。停机取消探测和 API 等待，避免等待完整的最长超时。

## 有界资源

- 监控操作并发默认 8，每个操作最多并行探测两个源站。
- 源站探测使用新连接，避免连接池误用其他源站连接或掩盖源站变化；取消无上限的按 IP 缓存客户端。
- HTTP 响应头限制 32 KiB、响应体读取限制 32 KiB；不下载整个大文件作健康检测。
- Cloudflare 响应限制 2 MiB，支持分页。429 / 5xx 最多三次尝试；共享退避遵守 `Retry-After` 并响应取消；创建记录的 POST 不自动重试。
- SQLite 使用 WAL、单连接和 busy timeout；历史按默认 30 天清理，统计在数据库聚合。
- Web 状态刷新使用一次 `/api/status`，不在每轮轮询中请求 Cloudflare Zone 列表。

## 可扩展方向

当前版本定位为**单进程、单副本管理一组 DNS 记录**。不能让多个实例同时管理同一条记录；跨实例协调不在当前范围内。

后续如需要，可通过 `HealthChecker` 添加 TCP / 业务内容检测，通过 `DNSProvider` 添加其他 DNS 服务商，通过可选 `DNSConfigurator` 配置服务商支持的 TTL。纯决策模块可扩展为多级备用、最短驻留时间等策略，并继续使用不依赖网络的测试验证。

多副本部署需另行设计租约 / 领导者选举；多点探测需定义仲裁规则；仅替换存储驱动不会自动解决这些一致性问题。
