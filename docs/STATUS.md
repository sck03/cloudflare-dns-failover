# 实现与验证记录

更新日期：**2026-11-10**。项目尚未正式部署，当前以全新安装为基准；每次提交的检查、构建和镜像结果以 [GitHub Actions](https://github.com/sck03/cloudflare-dns-failover/actions/workflows/docker-publish.yml) 为准。

## 本轮改进

| 范围 | 改进与验证 |
| --- | --- |
| SQLite 驱动 | 删除 glebarez 的两个分叉依赖，改用 GORM 官方适配器注入 modernc 纯 Go 连接池，实际使用上游驱动的取消与资源回收修复 |
| 数据库初始化 | 应用与测试共用 `openDatabase`；统一单连接、WAL、外键和 busy timeout，初始化失败关闭连接池，成功后才发布全局数据库 |
| 文件与时间 | 数据库文件名通过 URI 转义；保存时间不含单调时钟后缀。测试覆盖特殊字符文件名、时间往返与相等查询、外键约束 |
| 锁等待 | 通过真实双连接写锁验证 100 毫秒请求超时会提前终止 5 秒 busy timeout，取消后连接仍可继续写入 |
| DNS 客户端 | 不跟随 API 重定向；本地 HTTP 测试确认 Bearer Token / Global API Key 以及 PATCH 写操作不会发送至跳转目标 |
| 弹窗一致性 | 独立 `modal.js` 统一五类弹窗的会话标记。旧保存完成不能关闭新草稿，包括重新编辑同一账号；DNS 写入只刷新原 Zone |
| 前端构建 | Tailwind 3 → 4.3.5，使用独立 CLI 与 CSS 显式扫描来源，更新渐变类，删除旧 `tailwind.config.js` |
| 发布与文档 | Docker login action 更新至 4.8.0 并固定提交；同步架构、驱动约束、浏览器要求和运维说明，精简累积的进度记录 |

## 上游版本复核

| 依赖 | 当前选择 | 依据 |
| --- | --- | --- |
| Go | 1.27.3 | [官方发布列表](https://go.dev/dl/) 当前最新稳定版，保持不变 |
| GORM / SQLite 适配器 | 1.31.2 / 1.6.0 | 当前稳定版；官方适配器支持注入连接池，避免维护适配器分叉 |
| modernc SQLite | 1.62.0 / SQLite 3.54.1 | [更新记录](https://gitlab.com/cznic/sqlite/-/blob/v1.62.0/CHANGELOG.md) 包含语句重复解析、锁等待取消及连接钩子回收修复 |
| modernc libc | 1.80.1 | 严格匹配 SQLite 1.62.0 的要求；不单独更新为 1.80.2 |
| Go 辅助依赖 | x/sys 0.50.0、x/text 0.44.0 | 更新至已发布稳定版本并执行漏洞扫描 |
| Tailwind / CLI | 4.3.5 | [官方升级说明](https://tailwindcss.com/docs/upgrade-guide)，本项目无需旧浏览器兼容 |
| GitHub Actions | login-action 4.8.0，其余保持 | 逐一核对当前使用的 action 最新发布，均以完整提交 SHA 固定 |

保留单进程、单副本、SQLite 和标准库 HTTP 架构。没有引入新的运行时服务或前端框架；新数据库能力（自定义页缓存、OFD 锁、fdatasync）暂未启用，避免在没有项目测量依据时增加全局配置与维护成本。

## 验证与边界

- Go 单元 / 回归测试、竞态检测、`go vet`、模块校验；纯决策模块语句覆盖率 100%。
- 前端 22 项 Node 测试；全部 JavaScript 语法检查、CSS 重新生成一致性检查。
- `govulncheck` 与 npm 官方源审计未发现漏洞；actionlint 校验工作流。
- `scripts/build.sh` 构建 Linux amd64 / arm64 / armv7 与 Windows amd64，运行时使用 `CGO_ENABLED=0`。
- Windows 程序通过独立临时配置启动，自定义端口健康检查、状态 / 账号接口、CSS / JavaScript / 登录页静态响应正常。

测试使用临时 SQLite、本地 HTTP 服务和模拟 Cloudflare，不修改真实 DNS，也不发送真实通知。本机没有 Docker，容器启动、Ping 和停机验证由 GitHub 发布流程执行。页面可视检查未完成：浏览器连接器内部报错，Computer Use 随后因无法可靠识别浏览器 URL 而中止；自动化测试不替代页面截图检查。

验证使用的临时数据库、程序、构建产物、依赖目录和覆盖率文件不进入提交，完成后清理。源码、锁文件、配置示例及嵌入程序所需的预编译 CSS 保留。

没有对 CPU、RSS 或吞吐量作未经测量的百分比承诺。界面显示 Go 堆对象占用，不等同于进程 RSS。真实证书链、防火墙、Token 权限、DNS / CDN 缓存和业务健康接口仍需在首次部署时按 [OPERATIONS.md](OPERATIONS.md) 进行故障演练。
