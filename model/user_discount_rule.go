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
	if !common.RedisEnabled {
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
