# 用户折扣（单用户倍率折扣）设计

## 目标

管理员可在「用户管理」为单个用户设置折扣，该用户所有消费按折扣扣费。现有平台只支持分组倍率与「用户分组×使用分组」特殊倍率，粒度到分组，无法针对个人。

已确认决策：

| 项 | 决定 |
|---|---|
| 与分组倍率关系 | 叠加相乘：最终分组倍率 = (分组特殊倍率 或 分组倍率) × 用户折扣 |
| 作用范围 | 所有模型、所有分组；钱包与订阅额度消耗都生效 |
| 取值范围 | 有效折扣 ∈ (0, 1)；0 / 1 / 未设置 = 无折扣；不支持加价和免费 |
| 可见性 | 用户在定价页看到折后倍率，消费日志显示「用户折扣」 |
| 有效期 | 不支持，管理员手动修改/清除 |

成功标准：设置折扣的用户，所有计费路径的扣费 = 原扣费 × 折扣（按现有饱和转换取整）；未设置折扣的用户行为、日志字段、缓存命中与现在完全一致。

## 数据模型

`model.User` 新增：

```go
Discount float64 `json:"discount" gorm:"default:0;column:discount"`
```

- 存储值 0 表示未设置，已有用户无需回填。不加 `type` 标签，由 GORM 按方言映射（SQLite `real`、MySQL `double`、PostgreSQL `double precision`）。
- 有效值统一由一个方法得出，所有计费读取点只用它：

```go
// EffectiveUserDiscount 返回实际参与计费的折扣；非 (0,1) 区间的值一律视为无折扣。
func EffectiveUserDiscount(discount float64) float64
```

  NaN、≤0、≥1 均返回 1。这是防御层，接口层另有校验。

- `UserBase` 增加 `Discount float64`，`ToBaseUser` 填充；`userCacheSchemaVersion` 2 → 3，旧 Redis 缓存因 schema 不匹配自动回源重建。
- `UserBase.WriteContext` 写入新 context key `constant.ContextKeyUserDiscount`（存有效折扣值）。

## 计费

### 统一入口

`types.GroupRatioInfo` 新增 `UserDiscount float64`（1 表示无折扣）。

`relay/helper/price.go` 的 `HandleGroupRatio`：

1. 按现有逻辑得到 `GroupRatio` / `GroupSpecialRatio` / `HasSpecialRatio`。
2. 从 context 读取有效折扣（缺省 1），写入 `UserDiscount`。
3. 当 `UserDiscount < 1`：`GroupRatio *= UserDiscount`；若 `HasSpecialRatio`，`GroupSpecialRatio *= UserDiscount`（保持两者一致，日志与前端「用户专属倍率」展示即折后值）。

`RelayInfo` 新增 `UserDiscount float64`，在 `GenRelayInfo` 从 context 读取，供不经过 `HandleGroupRatio` 的路径使用。

### 自动生效的路径（只读 `GroupRatioInfo.GroupRatio`，无需改动）

文本/Claude/Gemini/Responses 结算（`service/text_quota.go`、`service/quota.go:182,315`）、图片（`service/image_billing.go`）、阶梯表达式（`service/tiered_settle.go`）、按次任务（`relay/relay_task.go`）、MJ（`relay/mjproxy_handler.go`）、违规费（`service/violation_fee.go`）、Responses WebSocket（`relay/responses_websocket.go`）、渠道测试（`controller/channel-test.go`）。预扣费与结算用同一 `GroupRatio`，差额结算一致。

### 需要单独接入的路径

| 位置 | 现状 | 改动 |
|---|---|---|
| `service/quota.go:106-121` realtime/音频 | 自行重算 `groupRatio` / `actualGroupRatio` | `actualGroupRatio *= relayInfo.UserDiscount`（有效值） |
| `service/task_billing.go:409` 任务按 token 重算 | 结算时按 `task.Group` 重新读分组倍率 | `TaskBillingContext` 新增 `UserDiscount float64 json:"user_discount,omitempty"`，提交时（`controller/relay.go:628`）快照；重算时 `finalGroupRatio *= EffectiveUserDiscount(bc.UserDiscount)`。旧任务无此字段 → 0 → 视为 1，行为不变 |

不在结算时重新读取用户折扣：任务提交到结算之间管理员改折扣，不应影响已提交任务。

