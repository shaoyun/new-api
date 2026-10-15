# 客户「分组 + 模型」折扣实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 用按「客户 + 分组 + 精确模型名」配置的折扣规则替换 PR #6 的用户统一折扣，日志拆开显示真实分组倍率与折扣。

**Architecture:** 新表 `user_discount_rules`，按用户整份缓存到 Redis；`HandleGroupRatio` 用最终分组 + 计费模型名查规则，`GroupRatio` 仍为计费用乘积，`GroupRatioInfo` 另存真实分组倍率与折扣供日志/快照使用。管理端在用户行操作打开侧边面板维护规则。

**Tech Stack:** Go 1.26、Gin、GORM v2（SQLite/MySQL/PostgreSQL）、Redis；React 19、TanStack Query、Base UI、i18next、Bun。

**Spec:** `docs/superpowers/specs/2026-10-15-group-model-discount-design.md`

**分支:** `feat/group-model-discount`（基于 `origin/main`，已含 PR #6）

## Global Constraints

- 开工前 Read `AGENTS.md` 与 `.agents/rules/billing.md`；前端任务另 Read `web/AGENTS.md` 与 shadcn-ui skill。
- 有效折扣 ∈ (0, 1)；NaN、≤0、≥1 一律视为 1。接口只接受 0 < discount < 1 的有限数。
- 规则只按精确模型名匹配；未命中 = 1。`auto` 不能作为规则分组。
- 不通过 `OtherRatios` 实现；不新增额度转换点，继续用 `common/quota_math.go`。
- JSON 编解码只用 `common.Marshal/Unmarshal/UnmarshalJsonStr/DecodeJson`。
- 原生 SQL 中 `group` 列用 `commonGroupCol`；优先 GORM 方法。
- 后端测试用 testify `require`/`assert`，表驱动；本功能至多新增一个后端测试文件：`model/user_discount_rule_test.go`。
- 新前端文案走 `t('English key')`，`bun run i18n:sync` 后补齐 7 种语言。
- `users.discount` 列保留（回滚用），代码不再读取它参与计费。
- 提交信息末尾加 `Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>`；只 `git add` 具体文件，不要提交根目录的 `*.csv`。

## Review Focus

1. 规则查询出错（DB/Redis 失败）：请求照常处理、按无折扣计费、记录错误 —— Task 2 测试覆盖 DB 表缺失时返回 1。
2. 无规则的客户每次请求都打一次 DB：空结果也要缓存；写规则后缓存必须失效 —— Task 1 用 miniredis 测试覆盖。
3. auto 分组跨组重试：第二次 `HandleGroupRatio` 必须用新分组的规则 —— Task 2 表驱动用例。
4. PR #6 期间的旧日志（`group_ratio` 已折后 + `user_discount`）在前端不能被重复相乘 —— Task 7 前端测试。
5. 迁移重复执行/部分失败：不重复插入、不覆盖运营已改的规则、成功后 `users.discount` 清零 —— Task 3 测试。

---
### Task 1: 规则表、CRUD 与缓存

**Files:**
- Create: `model/user_discount_rule.go`
- Create: `model/user_discount_rule_test.go`
- Modify: `model/main.go`（`migrateDB` 的 `AutoMigrate` 列表加 `&UserDiscountRule{}`）

**Interfaces:**
- Produces:
  - `type UserDiscountRule struct`（字段与 GORM 标签照抄 spec「数据模型」，表名 `user_discount_rules`）
  - `const MaxUserDiscountRuleModels = 500`
  - `func EffectiveUserDiscount(discount float64) float64`（已存在于 `model/user.go`，保留不动）
  - `func GetUserDiscountRuleMap(userId int) (map[string]map[string]float64, error)` — group → model → discount
  - `func GetUserGroupModelDiscount(userId int, group, modelName string) float64` — 未命中/出错返回 1；出错 `common.SysError`
  - `func ListUserDiscountRules(userId int) ([]UserDiscountRule, error)` — 按 group、model 升序
  - `func UpsertUserDiscountRules(userId int, group string, models []string, discount float64) error`
  - `func DeleteUserDiscountRules(userId int, ids []int) (int64, error)` — 只删 `user_id` 匹配的
  - `func CountUserDiscountRules(userIds []int) (map[int]int64, error)` — 一次 `GROUP BY user_id`
  - `func InvalidateUserDiscountRuleCache(userId int)`

- [ ] **Step 1: 写失败测试** `model/user_discount_rule_test.go`，复用 `setupUserUpdateTestState`（必要时在 `truncateTables` 清理列表加 `DELETE FROM user_discount_rules`）：

