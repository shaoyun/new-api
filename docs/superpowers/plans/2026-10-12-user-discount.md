# 用户折扣 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 管理员可为单个用户设置 (0,1) 折扣，该用户所有计费路径的分组倍率乘以折扣。

**Architecture:** `users.discount` 列 → `UserBase` 缓存 → gin context → `RelayInfo.UserDiscount`；`HandleGroupRatio` 统一相乘，realtime 预检与任务重算两处单独接入；日志追加 `user_discount`，定价页/分组列表按折后倍率展示；前端用户抽屉可编辑。

**Tech Stack:** Go 1.26 / Gin / GORM v2；React 19 / TanStack / zod / vitest；SQLite + MySQL + PostgreSQL。

**Spec:** `docs/superpowers/specs/2026-10-12-user-discount-design.md`

## Global Constraints

- 执行前读 `AGENTS.md`、`.agents/rules/billing.md`（全文）；改 `web/` 前读 `web/AGENTS.md` 与 shadcn-ui skill。
- 有效折扣 ∈ (0,1)；存储 0 = 无折扣；接口接受 [0,1]，1 归一化为 0；NaN/Inf/越界 → 参数错误。
- JSON 一律 `common.Marshal/Unmarshal/DecodeJson`；不直接调用 `encoding/json`。
- 额度转换只走 `common/quota_math.go` 现有调用，不新增 bare cast。
- 不写 `PriceData.OtherRatios`；折扣只乘在分组倍率上。
- Go 测试用 testify `require`/`assert`，扩展现有测试文件；整个功能最多新增 1 个 Go 测试文件（本计划不新增）。
- 前端文案 `t('English key')`，`bun run i18n:sync` 后补齐 7 种语言。
- 每个 Go 修改文件 `gofmt`。
- 提交信息结尾：`Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`。

## Review Focus

1. 旧前端 / 脚本 PUT `/api/user/` 不带 `discount` → 已有折扣必须保留（Task 2 测试）。
2. Redis 中 schema=2 的旧用户缓存 → 必须回源，不能把折扣读成 0 后继续沿用旧缓存（Task 1 测试）。
3. 用户有分组特殊倍率时 → 折扣同样生效，`GroupRatio` 与 `GroupSpecialRatio` 均为折后值（Task 3 测试）。
4. 任务提交后管理员修改折扣 → 结算按提交时快照；旧任务无 `user_discount` → 按 1 结算（Task 3 测试）。
5. 前端输入框清空 → 提交 `discount: 0`（清除），而非省略字段导致无法清除（Task 5 测试）。

---

### Task 1: 数据模型、缓存与 context

**Files:**
- Modify: `model/user.go`（`User` 结构体、`ToBaseUser`、`EditWithTx` 的 updates）
- Modify: `model/user_cache.go`（`UserBase`、`WriteContext`、`userCacheSchemaVersion` 2→3）
- Modify: `constant/context_key.go`
- Modify: `relay/common/relay_info.go`（`RelayInfo` 字段 + `GenRelayInfo` 赋值，约 617 行）
- Test: `model/user_update_test.go`、`model/user_cache_auth_version_test.go`

**Interfaces:**
- Produces:
  - `User.Discount float64` — tag `json:"discount" gorm:"default:0;column:discount"`
  - `User.DiscountUpdate *float64` — tag `json:"-" gorm:"-:all"`；非 nil 时 `EditWithTx` 把 `"discount": *DiscountUpdate` 加进 updates
  - `func EffectiveUserDiscount(discount float64) float64` — (0,1) 原样返回，其余（含 NaN）返回 1
  - `func NormalizeUserDiscount(discount float64) (float64, error)` — NaN/Inf/<0/>1 返回 error；1 返回 0；其余原样
  - `UserBase.Discount float64 json:"discount"`
  - `constant.ContextKeyUserDiscount ContextKey = "user_discount"`，`WriteContext` 写入 `EffectiveUserDiscount(user.Discount)`
  - `RelayInfo.UserDiscount float64` — `GenRelayInfo` 中读 `c.GetFloat64(string(constant.ContextKeyUserDiscount))`，不在 (0,1) 内（含读不到的 0）则置 1。`model` 已 import `relay/common`，此处不能反向 import `model`，故内联该判断；`WriteContext` 已写入有效值，这里只是兜底

