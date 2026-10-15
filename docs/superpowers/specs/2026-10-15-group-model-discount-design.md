# 客户「分组 + 模型」折扣设计

## 背景与目标

PR #6 的用户统一折扣（`users.discount`）对一个客户的所有分组、所有模型乘同一系数。不同分组、不同模型的成本不同，统一折扣容易让低毛利模型亏损。运营要求折扣细化到「客户 + 分组 + 模型」，并愿意维护细粒度配置。

已确认决策：

| 项 | 决定 |
|---|---|
| 规则归属 | 按客户配置：每个客户在每个「分组 + 模型」上有各自的折扣 |
| 模型匹配 | 只按精确模型名，不支持通配符 |
| 未配置 | 按原价：扣费 = 模型价格 × 分组倍率 |
| 与分组倍率关系 | 叠加相乘：扣费 = 模型价格 × 分组倍率 × 折扣。分组倍率默认 1，个别分组可特殊调整 |
| 取值范围 | (0, 1)，只能打折，不支持加价和免费 |
| 旧统一折扣 | 废弃并迁移，上线后只保留一套折扣机制 |
| 日志 | 拆开显示真实分组倍率与实际折扣，客户能按 `价格 × 用量 × 分组倍率 × 折扣` 自行核对 |
| 任务类请求 | 按提交时的折扣结算 |

成功标准：

- 命中规则的请求，所有计费路径的扣费 = 原扣费 × 折扣（按现有饱和转换取整）。
- 未命中规则的请求，扣费、日志字段与没有本功能时一致。
- 迁移前后，原统一折扣客户在迁移时已有的「分组 + 模型」上扣费不变。

## 数据模型

新表 `user_discount_rules`：

```go
type UserDiscountRule struct {
	Id        int     `json:"id"`
	UserId    int     `json:"user_id" gorm:"uniqueIndex:idx_user_group_model,priority:1;not null"`
	Group     string  `json:"group" gorm:"column:group;type:varchar(64);uniqueIndex:idx_user_group_model,priority:2;not null"`
	Model     string  `json:"model" gorm:"type:varchar(255);uniqueIndex:idx_user_group_model,priority:3;not null"`
	Discount  float64 `json:"discount" gorm:"not null"`
	CreatedAt int64   `json:"created_at" gorm:"bigint"`
	UpdatedAt int64   `json:"updated_at" gorm:"bigint"`
}
```

- `group` 是保留字，原生 SQL 中使用 `commonGroupCol`。字段长度与 `abilities` 表一致（`group varchar(64)`、`model varchar(255)`），utf8mb4 下联合索引约 1280 字节，低于 InnoDB 3072 字节上限（`abilities` 主键已是同等组合）。
- `users.discount` 列保留但不再被读取，保证回滚到旧版本时仍可用。`UserBase.Discount`、`ContextKeyUserDiscount`、`RelayInfo.UserDiscount`、`EffectiveUserDiscount` 等 PR #6 的读写点移除或改为规则查询。
- 有效折扣判定沿用一个函数：NaN、≤0、≥1 一律视为 1（无折扣）。这是防御层，接口层另有校验。

### 迁移

启动时执行，幂等：

1. 查询 `discount` 在 (0, 1) 之间的用户。
2. 对每个用户，在一个事务内：按 `GetUserUsableGroups(user.Group)`（排除 `auto`）× `GetGroupEnabledModels(group)` 展开，插入规则，已存在的 `(user_id, group, model)` 不覆盖；然后将该用户 `discount` 置 0。
3. 失败的用户记录 `common.SysError` 并跳过，下次启动重试。

迁移只覆盖迁移时已有的模型；之后上线的模型按原价，与「未配置按原价」一致。

## 计费

### 规则查询

- `model.GetUserDiscountRules(userId)` 返回该用户 `map[group]map[model]discount`。启用 Redis 时按用户缓存整份规则，TTL 沿用 `RedisKeyCacheSeconds()`；规则写入或删除后立即失效。未启用 Redis 时走 `(user_id, group, model)` 索引查询。
- `model.GetUserGroupModelDiscount(userId, group, model) float64` 返回有效折扣，未命中返回 1。