### 安全不变量

- 折扣只会使倍率变小（∈(0,1)），不引入溢出或负数风险；所有额度转换继续走 `common/quota_math.go` 现有调用，不新增转换点。
- 不通过 `OtherRatios` 实现（任务重算与部分路径不读取 OtherRatios，会导致漏折扣）。

## 管理接口

`PUT /api/user/`（`controller/UpdateUser` → `model.EditWithTx`）：

- 请求体 `discount` 字段为可选。解码到 `*float64` 辅助字段判断是否出现；未出现时不更新该列（旧前端/脚本不会误清）。
- 校验：必须为有限数且 0 ≤ discount ≤ 1，否则返回参数错误（400 语义，沿用 `MsgInvalidParams` 风格的 i18n 消息，新增 `MsgUserDiscountInvalid`）。1 归一化为 0 存储。
- 写入 `EditWithTx` 的 `updates` map（`"discount": value`），之后沿用现有 `updateUserCache` 刷新缓存。折扣不属于认证字段，不增加 `AuthVersion`、不撤销会话。
- 值变化时在现有审计参数中加入 `discount_old` / `discount_new`。
- 用户列表 / 详情接口随 `User` JSON 自然返回 `discount`。用户自身接口（`/api/user/self`）同样返回，用于前端展示。

用户侧所有设置接口（`UpdateUserSetting`、`UpdateSelf`、计费偏好）不接触该列，无法被用户修改。

## 展示

后端：

- `controller/pricing.go` `GetPricing`：在得到每个分组的（特殊）倍率后乘以当前登录用户的有效折扣；未登录不变。
- `controller/group.go:36` 用户可用分组列表的 `ratio`：在调用处乘以当前用户有效折扣；`service.GetUserGroupRatio` 签名不变。
- 消费日志 `other`：在 `GenerateTextOtherInfo`、`GenerateMjOtherInfo`、`service/task_billing.go:60` 等写 `group_ratio` 的位置，当 `UserDiscount < 1` 时追加公开字段 `user_discount`。`group_ratio` / `user_group_ratio` 记录折后值，保证「模型倍率 × 分组倍率」可直接核对金额。

前端（`web/`，实施前读 `web/AGENTS.md` 与 shadcn-ui skill，复用现有表单/数字输入组件）：

- `users-mutate-drawer.tsx`：编辑模式新增「用户折扣」数字输入，范围 0–1、步长 0.01，留空=无折扣；zod schema 同步校验；`types.ts` 增加 `discount`。
- 用户列表：分组列旁以现有 Badge 组件显示折扣（`0.8x`），无折扣不显示。
- 消费日志详情 `details-dialog.tsx` / 列表 / 移动卡片：存在 `user_discount` 时额外展示一行「用户折扣 0.8x」（说明性，倍率行已为折后值，不重复相乘）。
- 所有新文案走 `t('English key')`，运行 `bun run i18n:sync` 并补齐 7 种语言。

## 测试

后端（testify require/assert，表驱动，扩展现有文件，至多新增一个测试文件）：

- `HandleGroupRatio`：无折扣、折扣 0.8、折扣 + 分组特殊倍率、非法值（0、1、1.5、NaN、负数）回退为 1。
- `EffectiveUserDiscount` 边界值。
- 任务重算：BillingContext 带 / 不带 `user_discount` 的额度结果。
- `UpdateUser`：越界返回错误；字段缺省不覆盖已有折扣；1 归一化为 0。

前端：drawer 表单校验与提交 payload（现有 `__tests__` 目录约定）；`bun run build`、lint。

## 数据库验证

按 AGENTS.md 要求，在真实 SQLite、MySQL（≥5.7.8）、PostgreSQL（≥9.6）上：

1. 全新库启动迁移，确认 `discount` 列类型与默认值。
2. 用当前最新发布版本建库并写入用户数据，升级后启动，确认已有数据、索引、唯一约束保留，`discount` = 0。
3. 每种场景启动两次，确认 `AutoMigrate` 无重复 `ALTER TABLE`（幂等）。
4. 设置/清除折扣读写往返正确。

记录精确版本、命令与结果到 PR。任何一项无法执行时明确报告阻塞，不宣称完成。

## 不做

按模型折扣、折扣有效期、批量设置折扣、加价倍率、免费用户、折扣变更历史表。
