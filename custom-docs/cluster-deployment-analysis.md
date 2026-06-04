# 集群部署分析：主从分工、高可用与超时配置

基于源码分析（分支 `feat/channel-readonly-common-user`），记录集群部署的关键行为与推荐配置。

---

## 一、主从判断逻辑

```go
// common/init.go:84
IsMasterNode = os.Getenv("NODE_TYPE") != "slave"
```

只要不设置 `NODE_TYPE=slave`，节点就是 master。主从之间**没有任何直接通信**，都独立连接同一个 MySQL 和 Redis。

---

## 二、仅在 Master 运行的任务

| 代码位置 | 功能 |
|---|---|
| `model/main.go:198,238` | 数据库 schema 迁移（AutoMigrate） |
| `controller/channel-test.go:988` | 定时自动测试渠道健康 |
| `controller/channel_upstream_update.go:654` | 定时拉取上游模型列表 |
| `service/subscription_reset_task.go:31` | 订阅配额周期重置 |
| `service/codex_credential_refresh_task.go:37` | Codex 凭证自动刷新 |
| `main.go:134` | MJ / 视频等异步任务状态轮询 |

实现方式均为 `if !common.IsMasterNode { return }`，slave 启动时直接跳过。

---

## 三、Master 挂掉后的影响

**不受影响（slave 独立运转）：**
- ✅ 接收并转发 API 请求（relay 路由无条件注册）
- ✅ 实时扣减 quota / 写 billing 记录（直连 MySQL）
- ✅ 写请求日志（直连 LOG_DB）
- ✅ Token 鉴权、限流、用户查询

**受影响：**
- ❌ 订阅用户额度周期重置停止（对订阅制用户有实质影响）
- ❌ 渠道自动健康检测停止，disabled 渠道不会自动恢复
- ❌ 渠道上游模型列表不再同步
- ❌ MJ / 视频异步任务状态不更新（任务挂起）
- ❌ Codex 凭证过期后不自动刷新
- ❌ 部署新版本时无法执行 DB schema 升级

**真正的单点风险在 MySQL 和 Redis，不在 master 节点本身。**

---

## 四、ALB 多节点路由

### 控制台访问

Session 存储方式（`main.go:179`）：

```go
store := cookie.NewStore([]byte(common.SessionSecret))
```

使用 **Cookie Store**，session 数据加密存在客户端 cookie 中，服务端无状态。两节点只需 `SESSION_SECRET` 相同（集群必须配置），任何节点都能解密登录态。

**请求打到 slave 时控制台完全可用，CRUD 直连同一个 MySQL。**

### API 请求

`SetRelayRouter` 在两节点无条件注册，relay 转发直接读 DB/Redis，不经过 master。ALB 可按轮询 / 随机 / 最少连接调度，两节点完全对等。**无需配置粘性路由。**

### Claude Code 粘性会话

Claude Code CLI 走 API Key 鉴权，绕过 session 机制。对话历史由 CLI 本地维护，每次请求携带完整 messages 数组，new-api 无状态透传。SSE 流式响应期间单个 HTTP 连接不会中途换节点（ALB 按连接路由），无粘性问题。

---

## 五、超时参数分工

```
Client ── ALB ──────── new-api ─────────────────── Upstream
              ↑                ↑                          ↑
        idle timeout     PingInterval(10s)        STREAMING_TIMEOUT(300s)
        (ALB配置)         向客户端发心跳            等待上游下一个chunk的最长静默时间
                                                   RELAY_TIMEOUT = 整个HTTP请求总超时(0=不限)
```

| 参数 | 默认值 | 保护对象 | 触发后果 |
|---|---|---|---|
| ALB idle timeout | 用户配置 | 防止 ALB 主动断空闲连接 | 客户端 504 / 连接重置 |
| `PingInterval` | 10s | 向客户端发 SSE 心跳，刷新 ALB idle timer | 无（维活用） |
| `STREAMING_TIMEOUT` | 300s | 上游 chunk 间最大静默时间 | new-api 主动终止流 |
| `RELAY_TIMEOUT` | 0（无限） | 整个上游 HTTP 请求总时长 | new-api 中断上游连接 |

### STREAMING_TIMEOUT 的精确语义

每收到上游一个 SSE chunk 就重置计时器（`stream_scanner.go:229`），只有上游**连续静默**超过该值才触发超时。对 Claude extended thinking，思考阶段会流式输出 thinking token，每个 token 重置计时，不会误触发。

### 约束关系

- SSE：ALB idle timeout > PingInterval(10s) 即可，心跳维活
- 非流式：ALB idle timeout > RELAY_TIMEOUT > 上游实际生成时间，否则 ALB 先断 → 额度扣了客户端 504

---

## 六、推荐配置（当前已验证合理）

```yaml
# ALB
idle_timeout: 600s

# new-api 环境变量
STREAMING_TIMEOUT=300   # 默认值，普通模型 + extended thinking 均足够
RELAY_TIMEOUT=0          # 不限制总时长，适合长流式会话
```

**为何 RELAY_TIMEOUT=0 可接受：** ALB 600s 超时切断客户端后，goroutine 会等待上游自己断开。Anthropic 等主流上游自身有 60~120s 响应超时，goroutine 不会长时间泄漏。

**若需收紧（非流式请求量大）：** 设置 `RELAY_TIMEOUT=580`（略小于 ALB），确保 new-api 先于 ALB 断开连接，避免 goroutine 积压。代价是单次流式会话总时长不能超过 580s。