```go
func TestUserDiscountRuleUpsertLookupDelete(t *testing.T) {
	setupUserUpdateTestState(t)
	require.NoError(t, UpsertUserDiscountRules(7, "vip", []string{"m-a", "m-b"}, 0.8))
	require.NoError(t, UpsertUserDiscountRules(7, "vip", []string{"m-b"}, 0.9)) // 覆盖
	require.NoError(t, UpsertUserDiscountRules(8, "vip", []string{"m-a"}, 0.5))

	assert.Equal(t, 0.8, GetUserGroupModelDiscount(7, "vip", "m-a"))
	assert.Equal(t, 0.9, GetUserGroupModelDiscount(7, "vip", "m-b"))
	assert.Equal(t, 1.0, GetUserGroupModelDiscount(7, "default", "m-a")) // 分组不同
	assert.Equal(t, 1.0, GetUserGroupModelDiscount(7, "vip", "m-c"))     // 模型不同
	assert.Equal(t, 1.0, GetUserGroupModelDiscount(9, "vip", "m-a"))     // 无规则用户

	rules, err := ListUserDiscountRules(7)
	require.NoError(t, err)
	require.Len(t, rules, 2)

	counts, err := CountUserDiscountRules([]int{7, 8, 9})
	require.NoError(t, err)
	assert.Equal(t, map[int]int64{7: 2, 8: 1}, counts)

	other, _ := ListUserDiscountRules(8)
	n, err := DeleteUserDiscountRules(7, []int{rules[0].Id, other[0].Id}) // 跨用户 id 被忽略
	require.NoError(t, err)
	assert.Equal(t, int64(1), n)
	assert.Equal(t, 0.5, GetUserGroupModelDiscount(8, "vip", "m-a"))
}

func TestGetUserGroupModelDiscountFallsBackOnError(t *testing.T) {
	setupUserUpdateTestState(t)
	require.NoError(t, DB.Migrator().DropTable(&UserDiscountRule{}))
	t.Cleanup(func() { require.NoError(t, DB.AutoMigrate(&UserDiscountRule{})) })
	assert.Equal(t, 1.0, GetUserGroupModelDiscount(7, "vip", "m-a"))
}
```

  另加一条表驱动：存入非法值（直接 `DB.Create` 写 1.5、0）时 `GetUserGroupModelDiscount` 返回 1。

  再加 `TestUserDiscountRuleCache`（`miniredis.RunT(t)`，`common.RDB` 指向它，`common.RedisEnabled = true`，`t.Cleanup` 还原；写法参照 `controller/user_manage_test.go` 中的 miniredis 用法）：
  - 无规则用户查询一次后，`mr.Exists("user_discount_rules:9")` 为 true，值为 `{}`。
  - 用户 7 查询一次（命中缓存）→ `UpsertUserDiscountRules(7, "vip", ["m-a"], 0.6)` → 再查得 0.6（缓存已失效）。
  - `DeleteUserDiscountRules` 后同样失效。

- [ ] **Step 2: 运行确认失败** — `go test ./model -run 'TestUserDiscountRule|TestGetUserGroupModelDiscount' -count=1`，预期编译失败（未定义）。

- [ ] **Step 3: 实现 `model/user_discount_rule.go`**
  - Upsert：在事务内对每个模型 `Clauses(clause.OnConflict{Columns: user_id/group/model, DoUpdates: discount, updated_at})` 批量 `Create`；时间戳用 `common.GetTimestamp()`；提交后 `InvalidateUserDiscountRuleCache`。参数校验放在 controller，model 层只防御 `EffectiveUserDiscount(discount) == 1` 时返回错误。
  - 缓存：`common.RedisEnabled` 时 key `user_discount_rules:<id>`，值为 map 的 JSON（空 map 也缓存为 `{}`），TTL `common.RedisKeyCacheSeconds()` 秒；读缓存失败回源 DB；未开 Redis 直接查 DB。
  - `GetUserGroupModelDiscount` 对查到的值再过一遍 `EffectiveUserDiscount`。

- [ ] **Step 4: 运行确认通过** — 同 Step 2 命令，预期 PASS；再跑 `go test ./model -count=1` 确认无回归。

- [ ] **Step 5: 提交** — `git add model/user_discount_rule.go model/user_discount_rule_test.go model/main.go model/task_cas_test.go`，`feat(model): add per-user group-model discount rules`

### Task 2: 计费核心改为按规则查询，移除统一折扣的计费链路

