package model

import (
	"errors"
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// MaxUserDiscountRuleModels 是单次提交的模型数量上限，由 controller 在入口处校验。
const MaxUserDiscountRuleModels = 500

const userDiscountRuleCacheKeyPrefix = "user_discount_rules:"

// UserDiscountRule 是按 (用户, 分组, 模型) 配置的折扣规则，未命中按原价计费。
type UserDiscountRule struct {
	Id        int     `json:"id"`
	UserId    int     `json:"user_id" gorm:"uniqueIndex:idx_user_group_model,priority:1;not null"`
	Group     string  `json:"group" gorm:"column:group;type:varchar(64);uniqueIndex:idx_user_group_model,priority:2;not null"`
	Model     string  `json:"model" gorm:"type:varchar(255);uniqueIndex:idx_user_group_model,priority:3;not null"`
	Discount  float64 `json:"discount" gorm:"not null"`
	CreatedAt int64   `json:"created_at" gorm:"bigint"`
	UpdatedAt int64   `json:"updated_at" gorm:"bigint"`
}

func (UserDiscountRule) TableName() string {
	return "user_discount_rules"
}

func userDiscountRuleCacheKey(userId int) string {
	return fmt.Sprintf("%s%d", userDiscountRuleCacheKeyPrefix, userId)
}

// InvalidateUserDiscountRuleCache 在规则变更后丢弃该用户的规则缓存。
func InvalidateUserDiscountRuleCache(userId int) {
	if !common.RedisEnabled || common.RDB == nil {
		return
	}
	if err := common.RedisDel(userDiscountRuleCacheKey(userId)); err != nil {
		common.SysError(fmt.Sprintf("failed to invalidate user discount rule cache for user %d: %s", userId, err.Error()))
	}
}

// GetUserDiscountRuleMap 返回该用户的全部规则，结构为 group -> model -> discount。
// 启用 Redis 时整份规则按用户缓存（无规则也缓存为 {}）。
func GetUserDiscountRuleMap(userId int) (map[string]map[string]float64, error) {
	cacheKey := userDiscountRuleCacheKey(userId)
	if common.RedisEnabled {
		if cached, err := common.RedisGet(cacheKey); err == nil {
			ruleMap := make(map[string]map[string]float64)
			if err := common.UnmarshalJsonStr(cached, &ruleMap); err == nil {
				return ruleMap, nil
			}
			common.SysError(fmt.Sprintf("invalid user discount rule cache for user %d, falling back to database", userId))
		}
	}

	var rules []UserDiscountRule
	if err := DB.Where("user_id = ?", userId).Find(&rules).Error; err != nil {
		return nil, err
	}
	ruleMap := make(map[string]map[string]float64)
	for _, rule := range rules {
		if ruleMap[rule.Group] == nil {
			ruleMap[rule.Group] = make(map[string]float64)
		}
		ruleMap[rule.Group][rule.Model] = rule.Discount
	}

	if common.RedisEnabled {
		payload, err := common.Marshal(ruleMap)
		if err == nil {
			err = common.RedisSet(cacheKey, string(payload), time.Duration(common.RedisKeyCacheSeconds())*time.Second)
		}
		if err != nil {
			common.SysError(fmt.Sprintf("failed to cache user discount rules for user %d: %s", userId, err.Error()))
		}
	}
	return ruleMap, nil
}

// GetUserGroupModelDiscount 返回用户在指定分组、模型上的有效折扣。未命中、查询出错或存储值非法时返回 1（原价）。
func GetUserGroupModelDiscount(userId int, group, modelName string) float64 {
	if userId <= 0 {
		return 1
	}
	ruleMap, err := GetUserDiscountRuleMap(userId)
	if err != nil {
		common.SysError(fmt.Sprintf("failed to load discount rules for user %d: %s", userId, err.Error()))
		return 1
	}
	return EffectiveUserDiscount(ruleMap[group][modelName])
}

// ListUserDiscountRules 按分组、模型升序返回该用户的规则。
func ListUserDiscountRules(userId int) ([]UserDiscountRule, error) {
	var rules []UserDiscountRule
	err := DB.Where("user_id = ?", userId).
		Order(commonGroupCol + " ASC, model ASC").
		Find(&rules).Error
	return rules, err
}

// UpsertUserDiscountRules 为用户在指定分组下的每个模型写入同一折扣，已存在的规则被覆盖。
// 入参范围校验由 controller 负责，这里只拒绝无法生效的折扣值。
func UpsertUserDiscountRules(userId int, group string, models []string, discount float64) error {
	if EffectiveUserDiscount(discount) == 1 {
		return errors.New("discount must be greater than 0 and less than 1")
	}
	seen := make(map[string]struct{}, len(models))
	now := common.GetTimestamp()
	rules := make([]UserDiscountRule, 0, len(models))
	for _, modelName := range models {
		// 同一批次内重复的键会让 PostgreSQL 的 ON CONFLICT 报错，先去重。
		if _, ok := seen[modelName]; ok {
			continue
		}
		seen[modelName] = struct{}{}
		rules = append(rules, UserDiscountRule{
			UserId:    userId,
			Group:     group,
			Model:     modelName,
			Discount:  discount,
			CreatedAt: now,
			UpdatedAt: now,
		})
	}
	if len(rules) == 0 {
		return nil
	}

	err := DB.Transaction(func(tx *gorm.DB) error {
		return tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "user_id"}, {Name: "group"}, {Name: "model"}},
			DoUpdates: clause.AssignmentColumns([]string{"discount", "updated_at"}),
		}).CreateInBatches(&rules, 100).Error
	})
	if err != nil {
		return err
	}
	InvalidateUserDiscountRuleCache(userId)
	return nil
}

