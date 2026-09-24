package config

// pet.go —— 可训练宠物（黑暗之马 / 黑暗渡鸦）的判定、经验曲线与渡鸦升级门槛。
//
// 对照原版：
//   - DataModel/ItemExtensions.IsTrainablePet —— 定义为 PetExperienceFormula 非空；
//     Season6 里**只有** (13,4) 黑暗之马与 (13,5) 黑暗渡鸦设置了该公式（见
//     Persistence/Initialization/VersionSeasonSix/Items/Pets.cs）。导出件不带
//     pet_experience_formula，故这里按 (group, number) 精确判定，语义与原版一致。
//   - PetLevelHelper.GetExperienceOfPetLevel —— 公式 "level^3*100*(level+10)"。
//   - ItemExtensions.GetDarkRavenLeadershipRequirement —— 渡鸦升到 petLevel 所需统率 = petLevel*15+185。

// 可训练宠物的 (group, number)。
const (
	petGroup         = 13
	darkHorseNumber  = 4
	darkRavenNumber  = 5
	petLevelMaxValue = 50
)

// IsTrainablePet 报告物品定义是否为可自动升级的宠物（黑暗之马 / 黑暗渡鸦）。
func (i *Item) IsTrainablePet() bool {
	return i != nil && i.Group == petGroup &&
		(i.Number == darkHorseNumber || i.Number == darkRavenNumber)
}

// IsDarkHorse 报告定义是否为黑暗之马（骑乘/防御可训练宠，槽 8）。
func (i *Item) IsDarkHorse() bool {
	return i != nil && i.Group == petGroup && i.Number == darkHorseNumber
}

// IsDarkRaven 报告定义是否为黑暗渡鸦（攻击可训练宠，槽 1）。
func (i *Item) IsDarkRaven() bool {
	return i != nil && i.Group == petGroup && i.Number == darkRavenNumber
}

// MaximumPetLevel 返回宠物等级上限（导出件 maximum_item_level；无值回落 50）。
func (i *Item) MaximumPetLevel() int {
	if i != nil && i.MaximumItemLevel > 0 {
		return i.MaximumItemLevel
	}
	return petLevelMaxValue
}

// PetExperienceForLevel 返回"达到 petLevel 所需的累计经验"（对照
// GetExperienceOfPetLevel(level, maxLevel)：level<=0 或 >maxLevel 时返回 uint 上限）。
// 公式 level^3*100*(level+10)。
func PetExperienceForLevel(petLevel, maxLevel int) uint64 {
	if petLevel <= 0 || petLevel > maxLevel {
		return ^uint64(0)
	}
	l := uint64(petLevel)
	return l * l * l * 100 * (l + 10)
}

// DarkRavenLeadershipRequirement 返回渡鸦升到 petLevel 所需的总统率。
func DarkRavenLeadershipRequirement(petLevel int) int {
	return petLevel*15 + 185
}