### 统一入口 `HandleGroupRatio`

`hosttypes.GroupRatioInfo` 新增：

- `BaseGroupRatio float64`：真实分组倍率（特殊倍率或分组倍率，未乘折扣）。
- `GroupModelDiscount float64`：有效折扣，1 表示无折扣。

逻辑：

1. 按现有逻辑得到 `GroupRatio` / `GroupSpecialRatio` / `HasSpecialRatio`，并处理 `auto_group`。
2. `BaseGroupRatio` = 折扣前的分组倍率。
3. 用 `relayInfo.UserId`、最终 `UsingGroup`、计费模型名（`GetBillingModelName()`，与定价页模型名一致）查询折扣。
4. 折扣 < 1 时：`GroupRatio *= 折扣`；`HasSpecialRatio` 时 `GroupSpecialRatio *= 折扣`。

`GroupRatio` 仍为最终计费用的乘积，所有只读 `GroupRatio` 的路径（文本、图片、阶梯表达式、按次任务预扣、MJ、违规费、Responses WebSocket、渠道测试）无需改动。auto 跨组重试会重新调用 `HandleGroupRatio`（`controller/relay.go`、`relay/responses_websocket.go`），自动使用新分组的规则。

`ModelPriceHelperPerCall` 在调用 `HandleGroupRatio` 前未解析计费模型名，实施时确保该路径用其计价模型名（`OriginModelName`）查询，查询所用模型名与该路径计价所用模型名一致。

### 需要单独接入的路径

| 位置 | 现状 | 改动 |
|---|---|---|
| `service/quota.go` realtime/音频 | 自行计算 `actualGroupRatio` | 用 `UserId`、最终分组、该路径计价模型名查询折扣后相乘；日志写入真实分组倍率与折扣 |
| `service/task_billing.go` 任务按 token 重算 | 按 `task.Group` 重读分组倍率，乘快照的 `UserDiscount` | `TaskBillingContext` 快照字段改为 `GroupModelDiscount`（提交时 `controller/relay.go` 写入）；旧任务若只有 `user_discount` 快照，继续按它结算 |

不在结算时重新读取规则：任务提交到结算之间改规则，不影响已提交任务。

### 安全不变量

- 折扣 ∈ (0, 1)，只会让倍率变小，不引入溢出或负数风险。所有额度转换继续走 `common/quota_math.go` 现有调用，不新增转换点。
- 不通过 `OtherRatios` 实现（任务重算与部分路径不读取 `OtherRatios`，会漏折扣）。
- 规则查询失败（数据库或 Redis 错误）时记录错误并按无折扣计费，不阻断请求。

## 消费日志

- `other.group_ratio` 记录 `BaseGroupRatio`（真实分组倍率）。
- 命中规则时追加 `other.group_model_discount`（实际折扣）；未命中不写，日志与现在一致。
- 使用新字段名以区分 PR #6 期间的旧日志：旧日志的 `group_ratio` 已含折扣，`user_discount` 仅作说明，前端对旧日志保持现有展示，不重复相乘。
- 写入点：`GenerateTextOtherInfo`、`GenerateMjOtherInfo`、`service/task_billing.go`、realtime/音频日志，以及其他现有写 `group_ratio` 的位置。
- 前端日志详情、列表、移动卡片：存在 `group_model_discount` 时，计算过程展示为 `价格 × 用量 × 分组倍率 × 折扣`；「动态计费」区块同步加入折扣一项。

## 管理接口

