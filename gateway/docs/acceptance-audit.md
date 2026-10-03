# v4 后的逐项验收复核

从 OC `f781ee2c1114c019d4b25136484917d7641ad9d4`、Multica
`b2b01356b1ca588e29a6db43200c2ed0f847b2af` 两个干净 HEAD 开始。没有
重新采用旧环境 PASS，也没有把简单重复运行记为新增成果。

## 新发现与修复

真正并行完成两个 subject 的授权时，PG Serializable 事务可返回 40001。
旧接口将它报告为身份校验失败。新增存储回归先复现失败，再限制为仅对
40001/40P01（数据库已明确中止的事务）最多重试四次；每次重新读取 ticket、
owner 和当前权限。不会重试 native OAuth 或任何 provider 调用；耗尽返回
503 `CONNECT_RETRY_REQUIRED`。进程重启、双 subject、同 ticket 并发重试
现在通过，连接和 audit 数量不增加。

## 新证据

- C03/C04：真实 SDK 账户列表交集、重复值、数组顺序、过滤后分页、跨项目与
  权限变化后的 cursor 拒绝。此前证据主要是目录分页。
- A02/A06：真实 native 拒绝授权闭环及交易过期后的迟到 callback，均不激活。
- A05/A07/A08：gateway 真实进程在 CREATING、VERIFYING 阶段被终止/重启；
  双 subject 并发 OAuth、并发重复完成；独立 native IDs、精确 owner 和 audit。
  另注入 native 创建响应丢失，重启后不再次创建，保留未知事务和加密 orphan。
- S04/S10：实际 session 不能进入两个管理面；实际权威服务进程停机后旧会话拒绝。
- S05：实际 Codex 的配置、显式环境、协议 stdout 和诊断 stderr 扫描已知
  fixture control/native/provider/source secrets；仍只调用离线 RPC、无 model turn。
- S07/S08：新 session 重算缩小 action 交集；两小时前入队的任务在 claim 时
  获得新签发时间与超过59分钟有效期。使用可控持久时间，不声称等待了两小时。
- M02/M04：精确五工具集合；未授权账号与隐藏 action 不出现在视图；撤权后五入口
  都拒绝；workbench/proxy/bash 等调用和未支持 HTTP 路由明确失败。
- E05：90日前的 SUCCEEDED/UNKNOWN 记录仍保留幂等边界、不重派；不是生产清理演练。
- U01：脚本从原 Multica 基线与固定 candidate commit 提取 SDK，在同一
  loopback fixture 上实际运行七类请求，比较完整请求 header、URI、JSON body 与
  MCP headers，并单独禁止私有鉴权 header。Go workspace、proxy、sumdb 禁用；
  只用已存在缓存。原基线 `a9e82c7` 对 candidate `b2b01356b` PASS，规范化报文
  SHA256 `39b8591a8e3ec5ae67df90741ef46e0477524b6d4bc5e512458466a6414be7c4`。

这轮还执行了与产品事务修复相关的完整 gateway race 回归；没有重新运行无关
前端/238项 native 测试来增加数字。OC fix-check 通过。新测试的 Go 源码随恢复包保留。

## 仍需外部证据

原表40个 ID 已完整映射。A01 的真实账号、E03 的真实第三方结果、M03 的模型
选工具行为、真实 grant revoke、U02 的目标部署验收仍不能由离线 fixture 代替。
A04 的登录中间件与 gateway 身份绑定为分层验证，未宣称双浏览器 UI 或模型 E2E。
未知 OAuth 创建/未证明独占 grant 的 orphan 必须隔离保留，不能为“清理通过”而
删除密文或重试远程操作。首版没有写 Action、任意代理、工作台或远程 Bash。

没有选定真实 provider、action/scopes、真实 callback origins 或生产目标版本。
不要把方案中 Google 的风险示例误当作 provider 选择。具体最小输入在
[配置和部署差异](../ops/CONFIGURATION.md)。源码支持的 provider 数量不等于可用授权。

根 Compose 模板指向上游 latest 且未传兼容变量，当前 gateway 仅监听 loopback；
这些已明示为部署差异，没有偷偷修改默认信任、扩大监听范围或启用 provider。
原迁移468历史保留。原任务与其他项目未修改，无 push/PR/merge/生产操作。

独审最终确认无新增阻断性安全缺陷；所指出的完整 header 比较及隐藏 action/其他
账号 execute 负向证据均已补齐。当前 Multica SDK 与 wire candidate 的源码差异
为空（当前提交仅新增测试/文档），未通过改 SDK 消除基线差异。

当前 gateway 本地镜像：`sha256:b61e391a71c2f299f9e47d1596ba5f93458addd5ce765c65aba620b70f26e1d2`，构建代码提交 `4f3aa8d8`。
只读、非 root、无网络且禁止拉取的镜像启动检查通过；没有发布镜像。