**Files:**
- Modify: `types/price_data.go:10-14`（`GroupRatioInfo`）
- Modify: `relay/helper/price.go:43-77`（`HandleGroupRatio`）
- Modify: `service/quota.go:101-121`（`PreWssConsumeQuota`）
- Test: `relay/helper/price_test.go`（改写 `TestHandleGroupRatioAppliesUserDiscount`）

本任务不删除 `RelayInfo.UserDiscount` 等 PR #6 标量（日志与任务仍在读），由 Task 3 一并移除。

**Interfaces:**
- Consumes: Task 1 `model.GetUserGroupModelDiscount`
- Produces:

```go
type GroupRatioInfo struct {
	GroupRatio         float64 // 计费用：BaseGroupRatio × GroupModelDiscount
	GroupSpecialRatio  float64 // 真实特殊倍率（不含折扣），-1 表示无
	HasSpecialRatio    bool
	BaseGroupRatio     float64 // 真实分组倍率（特殊倍率或分组倍率）
	GroupModelDiscount float64 // 有效折扣；0 或 1 均表示无折扣，读取时一律过 EffectiveUserDiscount
}
```

- [ ] **Step 1: 改写测试** `TestHandleGroupRatioAppliesUserDiscount` → `TestHandleGroupRatioAppliesGroupModelDiscount`。测试内用 `gorm.Open(sqlite.Open(":memory:"))` 建库、`AutoMigrate(&model.UserDiscountRule{})`，临时替换 `model.DB` 并 `t.Cleanup` 还原，`common.RedisEnabled = false`。种子：用户 7 在 `vip/m-a` 0.8、`svip-target/m-a` 0.5、`fallback/m-a` 0.6。

| 用例 | UserGroup | UsingGroup | ctx auto_group | OriginModelName / BillingModelName | 期望 GroupRatio | BaseGroupRatio | GroupSpecialRatio | Discount |
|---|---|---|---|---|---|---|---|---|
| 未命中 | default | vip | - | m-x / - | 1.5 | 1.5 | -1 | 1 |
| 命中分组倍率 | default | vip | - | m-a / - | 1.2 | 1.5 | -1 | 0.8 |
| 命中 + 特殊倍率 | svip | svip-target | - | m-a / - | 0.45 | 0.9 | 0.9 | 0.5 |
| 按计费模型名匹配 | default | vip | - | m-a-thinking / m-a | 1.2 | 1.5 | -1 | 0.8 |
| auto 跨组后用新分组规则 | default | vip | fallback | m-a / - | 0.6 | 1 | -1 | 0.6 |

  分组倍率配置：`{"default":1,"vip":1.5,"svip-target":1,"fallback":1}`，特殊倍率 `{"svip":{"svip-target":0.9}}`。auto 用例额外断言 `info.UsingGroup == "fallback"`。

- [ ] **Step 2: 运行确认失败** — `go test ./relay/helper -run TestHandleGroupRatioAppliesGroupModelDiscount -count=1`，预期编译失败（字段未定义）。

- [ ] **Step 3: 实现**
  - `HandleGroupRatio`：保持现有分组/特殊倍率/auto 逻辑；记 `BaseGroupRatio = GroupRatio`；`d := model.GetUserGroupModelDiscount(relayInfo.UserId, relayInfo.UsingGroup, relayInfo.GetBillingModelName())`；`GroupModelDiscount = d`；`d < 1` 时 `GroupRatio *= d`。`GroupSpecialRatio` 不再乘折扣。
  - `PreWssConsumeQuota`：删 `EffectiveUserDiscount(relayInfo.UserDiscount)` 那行，改为 `actualGroupRatio *= model.GetUserGroupModelDiscount(relayInfo.UserId, relayInfo.UsingGroup, modelName)`（`modelName` 即该函数已有的 `relayInfo.OriginModelName`，与其计价一致）。
- [ ] **Step 4: 运行确认通过** — `go build ./... && go test ./relay/... ./model/... -count=1`，预期 PASS。

- [ ] **Step 5: 提交** — `feat(billing): resolve discount per group and model in HandleGroupRatio`

### Task 3: 日志拆开显示、任务快照，移除统一折扣标量

