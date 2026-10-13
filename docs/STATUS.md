# 完成进度与验证记录

本轮基于原始 `main` 提交 `a4682df`，核实日期为 **2026-10-13**。此文件记录实现范围；每次提交的 CI 结果和编译附件见 [GitHub Actions](https://github.com/sck03/cloudflare-dns-failover/actions/workflows/docker-publish.yml)。

| 工作项 | 状态 | 审阅依据 |
| --- | --- | --- |
| 复现 DNS 失败却显示成功 | 已完成 | `regression_test.go`、`cloudflare_test.go`；原代码在回归测试中失败 |
| 主备同时检测、连续计数、双故障保护 | 已完成 | `internal/failover`、`monitor_test.go` |
| 手动 / 计划 / 自动操作串行化 | 已完成 | `Engine.Lock`、并发切换测试 |
| 定时接口、优先地址持久化、故障后恢复 | 已完成 | `SaveSchedule`、计划备用故障测试 |
| HTTP Host/SNI、HTTPS 协议、证书开关、重定向 | 已完成 | `internal/health/checker_test.go` |
| Cloudflare 响应确认、失效 ID、歧义、分页、退避 | 已完成 | `internal/cloudflare/client_test.go`、`cloudflare_test.go` |
| 预先降低 TTL，不改当前源站 / 代理设置 | 已完成 | `ConfigureTTL` 及专门回归测试 |
| YAML 只初始化一次、原子保存、配置并发读写 | 已完成 | `api_test.go`、配置和初始化测试 |
| Go / 依赖 / GitHub Actions 稳定版升级 | 已完成 | `go.mod`、`Dockerfile`、固定 SHA 的工作流 |
| 移除 Gin、重复请求、无效按钮绑定和多余资源 | 已完成 | 标准库路由、精简后的 JS、移除 Chart.js 和旧 ICO |
| 桌面 / 移动界面、错误展示、失败后重试 | 已完成 | 前端测试与本地浏览器检查 |
| 架构、运维、升级与持续构建说明 | 已完成 | 本目录文档、README、工作流和脚本 |

## 验证方式

- 本地：`go test ./...`、`go vet ./...`、`govulncheck`、Node 前端测试、Actionlint 和 Shell 语法检查。
- GitHub：`scripts/check.sh` 执行竞态检测、覆盖率和漏洞扫描；通过后交叉编译 Linux amd64 / arm64 / armv7、Windows amd64，验证容器启动 / 健康检查 / 停机，再发布多架构镜像。
- 浏览器：真实 Chrome 无头模式检查状态显示、策略编辑、定时保存、DNS 失败提示及再次重试、桌面和窄屏布局。浏览器场景使用模拟 API；Go 接口测试使用临时 SQLite 和本地测试服务。
- 所有回归测试均不要求真实 Cloudflare 凭证，不修改生产 DNS，也不发送真实通知。

## 体积测量

在本轮改造中，用 Go 1.27.2、Windows amd64、`-trimpath -ldflags="-s -w"` 比较移除 Gin 前后的构建：约 **21.2 MiB → 15.5 MiB，减少约 27%**。对应显式模块条目从 46 降至 19。此对照用于衡量 HTTP 层精简；不同架构、编译器和后续源码变化会影响实际大小，以 Actions 附件为准。

不再保留原 README 中未经证明的“内存 <15MB”“CPU ~0%”或“零停机”承诺。界面显示 Go 堆分配量，不等同于进程 RSS。

## 尚需部署环境验证的边界

真实站点的证书链、源站防火墙、Cloudflare Token 权限、DNS / CDN 缓存和业务健康接口需要在实际部署环境检查。测试覆盖程序行为，无法替代运营环境的故障演练。步骤见 [OPERATIONS.md](OPERATIONS.md)。

多级备用、多副本协调和多点仲裁属于后续可选扩展，不包含在本轮交付范围内。
