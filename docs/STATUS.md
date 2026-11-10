# 实现与验证记录

更新日期：**2026-11-10**。项目尚未正式部署，当前以全新安装为基准；每次提交的检查、构建和镜像结果以 [GitHub Actions](https://github.com/sck03/cloudflare-dns-failover/actions/workflows/docker-publish.yml) 为准。

## 本轮改进

| 范围 | 改进与验证 |
| --- | --- |
| 调度隔离 | 周期检查和定时切换分别更新；新增 / 删除切换计划不推迟检测，修改检测间隔不重建切换计划；删除策略清理两类任务 |
| 异常恢复 | 调整 Cron 包装顺序，修复 panic 恢复后执行令牌丢失、任务永久跳过的问题；验证重复执行仍跳过重叠任务且不阻塞其他任务 |
| 调度开销 | 用一个结构化计划表替代注册表与字符串签名表；直接注册周期时长，移除无必要的格式化与 Cron 字符串解析 |
| 数据库取消 | 普通监控状态保存与定时清理响应引擎取消；历史清理显式接收数据库句柄，解除全局数据库依赖 |
| 切换提交 | Cloudflare 确认后给本地事务独立 5 秒期限；客户端断开仍尝试保存，连接池拥塞不再无限等待，失败触发重新核对 |
| 记录选择 | Web 策略表单支持查看、修改、清空 Record ID；完整 PUT 不再暗中保留空 ID，记录身份变化会清除旧 DNS 状态 |
| 界面一致性 | 保存、删除、恢复和计划修改共用一次状态刷新；当前位于主地址但优先地址仍为备用时，也可恢复主地址偏好 |
| 文档与清理 | 同步 Record ID 的 PUT 语义、调度与停机边界；清理本轮验证依赖、程序、临时配置、数据库和覆盖率文件 |

保留单进程、单副本、SQLite 和标准库 HTTP 架构，没有引入新的运行时服务、前端框架或兼容层。保持 Go 1.27.3、GORM 官方 SQLite 适配器、modernc SQLite 1.62.0 / libc 1.80.1 和 Tailwind 4.3.5；本轮不修改依赖版本。

## 验证与边界

- 先用回归测试复现计划修改推迟检测、panic 后任务永久停用、取消后仍等待连接、切换提交无期限四类问题，再验证修复。
- `scripts/check.sh`：Go 格式、模块校验、`go vet`、竞态 / 覆盖率测试、漏洞扫描、Shell / JavaScript 语法、24 项前端测试、CSS 生成一致性；纯决策模块语句覆盖率 100%。
- `govulncheck` 与 npm 官方源审计未发现漏洞；actionlint 校验工作流。
- `scripts/build.sh` 构建 Linux amd64 / arm64 / armv7 与 Windows amd64，运行时使用 `CGO_ENABLED=0`。
- 使用独立临时配置运行 Windows 程序：自定义端口健康检查、状态 / 账号 / Zone 接口、CSS / JavaScript / 登录页正常，嵌入 HTML 包含新增 Record ID 字段。

测试使用临时 SQLite、本地 HTTP 服务和模拟 Cloudflare，不修改真实 DNS，也不发送真实通知。本机没有 Docker，容器启动、Ping 和停机验证由 GitHub 发布流程执行。页面可视检查未完成：浏览器连接器报错 `Cannot read properties of undefined (reading 'timeoutMs')`，原生窗口工具随后因无法可靠识别当前 URL 自动停止。接口和前端自动化验证不替代截图检查。

验证使用的临时数据库、程序、构建产物、依赖目录和覆盖率文件已集中移至 Git 忽略的 `_cleanup/`，供手动删除，不进入提交。源码、锁文件、配置示例及嵌入程序所需的预编译 CSS 保留。

没有对 CPU、RSS 或吞吐量作未经测量的百分比承诺。界面显示 Go 堆对象占用，不等同于进程 RSS。真实证书链、防火墙、Token 权限、DNS / CDN 缓存和业务健康接口仍需在首次部署时按 [OPERATIONS.md](OPERATIONS.md) 进行故障演练。
