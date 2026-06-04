# AWS Bedrock anthropic_beta 字段处理记录

## 背景

Claude Code CLI 在每次请求时会携带如下 `anthropic-beta` header：

```
anthropic-beta: claude-code-20250219,interleaved-thinking-2025-05-14,redact-thinking-2026-02-12,context-management-2025-06-27,prompt-caching-scope-2026-01-05,effort-2025-11-24
```

这些 flag 是 Anthropic 直连 API 的 beta 特性标识，部分在 AWS Bedrock 上并不支持。当请求通过 new-api 路由到 AWS Bedrock 渠道时，`relay/channel/aws/dto.go` 的 `formatRequest` 函数会将 header 中的所有 flag 原样写入 Bedrock 请求 body 的 `anthropic_beta` 字段，导致 Bedrock 返回 400 invalid beta flag 错误。

---

## AWS Bedrock 官方支持的 anthropic_beta 值

参考：https://docs.aws.amazon.com/bedrock/latest/userguide/model-parameters-anthropic-claude-messages-request-response.html

| Flag | 用途 | 备注 |
|---|---|---|
| `computer-use-2024-10-22` | Computer use（旧版） | |
| `computer-use-2025-01-24` | Computer use | |
| `computer-use-2025-11-24` | Computer use（最新） | |
| `effort-2025-11-24` | Effort 参数（控制 token 消耗策略） | 仅 Claude Opus 4.5 |
| `context-management-2025-06-27` | Tool use clearing / Memory tool | |
| `tool-search-tool-2025-10-19` | Tool search | 仅 InvokeModel/InvokeModelWithResponseStream |
| `tool-examples-2025-10-29` | Tool examples（Opus 4.5） | |

**不支持的 flag（Anthropic 直连 API 专属）：**

| Flag | 说明 |
|---|---|
| `claude-code-20250219` | Claude Code CLI 专属标识 |
| `interleaved-thinking-2025-05-14` | 交错思考模式 |
| `redact-thinking-2026-02-12` | 思考内容脱敏 |
| `prompt-caching-scope-2026-01-05` | Prompt 缓存作用域（Bedrock 有自己的缓存机制） |

---

## 相关 PR 分析

### PR #3066（已合并，commit `ad326beb`）

**问题**：Bedrock 不接受 HTTP header 形式的 `anthropic-beta`，需要将其转换为 JSON body 中的 `anthropic_beta` 数组字段。同时，Claude Code 等客户端发来的请求体中可能包含 Bedrock 不认识的字段，导致解析失败。

**修复内容**：
- 新增 `ResolveHeaderOverride(info, c)` 函数，支持运行时 header 覆盖规则
- `doAwsClientRequest` 中调用该函数，将覆盖后的 header 应用到 Bedrock 请求
- `formatRequest` 中读取 `anthropic-beta` header 并转换为 `anthropic_beta` JSON 数组写入 body
- 请求体解析从 `ShouldBindJSON` 改为 `common.DecodeJson`，容忍未知字段

**局限**：只处理了"应用 override"的场景，没有处理"通过 override 删除 beta"的场景，也没有对 flag 做合法性过滤。

### PR #4955（未合并，截至 2026-06-01）

**问题**：当 `UseRuntimeHeadersOverride=true` 但 override map 中不包含 `anthropic-beta` 时，原有逻辑仍会把请求中携带的 `anthropic-beta` 写入 Bedrock body，导致 override 的"删除"语义失效。

**修复内容**：
- 在 `doAwsClientRequest` 中，若 runtime override 激活且 override map 中无 `anthropic-beta`，则主动清除 `requestHeader` 中的该字段，使 `formatRequest` 不再写入 `anthropic_beta`
- 新增测试 `RemovesAnthropicBetaWhenRuntimeOverrideDeletesIt` 覆盖该场景

**与 PR #3066 的关系**：是对 #3066 的补充修复，两者配合才能完整支持 override 的增删改语义。

---

## 正确的长期修复方案

在 `formatRequest`（`relay/channel/aws/dto.go`）中，对从 header 读取的 beta flag 做**白名单过滤**，只保留 Bedrock 明确支持的值：

```go
var bedrockSupportedBetaFlags = map[string]bool{
    "computer-use-2024-10-22":      true,
    "computer-use-2025-01-24":      true,
    "computer-use-2025-11-24":      true,
    "effort-2025-11-24":            true,
    "context-management-2025-06-27": true,
    "tool-search-tool-2025-10-19":  true,
    "tool-examples-2025-10-29":     true,
}
```

过滤逻辑插入在 `strings.Split` 之后、`json.Marshal` 之前：

```go
anthropicBetaValues := requestHeader.Get("anthropic-beta")
if len(anthropicBetaValues) > 0 {
    var filtered []string
    for _, flag := range strings.Split(anthropicBetaValues, ",") {
        flag = strings.TrimSpace(flag)
        if bedrockSupportedBetaFlags[flag] {
            filtered = append(filtered, flag)
        }
    }
    if len(filtered) > 0 {
        betaJson, err := json.Marshal(filtered)
        if err != nil {
            return nil, err
        }
        awsClaudeRequest.AnthropicBeta = betaJson
    }
}
```

同时建议合并 PR #4955，补全 override 删除语义。

**选择白名单而非黑名单的原因**：Bedrock 文档明确列出了支持的值，未知 flag 大概率被拒绝；黑名单需要持续追踪 Anthropic 新增的 API-only flag，维护成本更高。

---

## 本次临时处理决策

**决策时间**：2026-06-01

**决策**：暂时完全移除 `anthropic_beta` 字段的透传，不向 Bedrock 传递任何 beta flag。

**原因**：
1. 白名单方案需要持续维护，当前优先保证稳定性
2. Claude Code 发来的 flag 中大部分 Bedrock 不支持，直接透传必然 400
3. 绝大多数用户使用 Bedrock 时不依赖 beta 特性，移除影响面极小
4. 这是临时方案，后续应按上述白名单方案正式修复

**代码变更位置**：`relay/channel/aws/dto.go` `formatRequest` 函数，原 header 读取逻辑保留为注释，注明原因。

**后续 TODO**：
- [x] 实现白名单过滤方案替换本次临时修复（已完成，当前白名单：`context-management-2025-06-27`, `interleaved-thinking-2025-05-14`）
- [ ] 跟进合并 PR #4955
- [ ] 关注 AWS Bedrock 文档，及时更新白名单
