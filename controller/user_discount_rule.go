package controller

import (
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

// maxDiscountRuleModelNameBytes 与 user_discount_rules.model 列宽一致。
const maxDiscountRuleModelNameBytes = 255

// loadManagedDiscountUser 解析 :id 并确认操作者有权管理该用户；失败时已写出响应并返回 nil。
func loadManagedDiscountUser(c *gin.Context) *model.User {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return nil
	}
	user, err := model.GetUserById(id, false)
	if err != nil {
		common.ApiError(c, err)
		return nil
	}
	if !canManageTargetRole(c.GetInt("role"), user.Role) {
		common.ApiErrorI18n(c, i18n.MsgUserNoPermissionHigherLevel)
		return nil
	}
	return user
}

func GetUserDiscountRules(c *gin.Context) {
	user := loadManagedDiscountUser(c)
	if user == nil {
		return
	}
	rules, err := model.ListUserDiscountRules(user.Id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, rules)
}

// GetUserDiscountOptions 返回该用户可配置折扣的分组，以及各分组当前启用的模型和真实倍率（不含折扣）。
func GetUserDiscountOptions(c *gin.Context) {
	user := loadManagedDiscountUser(c)
	if user == nil {
		return
	}
	usableGroups := service.GetUserUsableGroups(user.Group)
	groupNames := make([]string, 0, len(usableGroups))
	for group := range usableGroups {
		if group != "auto" {
			groupNames = append(groupNames, group)
		}
	}
	slices.Sort(groupNames)

	type groupOption struct {
		Group  string   `json:"group"`
		Desc   string   `json:"desc"`
		Ratio  float64  `json:"ratio"`
		Models []string `json:"models"`
	}
	options := make([]groupOption, 0, len(groupNames))
	for _, group := range groupNames {
		models := model.GetGroupEnabledModels(group)
		if models == nil {
			models = []string{}
		}
		slices.Sort(models)
		options = append(options, groupOption{
			Group:  group,
			Desc:   usableGroups[group],
			Ratio:  service.GetUserGroupRatio(user.Group, group),
			Models: models,
		})
	}
	common.ApiSuccess(c, gin.H{"groups": options})
}

func PutUserDiscountRules(c *gin.Context) {
	user := loadManagedDiscountUser(c)
	if user == nil {
		return
	}
	var req struct {
		Group    string   `json:"group"`
		Models   []string `json:"models"`
		Discount float64  `json:"discount"`
	}
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	if math.IsNaN(req.Discount) || math.IsInf(req.Discount, 0) || req.Discount <= 0 || req.Discount >= 1 {
		common.ApiErrorI18n(c, i18n.MsgUserDiscountInvalid)
		return
	}
	if _, usable := service.GetUserUsableGroups(user.Group)[req.Group]; !usable || req.Group == "auto" {
		common.ApiErrorI18n(c, i18n.MsgUserDiscountGroupInvalid)
		return
	}

	models := make([]string, 0, len(req.Models))
	for _, name := range req.Models {
		name = strings.TrimSpace(name)
		if name == "" || slices.Contains(models, name) {
			continue
		}
		if len(name) > maxDiscountRuleModelNameBytes {
			common.ApiErrorI18n(c, i18n.MsgUserDiscountModelsInvalid)
			return
		}
		models = append(models, name)
	}
	if len(models) == 0 || len(models) > model.MaxUserDiscountRuleModels {
		common.ApiErrorI18n(c, i18n.MsgUserDiscountModelsInvalid)
		return
	}

	if err := model.UpsertUserDiscountRules(user.Id, req.Group, models, req.Discount); err != nil {
		common.ApiError(c, err)
		return
	}
	recordManageAuditFor(c, user.Id, "user.discount_rules.upsert", map[string]any{
		"username":    user.Username,
		"group":       req.Group,
		"model_count": len(models),
		"discount":    req.Discount,
	})
	common.ApiSuccess(c, gin.H{"updated": len(models)})
}

func DeleteUserDiscountRules(c *gin.Context) {
	user := loadManagedDiscountUser(c)
	if user == nil {
		return
	}
	var req struct {
		Ids []int `json:"ids"`
	}
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	deleted, err := model.DeleteUserDiscountRules(user.Id, req.Ids)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	recordManageAuditFor(c, user.Id, "user.discount_rules.delete", map[string]any{
		"username": user.Username,
		"deleted":  deleted,
	})
	common.ApiSuccess(c, gin.H{"deleted": deleted})
}

// fillDiscountRuleCounts 用一次查询为一页用户填充 discount_rule_count；失败只记录日志，计数保持为 0。
func fillDiscountRuleCounts(users []*model.User) {
	ids := make([]int, 0, len(users))
	for _, user := range users {
		ids = append(ids, user.Id)
	}
	counts, err := model.CountUserDiscountRules(ids)
	if err != nil {
		common.SysError("failed to count user discount rules: " + err.Error())
		return
	}
	for _, user := range users {
		user.DiscountRuleCount = counts[user.Id]
	}
}