- [ ] **Step 1: 写失败测试** — `user_update_test.go` 新增：
  - `TestEffectiveAndNormalizeUserDiscount` 表驱动：Effective `{0→1, 0.8→0.8, 1→1, 1.5→1, -0.1→1, NaN→1}`；Normalize `{0→0, 0.8→0.8, 1→0}` 无 error，`{-0.1, 1.01, NaN, +Inf}` 有 error。
  - `TestUserEditPreservesDiscountUnlessUpdated`：建用户 Discount=0.8；`Edit` 时 `DiscountUpdate=nil` → 库中仍 0.8；`DiscountUpdate=&0.5` → 0.5；`&0` → 0。
  - `user_cache_auth_version_test.go` 新增 `TestUserCacheCarriesDiscount`：`ToBaseUser().Discount == 0.8`，`CacheSchema == 3`；`WriteContext` 后 `c.GetFloat64("user_discount") == 0.8`，Discount=0 时为 1。
- [ ] **Step 2: 运行** `go test ./model/ -run 'Discount' -v` → 编译失败/FAIL
- [ ] **Step 3: 实现** 上述 Interfaces。
- [ ] **Step 4: 运行** `go test ./model/ -v` → 全部 PASS（含原有 user cache 测试）
- [ ] **Step 5: 提交** `feat(user): add per-user discount column and cache field`

### Task 2: 管理接口

**Files:**
- Modify: `controller/user.go`（`UpdateUser`，649 行起）
- Modify: `i18n/keys.go`、`i18n/locales/{en,zh-CN,zh-TW}.yaml`
- Test: `controller/user_manage_test.go`

**Interfaces:**
- Consumes: `model.NormalizeUserDiscount`、`User.DiscountUpdate`
- Produces: `PUT /api/user/` 可选字段 `discount`；`i18n.MsgUserDiscountInvalid = "user.discount_invalid"`（en: `Discount must be between 0 and 1`；zh-CN: `折扣必须在 0 到 1 之间`；zh-TW: `折扣必須在 0 到 1 之間`）

- [ ] **Step 1: 写失败测试** `TestUpdateUserDiscount`（复用 `setupManageUserTestDB`）表驱动：
  - body 含 `"discount":0.8` → 成功，库中 0.8
  - body 不含 discount（库中原 0.8）→ 成功，仍 0.8
  - `"discount":1` → 库中 0
  - `"discount":1.5` / `-0.1` → `success=false`，库中不变
- [ ] **Step 2: 运行** `go test ./controller/ -run TestUpdateUserDiscount -v` → FAIL
- [ ] **Step 3: 实现** 先 `io.ReadAll` 请求体，`common.Unmarshal` 到 `model.User`，再 `common.Unmarshal` 到 `struct{ Discount *float64 \`json:"discount"\` }`；非 nil 时 `NormalizeUserDiscount`，失败 `common.ApiErrorI18n(c, i18n.MsgUserDiscountInvalid)` 返回；成功赋 `updatedUser.DiscountUpdate`。值与 `originUser.Discount` 不同时 `auditParams["discount_old"/"discount_new"]`。
- [ ] **Step 4: 运行** `go test ./controller/ -v` → PASS
- [ ] **Step 5: 提交** `feat(user): allow admins to set user discount`

### Task 3: 计费接入

**Files:**
- Modify: `relay/helper/price.go`（`HandleGroupRatio`）
- Modify: `service/quota.go:106-121`（realtime 预检）
- Modify: `model/task.go`（`TaskBillingContext`）、`controller/relay.go:628`、`service/task_billing.go:409-416`
- Test: `relay/helper/price_test.go`、`service/task_billing_test.go`