**Files:**
- Modify: `service/log_info_generate.go`（`appendUserDiscount` → `appendGroupModelDiscount`；调用点 `GenerateTextOtherInfo`、`GenerateMjOtherInfo`；`GenerateWssOtherInfo`/`GenerateAudioOtherInfo`/`GenerateClaudeOtherInfo` 经 `GenerateTextOtherInfo` 自动覆盖）
- Modify: `service/task_billing.go`（`LogTaskConsumption`、`taskBillingOther`、`RecalculateTaskQuotaByTokens`）
- Modify: `model/task.go`（`TaskBillingContext`）
- Modify: `controller/relay.go:625-641`（提交快照）
- Modify: 移除 PR #6 标量：`relay/common/relay_info.go`（`UserDiscount` 及 `genBaseRelayInfo` 读取）、`constant/context_key.go`（`ContextKeyUserDiscount`）、`model/user_cache.go`（`UserBase.Discount`、`WriteContext` 写折扣；`userCacheSchemaVersion` 3 → 4）、`model/user.go`（`ToBaseUser`）、`model/user_auth_cache.go`（Lua `'Discount', ARGV[13]` 及参数；保留 PR #6 加的 CacheSchema 比对守卫）
- Test: `service/text_quota_test.go`（替换 `TestTextOtherInfoRecordsUserDiscount`）、`service/task_billing_test.go`（扩展 `TestRecalculate_AppliesSnapshottedUserDiscount` 与 `taskBillingOther` 用例）；删除 `TestGenRelayInfoUserDiscount`、`TestUserCacheCarriesDiscount`

**Interfaces:**
- Consumes: Task 2 `GroupRatioInfo.BaseGroupRatio` / `GroupModelDiscount`
- Produces:
  - `func appendGroupModelDiscount(other *model.LogOther, info hosttypes.GroupRatioInfo)` — 折扣 < 1 时覆盖 `group_ratio` 为 `BaseGroupRatio`，写 `group_model_discount`；否则不动
  - `TaskBillingContext` 新增 `BaseGroupRatio float64 json:"base_group_ratio,omitempty"`、`GroupModelDiscount float64 json:"group_model_discount,omitempty"`；保留 `UserDiscount`（旧任务兼容，新任务不再写）

- [ ] **Step 1: 写失败测试**
  - `TestTextOtherInfoRecordsGroupModelDiscount`（替换旧用例），表驱动，`relayInfo.PriceData.GroupRatioInfo` 设为：

| 用例 | GroupRatio | BaseGroupRatio | GroupModelDiscount | 期望 group_ratio | 期望 group_model_discount |
|---|---|---|---|---|---|
| 命中 | 0.8 | 1 | 0.8 | 1 | 0.8 |
| 未命中 | 1.5 | 1.5 | 1 | 1.5 | 不存在 |
| 零值结构 | 1 | 0 | 0 | 1 | 不存在 |

    调用 `GenerateTextOtherInfo(ctx, relayInfo, 1, info.GroupRatio, 1, 0, 0, 0, -1)`，并断言 `user_discount` 不存在。
  - `taskBillingOther`：`bc{GroupRatio:0.8, BaseGroupRatio:1, GroupModelDiscount:0.8}` → `group_ratio`=1、`group_model_discount`=0.8；旧任务 `bc{GroupRatio:0.8, UserDiscount:0.8}` → `group_ratio`=0.8、`user_discount`=0.8（保持旧展示）。
  - `TestRecalculate_AppliesSnapshottedUserDiscount` 增加一列快照字段：`GroupModelDiscount: 0.5` → 1500；`UserDiscount: 0.5`（旧任务）→ 1500；两者都为 0 → 3001。

- [ ] **Step 2: 运行确认失败** — `go test ./service -run 'TestTextOtherInfoRecordsGroupModelDiscount|TestRecalculate_AppliesSnapshotted|TestTaskBillingOther' -count=1`

- [ ] **Step 3: 实现**
  - `GenerateTextOtherInfo` / `GenerateMjOtherInfo` / `LogTaskConsumption`：在写完 `group_ratio` 后调用 `appendGroupModelDiscount(other, <对应的 GroupRatioInfo>)`。
  - `controller/relay.go` 快照：`GroupModelDiscount < 1` 时写 `BaseGroupRatio` 与 `GroupModelDiscount`，否则都留 0；不再写 `UserDiscount`。
  - `taskBillingOther`：`EffectiveUserDiscount(bc.GroupModelDiscount) < 1` 时 `group_ratio` 用 `bc.BaseGroupRatio` 并写 `group_model_discount`；否则沿用 `bc.GroupRatio`，旧任务 `UserDiscount` 仍写 `user_discount`。
  - `RecalculateTaskQuotaByTokens`：`finalGroupRatio *= EffectiveUserDiscount(bc.GroupModelDiscount) * EffectiveUserDiscount(bc.UserDiscount)`（同一任务最多一个非 1）。
  - 移除 PR #6 标量（见 Files），`go build ./...` 无残留引用。

