# 运维、故障排查与 API

## “网站坏了但没有切换”

按顺序核对：

1. **检测类型。** Ping 成功不等于网页正常。改为 HTTP/HTTPS，选择返回业务就绪状态的 `/health` 等接口。
2. **源站直连。** `original_ip` / `backup_ip` 应为真实服务器，不是 Cloudflare 边缘 IP。HTTP URL 保留业务域名，用于 Host 和 SNI。源站需要允许监控机器访问相应端口。
3. **证书与跳转。** 证书错误、跨主机重定向、最终非 2xx 均会显示在主 / 备错误字段。Origin CA / 自签名证书使用专门开关。
4. **备用健康。** 系统不会主动切到已经检测失败的备用端。主备均失败时保留当前 DNS 并显示 `Down`。
5. **失败阈值。** 失败必须连续；中间成功会清零。检查 `interval`、`timeout`、`retries` 和队列负载。
6. **Cloudflare 错误。** 查看 `last_error`，核对 Zone、名称、A/AAAA/CNAME 类型、Record ID、Token 的 Zone Read / DNS Edit 权限。
7. **多条记录。** 同名同类型有多条记录时填写确切 ID；其他记录仍可能把客户端送往故障服务器。IPv4、IPv6 都存在时分别配置策略。
8. **真实 DNS 与客户端。** `current_ip` 是控制面的最近确认值。用权威 DNS 查询和新的客户端连接核对；已经缓存的 DNS、CDN 缓存、长连接不会因为一次 PATCH 立即失效。

## 预期恢复时间

在监控队列没有拥塞、故障持续且 API 正常时，检测时间大致由剩余调度等待、连续失败次数和单次检查耗时组成。若检查比 `interval` 长，后续重叠触发会跳过，因此不能只用 `interval × retries` 承诺最坏时延。

DNS 更新后还需考虑旧 TTL、Cloudflare 内部传播、递归解析器 / 浏览器缓存和连接复用。上线前降低 TTL 并等待原 TTL 窗口，才能测试低 TTL 下的切换效果。代理模式下 Cloudflare 使用自动 TTL，本工具不能控制所有边缘缓存行为。

## 故障演练

使用独立测试域名和已验证的备用服务：

1. 确认两端业务与证书可正常响应，并等待控制台第一次核对 Cloudflare。
2. 在主端让健康接口返回 503；确认达到失败阈值后才切换，历史中记录正确的起止地址。
3. 恢复主端，观察连续成功阈值，确认切回。
4. 两端都返回失败；确认没有把 DNS 切到已知不可用的备用。
5. 将测试 Token 的编辑权限移除；确认操作报错，本地不会记录虚假的切换成功。
6. 保存计划切到备用；使该备用故障，检查自动保护和随后恢复到计划优先地址。

通知测试请使用专用测试接收方。生产演练、真实 DNS 修改和实例部署由操作者控制。

## HTTP API

所有 `/api` 管理接口需要登录 Cookie；也支持同一 JWT 的 `Authorization: Bearer ...`。登录接口为 `POST /api/auth/login`，请求 `{"token":"登录密码"}`；令牌保存在 HttpOnly、SameSite Cookie 中，并在 JSON 的 `token` 字段返回以兼容 API 客户端。不要把密码放在 URL 参数里。

反向代理应保留原始 `Host`，例如 Nginx 使用 `proxy_set_header Host $http_host;`。写操作会核对浏览器的 Origin，代理改写 Host 会导致请求被拒绝。

| 方法 / 路径 | 用途 |
| --- | --- |
| `GET /healthz` | 进程 / 数据库存活，供容器检查 |
| `GET /api/status` | 监控、计划、最近事件、24 小时高频故障、Go 堆和协程数 |
| `GET /api/monitors` | 监控列表 |
| `POST /api/monitors` | 创建配置，返回 201；后台开始检测并核对 DNS |
| `PUT /api/monitors/{id}` | 提交完整监控配置；运行状态不可从请求覆盖 |
| `DELETE /api/monitors/{id}` | 删除策略及计划，不修改远端 DNS |
| `POST /api/monitors/{id}/restore` | 验证主端并恢复；可传 `{"proxied":true}` |
| `POST /api/monitors/{id}/schedule` | `{"enabled":true,"cron":"0 */2 * * *","target_ip":"192.0.2.20"}`；关闭传 `{"enabled":false}` |
| `GET /api/zones` | 当前管理账号的 Zone 列表，自动分页 |
| `GET/POST /api/zones/{zoneId}/records` | DNS 列表 / 创建 |
| `PUT/DELETE /api/zones/{zoneId}/records/{recordId}` | 修改 / 删除未受监控管理的记录 |
| `GET/POST /api/config` | 读取 / 保存管理账号 Token 与通知设置 |
| `/api/cloudflare-accounts` | 账号列表 / 创建；`PUT/DELETE /{id}` 修改 / 删除，`POST /{id}/activate` 激活管理账号 |

监控 JSON 使用 `account_name`、`cf_domain`、`cf_zone_id`；对应的 YAML 字段分别是 `account`、`domain`、`zone_id`。其余监控字段参照示例。

`PUT` 中不提供 `schedules` 则保留已有计划；显式传 `"schedules":[]` 则清空。简化计划接口只改计划，不覆盖策略其他字段。手动 / 计划失败会返回非 2xx 和 `{"error":"具体原因"}`。

## 日常维护

配置和数据库是持久化数据，不能放到镜像构建上下文或提交到 Git。使用停机备份或 SQLite 的在线备份机制；不要只复制仍在写入的单个 `.db` 文件。

默认事件保留 30 天。通知队列满时会记录日志，避免无限创建工作协程。启用 `server.debug` 可记录 HTTP 方法与路径，默认关闭以减少日志开销。

工作流失败时，在 [Actions](https://github.com/sck03/cloudflare-dns-failover/actions) 查看失败步骤及其日志。`cfguard-binaries` 附件包含 SHA256 校验；镜像可固定 `sha-xxxxxxx` 标签或摘要，避免升级时版本不明确。
