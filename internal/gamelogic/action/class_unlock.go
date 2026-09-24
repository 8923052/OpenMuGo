package action

// class_unlock.go —— 等级到线解锁"可创建职业"（对照原版
// GameLogic/PlugIns/UnlockCharacterClass 的四个插件类 + UnlockCharacterAtLevelBase）。
// 原版是"角色升级插件"，解锁结果写进账号（Account.UnlockedCharacterClasses），
// 在**下一次角色列表**时以 CreationAllowedFlag 聚合下发（F3 00 的 UnlockFlags 与 DE 00）。

import (
	"mugo/internal/gamelogic/config"
	"mugo/internal/gamelogic/entity"
)

// ClassUnlockRule 是一条"某个角色升到该等级后，账号可创建某职业"的规则。
type ClassUnlockRule struct {
	ClassNumber  int
	MinimumLevel int
}

// ClassUnlockRules 逐条对应原版插件：Dark Lord@250、Magic Gladiator@220、
// Rage Fighter@150、Summoner@1（门槛 1 是原样照抄，不是笔误）。
var ClassUnlockRules = []ClassUnlockRule{
	{ClassNumber: 16, MinimumLevel: 250},
	{ClassNumber: 12, MinimumLevel: 220},
	{ClassNumber: 24, MinimumLevel: 150},
	{ClassNumber: 20, MinimumLevel: 1},
}

// UnlockClassesAtLevel 把"等级已达门槛且账号尚未解锁"的职业加进账号，返回新解锁的
// 职业号；配置里查不到的职业跳过并通过 missing 返回（原版在此打 LogWarning，不解锁）。
func UnlockClassesAtLevel(acc *entity.Account, gc *config.GameConfig, level int) (unlocked, missing []int) {
	if acc == nil || gc == nil {
		return nil, nil
	}
	for _, r := range ClassUnlockRules {
		if level < r.MinimumLevel || accountHasUnlockedClass(acc, r.ClassNumber) {
			continue
		}
		if _, ok := gc.Class(r.ClassNumber); !ok {
			missing = append(missing, r.ClassNumber)
			continue
		}
		acc.UnlockedClasses = append(acc.UnlockedClasses, r.ClassNumber)
		unlocked = append(unlocked, r.ClassNumber)
	}
	return unlocked, missing
}

// accountHasUnlockedClass 报告账号是否已解锁某职业（原版 All(c => c.Number != n) 取反）。
func accountHasUnlockedClass(acc *entity.Account, classNumber int) bool {
	for _, n := range acc.UnlockedClasses {
		if n == classNumber {
			return true
		}
	}
	return false
}

// CharacterCreationUnlockFlags 把账号已解锁职业的 CreationAllowedFlag 按位或起来
// （对照 ShowCharacterListPlugIn.CreateUnlockFlags:51-58）。
func CharacterCreationUnlockFlags(acc *entity.Account, gc *config.GameConfig) byte {
	if acc == nil || gc == nil {
		return 0
	}
	var flags byte
	for _, n := range acc.UnlockedClasses {
		if def, ok := gc.Class(n); ok {
			flags |= byte(def.CreationAllowedFlag)
		}
	}
	return flags
}