- [ ] **Step 4: 运行确认通过** — `go build ./... && go test ./service/... ./relay/... ./model/... ./controller/... -count=1`

- [ ] **Step 5: 提交** — `feat(billing): log base group ratio and group-model discount separately`

### Task 4: 统一折扣迁移为规则

**Files:**
- Modify: `model/user_discount_rule.go`、`model/user_discount_rule_test.go`
- Modify: `main.go`（`model.InitOptionMap()` 之后、仅 `common.IsMasterNode` 时调用）

**Interfaces:**
- Consumes: Task 1 表；`service.GetUserUsableGroups(userGroup string) map[string]string`；`model.GetGroupEnabledModels(group string) []string`
- Produces: `func MigrateLegacyUserDiscounts(usableGroups func(userGroup string) []string) error` — model 不能 import service，由 `main.go` 传入：`func(g string) []string { 取 maps.Keys(service.GetUserUsableGroups(g))，去掉 "auto" }`

- [ ] **Step 1: 写失败测试** `TestMigrateLegacyUserDiscounts`：
  - 种子 abilities：`vip` 下 `m-a`、`m-b`（enabled），`vip` 下 `m-off`（disabled），`default` 下 `m-a`。
  - 用户 A：group `default`、discount 0.8；预置规则 `vip/m-a` 0.9。用户 B：discount 0；用户 C：discount 1.5（脏数据）。
  - 回调返回 `[]string{"default","vip"}`。
  - 断言：A 得到 `default/m-a` 0.8、`vip/m-b` 0.8，`vip/m-a` 保持 0.9（不覆盖），无 `m-off`；A 的 `discount` 变 0；B、C 无规则且 C 的 `discount` 不变。
  - 再调用一次：规则条数不变，无错误。

- [ ] **Step 2: 运行确认失败** — `go test ./model -run TestMigrateLegacyUserDiscounts -count=1`

- [ ] **Step 3: 实现** — 查 `discount > 0 AND discount < 1` 的用户；逐个用户开事务：展开后 `Clauses(clause.OnConflict{DoNothing: true})` 批量插入，再 `Update("discount", 0)`；提交后失效缓存。单个用户失败 `common.SysError` 并继续，函数返回汇总错误（main 里只记日志，不阻断启动）。

- [ ] **Step 4: 运行确认通过** — 同 Step 2；`go build ./...`

- [ ] **Step 5: 提交** — `feat(model): migrate legacy user discount into group-model rules`

### Task 5: 管理接口，清理旧折扣入口

**Files:**
- Create: `controller/user_discount_rule.go`
- Modify: `router/api-router.go`（`adminRoute` 下加 4 条路由）
- Modify: `controller/user.go`（`UpdateUser` 删 `discountRequest` 解析与 `discount_old/new` 审计；`GetAllUsers`/`SearchUsers` 填 `DiscountRuleCount`）
- Modify: `model/user.go`（删 `DiscountUpdate`、`NormalizeUserDiscount`、`EditWithTx` 中折扣分支；`Discount` 的 JSON 标签改 `json:"-"`；`GetSelfUserById` select 去掉 `"discount"`；新增 `DiscountRuleCount int64 json:"discount_rule_count" gorm:"-:all"`）
- Modify: `i18n/keys.go`、`i18n/locales/{en,zh-CN,zh-TW}.yaml`
- Test: `controller/user_manage_test.go`（替换 `TestUpdateUserDiscount`）、`model/user_update_test.go`（删 `TestEffectiveAndNormalizeUserDiscount` 中 Normalize 部分、`TestUserEditPreservesDiscountUnlessUpdated`、`TestUserEditRejectsInvalidDiscountUpdate`，保留 `EffectiveUserDiscount` 边界）

**Interfaces:**
- Consumes: Task 1 全部 CRUD；`service.GetUserUsableGroups`；`model.GetGroupEnabledModels`；`canManageTargetRole`；`recordManageAuditFor`
- Produces（前端 Task 7 依赖）：