// DeleteUserDiscountRules 删除属于该用户的指定规则，返回实际删除的条数；其他用户的 id 会被忽略。
func DeleteUserDiscountRules(userId int, ids []int) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	result := DB.Where("user_id = ? AND id IN ?", userId, ids).Delete(&UserDiscountRule{})
	if result.Error != nil {
		return 0, result.Error
	}
	InvalidateUserDiscountRuleCache(userId)
	return result.RowsAffected, nil
}

// CountUserDiscountRules 一次查询返回各用户的规则数，没有规则的用户不出现在结果中。
func CountUserDiscountRules(userIds []int) (map[int]int64, error) {
	counts := make(map[int]int64)
	if len(userIds) == 0 {
		return counts, nil
	}
	var rows []struct {
		UserId int
		Count  int64
	}
	err := DB.Model(&UserDiscountRule{}).
		Select("user_id, COUNT(*) AS count").
		Where("user_id IN ?", userIds).
		Group("user_id").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		counts[row.UserId] = row.Count
	}
	return counts, nil
}

// MigrateLegacyUserDiscounts 把 users.discount 中的统一折扣展开为 (可用分组 × 该分组已启用模型) 的规则，
// 然后将 users.discount 清零（列保留以便回滚）。已存在的规则不会被覆盖；discount 不在 (0,1) 内的用户不处理。
// usableGroups 由调用方提供（model 不能依赖 service），返回用户所属分组可用的分组名。
// 单个用户失败只记录并继续，最终返回汇总错误。重复执行是幂等的。
func MigrateLegacyUserDiscounts(usableGroups func(userGroup string) []string) error {
	var users []User
	if err := DB.Where("discount > 0 AND discount < 1").Find(&users).Error; err != nil {
		return err
	}

	groupsByUserGroup := make(map[string][]string)
	modelsByGroup := make(map[string][]string)
	var errs []error
	for _, user := range users {
		groups, ok := groupsByUserGroup[user.Group]
		if !ok {
			groups = usableGroups(user.Group)
			groupsByUserGroup[user.Group] = groups
		}

		now := common.GetTimestamp()
		var rules []UserDiscountRule
		seen := make(map[[2]string]struct{})
		for _, group := range groups {
			if group == "auto" {
				continue
			}
			models, ok := modelsByGroup[group]
			if !ok {
				models = GetGroupEnabledModels(group)
				// 空结果可能是查询瞬时失败（GetGroupEnabledModels 吞掉了错误），不缓存，让后续用户重新查询。
				if len(models) > 0 {
					modelsByGroup[group] = models
				}
			}
			for _, modelName := range models {
				key := [2]string{group, modelName}
				// 同一批次内重复的键会让 PostgreSQL 的 ON CONFLICT 报错，先去重。
				if _, dup := seen[key]; dup {
					continue
				}
				seen[key] = struct{}{}
				rules = append(rules, UserDiscountRule{
					UserId:    user.Id,
					Group:     group,
					Model:     modelName,
					Discount:  user.Discount,
					CreatedAt: now,
					UpdatedAt: now,
				})
			}
		}

		if len(rules) == 0 {
			// 可能是 abilities 尚未就绪或查询瞬时失败；不清零 discount，下次启动重试，避免用户永久丢失折扣。
			common.SysError(fmt.Sprintf("skip legacy discount migration for user %d: no enabled group-model to expand, will retry on next startup", user.Id))
			continue
		}

		err := DB.Transaction(func(tx *gorm.DB) error {
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).CreateInBatches(&rules, 100).Error; err != nil {
				return err
			}
			return tx.Model(&User{}).Where("id = ?", user.Id).Update("discount", 0).Error
		})
		if err != nil {
			common.SysError(fmt.Sprintf("failed to migrate legacy discount for user %d: %s", user.Id, err.Error()))
			errs = append(errs, fmt.Errorf("user %d: %w", user.Id, err))
			continue
		}
		InvalidateUserDiscountRuleCache(user.Id)
	}
	return errors.Join(errs...)
}
