# 实现与验证记录

更新日期：**2026-11-09**。项目尚未正式部署，当前以全新安装为基准；每次提交的检查、构建和镜像结果以 [GitHub Actions](https://github.com/sck03/cloudflare-dns-failover/actions/workflows/docker-publish.yml) 为准。

## 本次改进

| 范围 | 行为与验证依据 |
| --- | --- |
| 并发隔离 | `internal/keylock` 替代 64 组锁条带，不同策略不会因编号碰撞漏检；测试覆盖互斥、取消和回收 |
| 停机 | 引擎取消并等待手动 / 自动 / 计划操作，停止后拒绝新任务；`monitor_test.go` 验证手动探测取消 |
| DNS 状态 | 外部 DNS 地址变化后清零旧源站计数，避免提前切换；回归测试覆盖阈值行为 |
| DNS 编辑保护 | 根据真实记录 ID 和名称 / 类型检查归属，阻止绕过尚未发现 ID 的策略以及创建重复记录 |
| 凭证 | Cloudflare Token / API Key 不再返回浏览器；空值编辑保留、填写新值轮换，接口测试覆盖 |
| 配置与计划 | 单一路径、严格 YAML 校验；移除旧账号补写；计划统一规范化 IPv6 和域名 |
| 资源占用 | 常用配置读取避免完整深拷贝；状态统计使用 `runtime/metrics`；前端隐藏时取消轮询 |
| 前端一致性 | 域名和账号切换取消旧请求、清理缓存；过期响应不能覆盖新视图，Node 测试验证 |
| 维护 | 合并 YAML 3.0.6 依赖升级，删除不适用的旧版升级说明，统一当前文档 |

## 验证方式

- Go 单元及回归测试、竞态检测、`go vet`、模块校验、依赖漏洞扫描。
- Node 测试覆盖状态显示、转义、接口错误、取消及过期响应；检查全部 JavaScript 语法。
- 工作流校验、Linux amd64 / arm64 / armv7 与 Windows amd64 构建。
- GitHub 发布流程另外验证容器启动、配置端口健康检查、Ping 和停机；本地环境没有 Docker，容器结果由该流程提供。
- 测试使用临时 SQLite、本地 HTTP 服务和模拟 API，不修改真实 Cloudflare DNS，也不发送真实通知。

没有对 CPU、RSS 或吞吐量作百分比承诺。界面显示 Go 堆对象占用，不等同于进程 RSS。真实证书链、防火墙、Token 权限、DNS / CDN 缓存和业务健康接口仍需在首次部署时按 [OPERATIONS.md](OPERATIONS.md) 进行故障演练。