| 方法 | 路径 | 请求 | 响应 `data` |
|---|---|---|---|
| GET | `/api/user/:id/discount_rules` | - | `UserDiscountRule[]` |
| GET | `/api/user/:id/discount_options` | - | `{groups: [{group, desc, ratio, models: string[]}]}`，排除 `auto` |
| PUT | `/api/user/:id/discount_rules` | `{group, models, discount}` | `{updated: number}` |
| DELETE | `/api/user/:id/discount_rules` | `{ids: number[]}` | `{deleted: number}` |

  新 i18n key：`MsgUserDiscountGroupInvalid = "user.discount_group_invalid"`、`MsgUserDiscountModelsInvalid = "user.discount_models_invalid"`；范围错误复用 `MsgUserDiscountInvalid`（文案改为「折扣必须大于 0 且小于 1」）。审计 action：`user.discount_rules.upsert`（group、model_count、discount）、`user.discount_rules.delete`（deleted）。

- [ ] **Step 1: 写失败测试** `TestUserDiscountRuleAPI`（扩展 `user_manage_test.go`，复用 `setupManageUserTestDB`，迁移列表加 `UserDiscountRule`、`Ability`）。表驱动断言状态与结果：
  - PUT discount 0、1、1.5、NaN（字符串 `"NaN"` 解码失败也算）→ 失败消息为 `MsgUserDiscountInvalid`。
  - PUT group `auto` 或用户不可用的分组 → `MsgUserDiscountGroupInvalid`。
  - PUT models 空、全为空白、501 个 → `MsgUserDiscountModelsInvalid`；`[" m-a ", "m-a"]` 存为一条 `m-a`。
  - PUT 成功后 GET 返回规则；再 PUT 同模型新折扣为覆盖。
  - DELETE 带另一用户的规则 id → `deleted` 只计本用户的。
  - 操作者角色低于目标用户 → `MsgUserNoPermissionHigherLevel`。
  - `PUT /api/user/` 带 `discount: 0.5` → 成功且 `users.discount` 不变。
  - `GET /api/user/` 列表项 `discount_rule_count` 正确。

- [ ] **Step 2: 运行确认失败** — `go test ./controller -run 'TestUserDiscountRuleAPI' -count=1`

- [ ] **Step 3: 实现** — 解析 `:id`，查目标用户并校验 `canManageTargetRole`；JSON 用 `common.DecodeJson`；校验顺序：discount（`math.IsNaN/IsInf`、`>0 && <1`）→ group（在 `GetUserUsableGroups(user.Group)` 中且 ≠ `auto`）→ models（`strings.TrimSpace`、去空、去重、≤ `model.MaxUserDiscountRuleModels`、每个 ≤255 字节）。

- [ ] **Step 4: 运行确认通过** — `go build ./... && go test ./controller/... ./model/... -count=1`

- [ ] **Step 5: 提交** — `feat(api): admin endpoints for user discount rules`

### Task 6: 定价接口与分组列表恢复真实倍率

**Files:**
- Modify: `controller/pricing.go`（删 `roundDiscountedDisplayRatio` 及折扣相乘；登录用户返回 `user_discounts`）
- Modify: `controller/group.go`（`GetUserGroups` 删折扣相乘，`ratio` 恢复 `service.GetUserGroupRatio`）
- Test: `controller/user_manage_test.go`（替换 `TestGetUserGroupsRoundsDiscountedRatio`）

**Interfaces:**
- Consumes: Task 1 `model.GetUserDiscountRuleMap`
- Produces: `GET /api/pricing` 响应顶层新增 `user_discounts: Record<group, Record<model, number>>`（未登录或无规则时为 `{}`，只含 `usable_group` 中的分组）

- [ ] **Step 1: 写失败测试** `TestPricingAndGroupsUseBaseRatioWithUserDiscounts`：分组倍率 `{"default":1,"vip":1.5}`，用户规则 `vip/m-a` 0.8、以及一条用户不可用分组 `hidden/m-a` 0.5。断言 `/api/user/self/groups` 的 `vip.ratio == 1.5`；`/api/pricing` 的 `group_ratio.vip == 1.5`、`user_discounts == {"vip":{"m-a":0.8}}`。
- [ ] **Step 2: 运行确认失败** — `go test ./controller -run TestPricingAndGroupsUseBaseRatioWithUserDiscounts -count=1`
- [ ] **Step 3: 实现** — 按上述 Interfaces；查规则出错时返回 `{}` 并 `common.SysError`，不影响定价接口。
- [ ] **Step 4: 运行确认通过** — `go build ./... && go test ./controller/... -count=1`
- [ ] **Step 5: 提交** — `feat(pricing): expose per-user group-model discounts`

### Task 7: 前端日志展示计算过程