挂在 `router/api-router.go` 的 `adminRoute`（`middleware.AdminAuth()`）下：

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/user/:id/discount_rules` | 返回该用户全部规则 |
| GET | `/api/user/:id/discount_options` | 返回该用户可用分组（排除 `auto`）及每个分组已启用的模型 |
| PUT | `/api/user/:id/discount_rules` | 请求体 `{group, models: [...], discount}`，批量写入，已存在则覆盖 |
| DELETE | `/api/user/:id/discount_rules` | 请求体 `{ids: [...]}`，批量删除该用户的规则 |

校验：

- `discount` 为有限数且 0 < discount < 1，否则返回参数错误（新增 i18n 消息）。
- `group` 必须在该用户 `GetUserUsableGroups` 中，且不为 `auto`。
- `models` 非空，去除首尾空白后去重，单次最多 500 个，每个不超过字段长度。
- 不要求模型当前已启用，便于渠道临时下线时提前配置。
- 删除只作用于 `user_id` 匹配的规则 id。

写入和删除在一个事务内完成，之后失效该用户规则缓存，并写审计记录（分组、模型数、折扣值；删除记录条数）。

用户列表接口返回 `discount_rule_count`（按当前页用户 id 一次聚合查询）。

PR #6 的 `PUT /api/user/` `discount` 字段改为忽略，不再写入该列。

## 配置界面

实施前读 `web/AGENTS.md` 与 shadcn-ui skill，复用现有组件。

- 用户列表行操作新增「折扣规则」，打开侧边面板 `users/components/dialogs/user-discount-rules-sheet.tsx`，结构参照 `subscriptions/components/dialogs/user-subscriptions-dialog.tsx`。
- 添加区：
  - 分组：`Combobox` 单选，可搜索，选项来自 `discount_options`。
  - 模型：`MultiSelect`（`showBulkActions`），只列所选分组的模型，支持搜索、全选、清空。
  - 折扣：数字输入，范围 (0, 1)，步长 0.01。
  - 选中模型中已有规则时提示「将覆盖 N 条已有规则」。
- 规则表格：分组、模型、折扣、更新时间；按分组、模型筛选；单条编辑、单条删除、勾选批量删除（使用现有确认对话框）。
- 移除用户编辑抽屉中的统一折扣输入。用户列表原 `0.8x` 标签改为「折扣 N 条」，无规则不显示。
- 新文案走 `t('English key')`，运行 `bun run i18n:sync` 并补齐 7 种语言。

## 定价页与分组列表

- `controller/pricing.go` `GetPricing`：`group_ratio` 恢复为真实分组倍率；登录用户额外返回 `user_discounts: {group: {model: discount}}`，只含该用户自己的规则。
- 前端定价页在对应分组和模型上展示折后价格并加折扣标记。
- `controller/group.go` `/api/user/self/groups` 的 `ratio` 恢复为真实分组倍率。

## 测试

后端（testify require/assert，表驱动，优先扩展现有测试文件，至多新增一个）：

- `HandleGroupRatio`：命中、未命中、命中 + 分组特殊倍率、auto 跨组后使用新分组规则、非法折扣值回退为 1。
- realtime/音频与按次计费路径命中折扣。
- 任务重算：使用提交时快照；旧任务 `user_discount` 快照兼容。
- 日志：`group_ratio` 为真实值，命中时 `group_model_discount` 为实际折扣，未命中时不写。
- 迁移：展开结果正确、重复启动幂等、迁移前后同一请求扣费相同。
- 接口：折扣越界、`auto` 或不可用分组、模型超量、跨用户删除均被拒绝；写入后缓存失效。

前端：侧边面板添加、覆盖提示、删除，以及日志详情折扣展示；`bun run build`、lint。

## 数据库验证

在真实 SQLite、MySQL（≥5.7.8）、PostgreSQL（≥9.6）上：

1. 全新库启动迁移，确认表结构、联合唯一索引。
2. 用当前生产版本建库并写入含统一折扣的用户，升级后启动，确认规则展开正确、已有数据与约束保留、`users.discount` 清零。
3. 每种场景启动两次，确认 `AutoMigrate` 无重复 `ALTER TABLE`，迁移不重复插入。
4. 规则写入、覆盖、删除往返正确。

记录精确版本、命令与结果到 PR。任何一项无法执行时明确报告阻塞，不宣称完成。

## 不做

通配符匹配、加价、免费、折扣有效期、跨客户的批量配置界面、折扣变更历史表。