**Interfaces:**
- Consumes: `RelayInfo.UserDiscount`、`model.EffectiveUserDiscount`
- Produces: `TaskBillingContext.UserDiscount float64 json:"user_discount,omitempty"`；`HandleGroupRatio` 返回的 `GroupRatio` / `GroupSpecialRatio`（HasSpecialRatio 时）均为折后值

- [ ] **Step 1: 写失败测试**
  - `price_test.go` `TestHandleGroupRatioAppliesUserDiscount`：显式 `ratio_setting.UpdateGroupRatioByJSONString(`{"default":1,"vip":1.5}`)`、`UpdateGroupGroupRatioByJSONString(`{"svip":{"vip":0.9}}`)`，`t.Cleanup` 还原。表：
    | UserGroup | UsingGroup | UserDiscount | GroupRatio | GroupSpecialRatio | HasSpecial |
    |---|---|---|---|---|---|
    | default | default | 1 | 1 | -1 | false |
    | default | vip | 0.8 | 1.2 | -1 | false |
    | svip | vip | 0.5 | 0.45 | 0.45 | true |
    | default | vip | 0 (未设置) | 1.5 | -1 | false |
    用 `assert.InDelta(…, 1e-9)`。
  - `task_billing_test.go` `TestRecalculate_AppliesSnapshottedUserDiscount`：参照 `TestRecalculate_PositiveDelta` 夹具，同 token 数下 BillingContext `UserDiscount: 0.5` 的实际额度 = 不带该字段时的一半（`QuotaFromFloat` 截断后精确值写死）；不带字段时结果与现有 PositiveDelta 期望一致。
- [ ] **Step 2: 运行** `go test ./relay/helper/ ./service/ -run 'UserDiscount' -v` → FAIL
- [ ] **Step 3: 实现**
  - `HandleGroupRatio`：现有逻辑后，`d := relayInfo.UserDiscount`，`if d > 0 && d < 1 { GroupRatio *= d; if HasSpecialRatio { GroupSpecialRatio *= d } }`。
  - `service/quota.go`：`actualGroupRatio` 确定后乘 `EffectiveUserDiscount(relayInfo.UserDiscount)`。
  - `controller/relay.go:628` 写 `UserDiscount: relayInfo.UserDiscount`；`task_billing.go` `finalGroupRatio *= model.EffectiveUserDiscount(bc.UserDiscount)`（bc 为 nil 时不乘）。
- [ ] **Step 4: 运行** `go test ./relay/... ./service/... ./controller/...` → PASS
- [ ] **Step 5: 提交** `feat(billing): apply user discount to group ratio`

### Task 4: 日志与定价展示（后端）

**Files:**
- Modify: `service/log_info_generate.go`（`GenerateTextOtherInfo`、`GenerateMjOtherInfo`）
- Modify: `service/task_billing.go:60`、`service/task_billing.go:157`（`taskBillingOther`）
- Modify: `controller/pricing.go`（`GetPricing`）、`controller/group.go:36`
- Test: `service/text_quota_test.go`

**Interfaces:**
- Consumes: `RelayInfo.UserDiscount`、`TaskBillingContext.UserDiscount`、`UserBase.Discount`
- Produces: 日志 `other.user_discount`（公开，仅当有效折扣 < 1 时写入）

- [ ] **Step 1: 写失败测试** `text_quota_test.go` `TestTextOtherInfoRecordsUserDiscount`：`RelayInfo.UserDiscount=0.8` → `other` 公开字段 `user_discount == 0.8`；`=0` 或 `=1` → 无该键。
- [ ] **Step 2: 运行** `go test ./service/ -run TestTextOtherInfoRecordsUserDiscount -v` → FAIL
- [ ] **Step 3: 实现** 四处日志生成在 `d < 1` 时 `SetPublic("user_discount", d)`（Claude/Wss/Audio 经由 `GenerateTextOtherInfo` 自动覆盖）。`GetPricing` 与 `controller/group.go` 在已登录用户分支内，每个分组倍率乘 `model.EffectiveUserDiscount(user.Discount)`（group.go 用 `model.GetUserCache(c.GetInt("id"))`）。
- [ ] **Step 4: 运行** `go test ./service/ ./controller/` → PASS；`go build ./...` 成功
- [ ] **Step 5: 提交** `feat(billing): expose user discount in logs and pricing`