**Files:**
- Modify: `web/src/features/usage-logs/types.ts`（加 `group_model_discount?: number`）
- Modify: `web/src/features/usage-logs/components/dialogs/details-dialog.tsx:229-244`
- Modify: `web/src/features/usage-logs/components/columns/common-logs-columns.tsx:~329`
- Modify: `web/src/features/usage-logs/components/common-log-mobile-card.tsx:~80`
- Test: `web/src/features/usage-logs/components/__tests__/cost-display.test.tsx`（扩展）

**Interfaces:**
- Consumes: Task 3 日志字段 `group_ratio`（真实值）、`group_model_discount`

- [ ] **Step 1: 写失败测试**（扩展 `cost-display.test.tsx`）：
  - 新日志 `{group_ratio:1, group_model_discount:0.8}`：详情含「Group Ratio 1x」和「Discount 0.8x」，列表摘要含 `Discount 0.8x`。
  - 新日志带特殊倍率 `{group_ratio:0.9, user_group_ratio:0.9, group_model_discount:0.5}`：显示「User Exclusive Ratio 0.9x」和「Discount 0.5x」。
  - 旧日志 `{group_ratio:0.8, user_discount:0.8}`：保持现有「Group Ratio 0.8x」「User discount 0.8x」展示（回归保护）。
  - 无折扣日志：不出现 Discount 行。
- [ ] **Step 2: 运行确认失败** — `cd web && bun run test src/features/usage-logs/components/__tests__/cost-display.test.tsx`
- [ ] **Step 3: 实现** — `group_model_discount` 存在且有限时，在分组倍率行后加 `t('Discount')` 行；按次/按量/阶梯区块中「分组倍率」乘数旁同样展示折扣项。新文案 `Discount`，`bun run i18n:sync` 后补 7 种语言（zh：「折扣」）。
- [ ] **Step 4: 运行确认通过** — 同 Step 2；`bun run typecheck && bun run lint`
- [ ] **Step 5: 提交** — `feat(web): show group ratio and discount separately in logs`

### Task 8: 前端折扣规则侧边面板，移除统一折扣输入

**Files:**
- Create: `web/src/features/users/components/dialogs/user-discount-rules-sheet.tsx`
- Modify: `web/src/features/users/api.ts`（4 个接口函数）
- Modify: `web/src/features/users/types.ts`（`UserDiscountRule`、`UserDiscountOptions`；`User` 删 `discount`、加 `discount_rule_count?: number`）
- Modify: `web/src/features/users/components/data-table-row-actions.tsx`（新增「Discount rules」行操作与本地 open 状态，仿 `subscriptionsDialogOpen`）
- Modify: `web/src/features/users/components/users-mutate-drawer.tsx`、`web/src/features/users/lib/user-form.ts`（删折扣字段与提交）
- Modify: `web/src/features/users/components/users-columns.tsx`（`0.8x` 徽标改为 `t('{{count}} discount rules')`，0 不显示）
- Test: 改写 `web/src/features/users/components/__tests__/user-discount.test.tsx`

**Interfaces:**
- Consumes: Task 5 接口与响应形状
- Produces: `getUserDiscountRules(userId)`、`getUserDiscountOptions(userId)`、`upsertUserDiscountRules(userId, {group, models, discount})`、`deleteUserDiscountRules(userId, ids)`

- [ ] **Step 1: 写失败测试**（mock api）：
  - 选分组 `vip` 后模型多选只列 `vip` 的模型；全选后填 0.8 提交，`upsertUserDiscountRules` 收到 `{group:'vip', models:[...全部], discount:0.8}`。
  - 选中已有规则的模型时出现「Will overwrite 1 existing rule」提示。
  - 折扣输入 0、1、空时提交按钮禁用或显示校验错误。
  - 勾选两条规则批量删除，确认后 `deleteUserDiscountRules` 收到两个 id。
  - 用户编辑抽屉不再渲染折扣输入；提交 payload 不含 `discount`。
  - 列表 `discount_rule_count: 3` 显示徽标，0 不显示。
- [ ] **Step 2: 运行确认失败** — `cd web && bun run test src/features/users/components/__tests__/user-discount.test.tsx`
- [ ] **Step 3: 实现** — 结构参照 `features/subscriptions/components/dialogs/user-subscriptions-dialog.tsx`（`Sheet` + `sideDrawerContentClassName('sm:max-w-2xl')`）；分组用 `components/ui/combobox.tsx`，模型用 `components/multi-select.tsx`（`showBulkActions`），删除确认用 `components/confirm-dialog.tsx`；查询/变更用 TanStack Query，成功后 invalidate 规则与用户列表查询。规则表格支持按分组、模型文本筛选，单条编辑复用添加区（回填分组/模型/折扣）。新文案全部 `t()`，`bun run i18n:sync` 补 7 种语言。
- [ ] **Step 4: 运行确认通过** — 同 Step 2；`bun run typecheck && bun run lint`
- [ ] **Step 5: 提交** — `feat(web): manage per-user group-model discount rules`

### Task 9: 前端定价页显示折后价

**Files:**
- Modify: `web/src/features/pricing/types.ts`（`PricingData.user_discounts?: Record<string, Record<string, number>>`；`PricingModel.user_discounts?: Record<string, number>` 即该模型 group → discount）
- Modify: `web/src/features/pricing/hooks/use-pricing-data.ts`（构造 model 时：`group_ratio` 为该模型的「真实倍率 × 该分组该模型折扣」，并挂 `user_discounts`）
- Modify: `web/src/features/pricing/components/model-details.tsx`（分组价格表从 `model.group_ratio` 取倍率，而不是全局 `props.groupRatio`；有折扣的分组显示折扣徽标）
- Modify: `web/src/features/pricing/components/model-card.tsx`（存在任一折扣时显示折扣徽标）
- Test: `web/src/features/pricing/__tests__/model-cards.test.tsx`（扩展）

**Interfaces:**
- Consumes: Task 6 `user_discounts`

- [ ] **Step 1: 写失败测试**：`group_ratio {vip:1.5}`、`user_discounts {vip:{"m-a":0.8}}`；`m-a` 在 vip 的展示价格 = 基础价 × 1.2，带折扣徽标；`m-b` 在 vip = 基础价 × 1.5，无徽标；无 `user_discounts` 字段时与现在一致。
- [ ] **Step 2: 运行确认失败** — `cd web && bun run test src/features/pricing/__tests__/model-cards.test.tsx`
- [ ] **Step 3: 实现** — 按 Files；折后倍率仅用于展示，四舍五入到 6 位小数去浮点噪声。
- [ ] **Step 4: 运行确认通过** — `bun run test && bun run typecheck && bun run lint && bun run build`
- [ ] **Step 5: 提交** — `feat(web): show discounted prices per group and model`

### Task 10: 三数据库验证与收尾

**Files:** 无新增代码；必要时修复验证中发现的问题。

- [ ] **Step 1: 起数据库** — `docker run -d --name gmd-mysql -e MYSQL_ROOT_PASSWORD=pw -e MYSQL_DATABASE=newapi -p 13306:3306 mysql:5.7`；`docker run -d --name gmd-pg -e POSTGRES_PASSWORD=pw -e POSTGRES_DB=newapi -p 15432:5432 postgres:9.6`。记录 `SELECT VERSION()` / `SHOW server_version` 结果。
- [ ] **Step 2: 接口测试跑三库** — `TEST_MANAGE_USER_DIALECT={sqlite,mysql,postgres}`，配 `TEST_MYSQL_DSN='root:pw@tcp(127.0.0.1:13306)/newapi?parseTime=true'`、`TEST_POSTGRES_DSN='postgres://postgres:pw@127.0.0.1:15432/newapi?sslmode=disable'`，跑 `go test ./controller -run 'TestUserDiscountRuleAPI|TestPricingAndGroupsUseBaseRatioWithUserDiscounts' -count=1`，三者全部 PASS。
- [ ] **Step 3: 全新库启动** — 每种库用 `SQL_DSN` 启动 `go run .` 两次；确认 `user_discount_rules` 表与 `idx_user_group_model` 联合唯一索引存在，第二次启动日志无针对该表或 `users` 的 `ALTER TABLE`。
- [ ] **Step 4: 升级路径** — 每种库先用生产当前镜像对应提交（`git worktree add /tmp/gmd-prod 154180d94`）启动建库，写入一个 `discount=0.8` 的用户、若干 abilities；切到本分支启动两次。确认：规则按「可用分组 × 已启用模型」展开、`users.discount` 清零、已有用户/令牌/索引不变、第二次启动无新增规则。
- [ ] **Step 5: 读写往返** — 每种库通过 API 写入、覆盖、删除规则各一次，直接查表确认结果。
- [ ] **Step 6: 全量检查** — `go build ./... && go test ./... -count=1`；`cd web && bun run test && bun run typecheck && bun run lint && bun run build`。
- [ ] **Step 7: 清理** — 删除容器与 `/tmp/gmd-prod` worktree；把精确的数据库版本、命令与结果整理进 PR 描述。任何一项跑不了，明确写出阻塞，不宣称完成。