### Task 5: 前端

**Files:**
- Modify: `web/src/features/users/types.ts`（`userSchema` 加 `discount: z.number().optional()`；`UserFormData.discount?: number`）
- Modify: `web/src/features/users/lib/user-form.ts`（schema、defaults、`transformFormDataToPayload`、user→form 回填）
- Modify: `web/src/features/users/components/users-mutate-drawer.tsx`（编辑模式，group 字段之后）
- Modify: `web/src/features/users/components/users-columns.tsx`（分组列旁 Badge）
- Modify: `web/src/features/usage-logs/components/dialogs/details-dialog.tsx`、`columns/common-logs-columns.tsx`、`common-log-mobile-card.tsx`、`web/src/features/usage-logs/types.ts`
- Modify: `web/src/i18n/locales/*.json`
- Test: `web/src/features/users/components/__tests__/user-discount.test.tsx`

**Interfaces:**
- Consumes: 后端 `discount` 字段、日志 `other.user_discount`
- Produces: 表单字段 `discount: number | undefined`（schema `z.number().min(0).max(1).optional()`）；payload 在编辑模式恒带 `discount`（undefined/空 → 0）

- [ ] **Step 1: 写失败测试** `user-discount.test.tsx`：
  - `transformFormDataToPayload({...defaults, discount: 0.8}, 1).discount === 0.8`
  - `discount: undefined` + userId → payload `discount === 0`
  - 创建模式（无 userId）payload 不含 `discount`
  - `userFormSchema.safeParse({...defaults, discount: 1.2}).success === false`
- [ ] **Step 2: 运行** `cd web && bun run test -- user-discount` → FAIL
- [ ] **Step 3: 实现** 表单逻辑；抽屉用与 `quota_dollars` 相同的现有数字输入组件，label `t('User discount')`，description `t('Multiplier applied on top of the group ratio, e.g. 0.8 means 20% off. Leave empty for no discount.')`，step 0.01；列表 Badge 仅 `0 < discount < 1` 时显示 `${discount}x`，tooltip `t('User discount')`；日志三处在 `user_discount` 存在时追加一行 `t('User discount')` / `${formatRatio(v)}x`，不参与金额计算。
- [ ] **Step 4: 运行** `cd web && bun run i18n:sync && bun run typecheck && bun run lint && bun run test && bun run build` → 全部成功；补齐 7 种语言译文
- [ ] **Step 5: 提交** `feat(web): edit and display user discount`

### Task 6: 三库验证

**Files:** 无代码改动（如发现问题回到对应 Task 修复）

- [ ] **Step 1: 启动实例** `docker run -d --name nd-mysql -e MYSQL_ROOT_PASSWORD=pw -e MYSQL_DATABASE=newapi -p 13306:3306 mysql:5.7` 与 `docker run -d --name nd-pg -e POSTGRES_PASSWORD=pw -e POSTGRES_DB=newapi -p 15432:5432 postgres:9.6`；SQLite 用临时文件
- [ ] **Step 2: 升级场景** 对每个库：`git worktree add /tmp/na-rc42 v1.0.0-rc.42`，用该版本 `SQL_DSN=<dsn> go run .` 启动建库并创建 2 个用户后停止；切回本分支同 DSN 启动两次。期望：启动日志无错误，第二次无 `ALTER TABLE`；`SELECT id, username, discount FROM users` 原用户 discount=0；索引/唯一约束与升级前 `SHOW INDEX` / `\d users` / `.schema users` 一致
- [ ] **Step 3: 全新库场景** 空库用本分支启动两次，确认列类型（`double` / `double precision` / `real`）与默认值 0
- [ ] **Step 4: 往返** 管理员接口设置 0.8 → 查询为 0.8；设置 1 → 0；用该用户调用一个按量模型，消费日志额度为无折扣时的 0.8 倍、含 `user_discount`
- [ ] **Step 5: 清理** 删除容器、worktree、临时 SQLite 文件；把版本号、命令、结果记录到 PR 描述
