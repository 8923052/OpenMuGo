// area_skill.go —— 区域技能形状设置（原版 DataModel/Configuration/AreaSkillSettings）。
//
// 背景：goldenconfig 导出 30_skills.json 时未导出 Skill.AreaSkillSettings，导致
// Go 侧一度把所有区域技能退化成"以点击点为中心的方形全向命中"——扇形技能
// （如魔剑的天雷闪系 Fire Slash/Power Slash、法师 Twister、弓手 Triple Shot…）
// 背后也能打到。原版对这些技能用 FrustumBasedTargetFilter 从**施法者位置**沿
// rotation 朝向展开一个梯形(frustum)，落在梯形外的目标不打。
//
// 数据口径 = 原版四处设置 AreaSkillSettings 的初始化/更新插件的净效果（"参照原版做法"，
// 不重跑 goldenconfig 内存初始化），逐一从源码提取：
//   - AddAreaSkillSettingsUpdatePlugIn（基础扇形/小圆/延迟）
//   - FixAreaSkillsUpdatePlugIn（Decay/Hellfire 的 EffectRange、IceStorm 延时归零）
//   - FinishDarkLordMasterTreePlugIn（Earthshake 系 target-area + 多段；Chaotic Diseier 最小命中数）
//   - FixSummonerCurseSkillsPlugIn（Explosion/Requiem/Pollution/Lightning Shock）
//
// 原版还在初始化末尾把"被替换技能(ReplacedSkill)"的 AreaSkillSettings 复制给大师/强化技能
// （见 SkillsInitializer）；本仓不载入 ReplacedSkill 图，故用 areaSkillVariants 显式登记常见变体。
//
// 结构体字段与原版 AreaSkillSettings 一一对应（TimeSpan 以毫秒存储）。
package config

import (
	"encoding/json"
	"fmt"
	"io/fs"
)

// AreaSkillSettings 是区域技能的形状/结算设置（原版 AreaSkillSettings）。
// json tag 对齐 goldenconfig 导出的 area_skill_settings 对象；源数据优先级：
//   - 若 30_skills.json 带 area_skill_settings（导出器已应用 AreaSkillSettings 更新插件）→ 直接用；
//   - 否则由 areaSkillSettingsTable（Go 侧内置表，照搬四处插件净效果）兜底（见 applyAreaSkillSettings）。
type AreaSkillSettings struct {
	// UseFrustumFilter=true 时按扇形(frustum)判定：仅施法者朝向梯形内的目标受击。
	UseFrustumFilter  bool    `json:"use_frustum_filter"`
	FrustumStartWidth float64 `json:"frustum_start_width"` // 近端半宽
	FrustumEndWidth   float64 `json:"frustum_end_width"`   // 远端半宽
	FrustumDistance   float64 `json:"frustum_distance"`    // 梯形长度（作用距离）
	// UseTargetAreaFilter=true 时以点击点为圆心、TargetAreaDiameter/2 为半径收口。
	UseTargetAreaFilter bool    `json:"use_target_area_filter"`
	TargetAreaDiameter  float64 `json:"target_area_diameter"`
	// EffectRange 为作用半径（>0 时优先于 Skill.Range 做候选半径）。
	EffectRange int `json:"effect_range"`
	// 多段命中 / 命中衰减（AttackTargetsAsync 语义）。0/默认值表示"每目标一次"。
	MinHitsPerTarget int `json:"min_hits_per_target"` // 原版 MinimumNumberOfHitsPerTarget（默认 1）
	MaxHitsPerTarget int `json:"max_hits_per_target"` // 原版 MaximumNumberOfHitsPerTarget（默认 1）
	MinHitsPerAttack int `json:"min_hits_per_attack"` // 原版 MinimumNumberOfHitsPerAttack（默认 0）
	MaxHitsPerAttack int `json:"max_hits_per_attack"` // 原版 MaximumNumberOfHitsPerAttack（默认 0）
	// HitChancePerDistanceMultiplier：超过 MinHitsPerTarget 轮后命中概率 = mult^距离。
	HitChancePerDistanceMultiplier float64 `json:"hit_chance_per_distance_multiplier"` // 默认 1
	// 延迟错峰（UseDeferredHits）：伤害按距离/轮次延迟到达（当前结算同步应用，仅登记）。
	UseDeferredHits       bool  `json:"use_deferred_hits"`
	DelayPerOneDistanceMs int64 `json:"delay_per_one_distance_ms"`
	DelayBetweenHitsMs    int64 `json:"delay_between_hits_ms"`
	// ProjectileCount>1 时把 frustum 横向切分为多条弹道，每目标只被穿越其位置的弹道命中。
	ProjectileCount int `json:"projectile_count"`
}

// IsDefault 对照 AreaSkillAttackAction.AreaSkillSettingsAreDefault：全默认则"每目标各打一次"，
// 不走多段/命中衰减分支。
func (s *AreaSkillSettings) IsDefault() bool {
	if s == nil {
		return true
	}
	const eps = 0.00001
	return !s.UseDeferredHits &&
		s.DelayPerOneDistanceMs <= 0 &&
		s.MinHitsPerTarget == 1 && s.MaxHitsPerTarget == 1 &&
		s.MinHitsPerAttack == 0 && s.MaxHitsPerAttack == 0 &&
		absF(s.HitChancePerDistanceMultiplier-1.0) <= eps
}

func absF(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

// 技能号常量（= 原版 SkillNumber 枚举值，与 30_skills.json 的 number 对齐）。
const (
	skillFlame              = 5
	skillTwister            = 8
	skillEvilSpirit         = 9
	skillAquaBeam           = 12
	skillCometfall          = 13
	skillTripleShot         = 24
	skillDecay              = 38
	skillIceStorm           = 39
	skillPenetration        = 52
	skillFireSlash          = 55
	skillPowerSlash         = 56
	skillElectricSpike      = 65
	skillForceWave          = 66
	skillStun               = 67
	skillFireScream         = 78
	skillMultiShot          = 235
	skillFlameStrike        = 236
	skillChaoticDiseier     = 238
	skillDecayStreng        = 387
	skillHellfireStreng     = 388
	skillFlameStrengDW      = 378
	skillFlameStrengMK      = 483
	skillEvilSpiritStreng   = 385
	skillEvilSpiritStrengDM = 487
	skillTripleShotStreng   = 414
	skillTripleShotMastery  = 418
	skillPenetrationStreng  = 416
	skillPowerSlashStreng   = 482
	skillForceWaveStreng    = 509
	skillFireScreamStreng   = 518
	// —— FinishDarkLordMasterTree / FixSummonerCurseSkills 新增源 ——
	skillEarthquake        = 62 // Earthshake（楼主地裂）
	skillEarthquakeStreng  = 512
	skillEarthquakeMastery = 516
	skillExplosion         = 223 // Summoner Explosion
	skillRequiem           = 224
	skillPollution         = 225
	skillLightningShock    = 230
	skillLightningShockStr = 456
)

// areaSkillSettingsTable 是基础技能 → 形状设置（AddAreaSkillSettings + FixAreaSkills 净效果）。
// 仅覆盖区域/扇形判定所需字段；未列出的技能无特殊形状（按 Skill.Range 圆形候选）。
var areaSkillSettingsTable = map[int]*AreaSkillSettings{
	// —— 扇形(frustum) 定向技能：仅施法者朝向梯形内受击 ——
	skillFireSlash:      {UseFrustumFilter: true, FrustumStartWidth: 1.5, FrustumEndWidth: 2, FrustumDistance: 2, MinHitsPerTarget: 1, MaxHitsPerTarget: 1, HitChancePerDistanceMultiplier: 1},
	skillPowerSlash:     {UseFrustumFilter: true, FrustumStartWidth: 1.0, FrustumEndWidth: 6.0, FrustumDistance: 6.0, MinHitsPerTarget: 1, MaxHitsPerTarget: 1, HitChancePerDistanceMultiplier: 1},
	skillAquaBeam:       {UseFrustumFilter: true, FrustumStartWidth: 1.5, FrustumEndWidth: 1.5, FrustumDistance: 8, MinHitsPerTarget: 1, MaxHitsPerTarget: 1, HitChancePerDistanceMultiplier: 1},
	skillForceWave:      {UseFrustumFilter: true, FrustumStartWidth: 1, FrustumEndWidth: 1, FrustumDistance: 4, MinHitsPerTarget: 1, MaxHitsPerTarget: 1, HitChancePerDistanceMultiplier: 1},
	skillStun:           {UseFrustumFilter: true, FrustumStartWidth: 1.5, FrustumEndWidth: 1.5, FrustumDistance: 3, MinHitsPerTarget: 1, MaxHitsPerTarget: 1, HitChancePerDistanceMultiplier: 1},
	skillFireScream:     {UseFrustumFilter: true, FrustumStartWidth: 2, FrustumEndWidth: 3, FrustumDistance: 6, MinHitsPerTarget: 1, MaxHitsPerTarget: 1, HitChancePerDistanceMultiplier: 1},
	skillMultiShot:      {UseFrustumFilter: true, FrustumStartWidth: 1, FrustumEndWidth: 6, FrustumDistance: 7, MinHitsPerTarget: 1, MaxHitsPerTarget: 1, HitChancePerDistanceMultiplier: 1},
	skillFlameStrike:    {UseFrustumFilter: true, FrustumStartWidth: 5, FrustumEndWidth: 2, FrustumDistance: 4, MinHitsPerTarget: 1, MaxHitsPerTarget: 1, HitChancePerDistanceMultiplier: 1},
	skillChaoticDiseier: {UseFrustumFilter: true, FrustumStartWidth: 1.5, FrustumEndWidth: 1.5, FrustumDistance: 6, MinHitsPerTarget: 1, MaxHitsPerTarget: 1, MinHitsPerAttack: 7, HitChancePerDistanceMultiplier: 1}, // FinishDarkLordMasterTree 把 MinHitsPerAttack 调为 7
	// 扇形 + 延迟错峰 + 多段命中
	skillTwister: {
		UseFrustumFilter: true, FrustumStartWidth: 1.5, FrustumEndWidth: 1.5, FrustumDistance: 4,
		UseDeferredHits: true, DelayPerOneDistanceMs: 300, DelayBetweenHitsMs: 1000,
		MinHitsPerTarget: 0, MaxHitsPerTarget: 2, HitChancePerDistanceMultiplier: 0.7,
	},
	skillPenetration: {
		UseFrustumFilter: true, FrustumStartWidth: 1.1, FrustumEndWidth: 1.2, FrustumDistance: 8,
		UseDeferredHits: true, DelayPerOneDistanceMs: 50,
		MinHitsPerTarget: 1, MaxHitsPerTarget: 1, HitChancePerDistanceMultiplier: 1,
	},
	skillElectricSpike: {
		UseFrustumFilter: true, FrustumStartWidth: 1.5, FrustumEndWidth: 1.5, FrustumDistance: 12,
		UseDeferredHits: true, DelayPerOneDistanceMs: 10,
		MinHitsPerTarget: 1, MaxHitsPerTarget: 1, HitChancePerDistanceMultiplier: 1,
	},
	skillTripleShot: {
		UseFrustumFilter: true, FrustumStartWidth: 1, FrustumEndWidth: 4.5, FrustumDistance: 7,
		UseDeferredHits: true, DelayPerOneDistanceMs: 50,
		MinHitsPerTarget: 1, MaxHitsPerTarget: 3, MaxHitsPerAttack: 3, HitChancePerDistanceMultiplier: 1,
	},
	// —— 点击点小圆(target-area) / 纯延迟(非扇形) ——
	skillFlame: {
		UseTargetAreaFilter: true, TargetAreaDiameter: 2,
		UseDeferredHits: true, DelayBetweenHitsMs: 500,
		MinHitsPerTarget: 0, MaxHitsPerTarget: 2, HitChancePerDistanceMultiplier: 0.5,
	},
	skillCometfall: {
		UseTargetAreaFilter: true, TargetAreaDiameter: 2,
		MinHitsPerTarget: 1, MaxHitsPerTarget: 1, HitChancePerDistanceMultiplier: 1,
	},
	skillIceStorm: {
		UseTargetAreaFilter: true, TargetAreaDiameter: 3,
		UseDeferredHits: true, DelayBetweenHitsMs: 0, // FixAreaSkills 把 200ms 归零
		MinHitsPerTarget: 1, MaxHitsPerTarget: 1, HitChancePerDistanceMultiplier: 1,
	},
	skillEvilSpirit: {
		UseDeferredHits: true, DelayPerOneDistanceMs: 100, DelayBetweenHitsMs: 1000,
		MinHitsPerTarget: 0, MaxHitsPerTarget: 2, HitChancePerDistanceMultiplier: 0.7,
	},
	// —— 纯作用半径(effect-range) 收口 ——
	skillDecay:          {EffectRange: 2, MinHitsPerTarget: 1, MaxHitsPerTarget: 1, HitChancePerDistanceMultiplier: 1},
	skillDecayStreng:    {EffectRange: 2, MinHitsPerTarget: 1, MaxHitsPerTarget: 1, HitChancePerDistanceMultiplier: 1},
	skillHellfireStreng: {EffectRange: 2, MinHitsPerTarget: 1, MaxHitsPerTarget: 1, HitChancePerDistanceMultiplier: 1},
	// —— 楼主(Dark Lord)：Earthshake 地裂 = 点击点直径 10 小圆 + 9~15 段命中 ——
	skillEarthquake:        {UseTargetAreaFilter: true, TargetAreaDiameter: 10, MinHitsPerAttack: 9, MaxHitsPerAttack: 15, MinHitsPerTarget: 1, MaxHitsPerTarget: 1, HitChancePerDistanceMultiplier: 1},
	skillEarthquakeStreng:  {UseTargetAreaFilter: true, TargetAreaDiameter: 10, MinHitsPerAttack: 9, MaxHitsPerAttack: 15, MinHitsPerTarget: 1, MaxHitsPerTarget: 1, HitChancePerDistanceMultiplier: 1},
	skillEarthquakeMastery: {UseTargetAreaFilter: true, TargetAreaDiameter: 10, MinHitsPerAttack: 9, MaxHitsPerAttack: 15, MinHitsPerTarget: 1, MaxHitsPerTarget: 1, HitChancePerDistanceMultiplier: 1},
	// —— 召唤师(Summoner) 诅咒系：EffectRange 收口 / Lightning Shock 大圆多段 ——
	skillExplosion:         {EffectRange: 2, MinHitsPerTarget: 1, MaxHitsPerTarget: 1, HitChancePerDistanceMultiplier: 1},
	skillRequiem:           {EffectRange: 2, MinHitsPerTarget: 1, MaxHitsPerTarget: 1, HitChancePerDistanceMultiplier: 1},
	skillPollution:         {EffectRange: 3, MinHitsPerAttack: 4, MaxHitsPerAttack: 8, MinHitsPerTarget: 1, MaxHitsPerTarget: 1, HitChancePerDistanceMultiplier: 1},
	skillLightningShock:    {UseTargetAreaFilter: true, TargetAreaDiameter: 14, MinHitsPerAttack: 5, MaxHitsPerAttack: 12, MinHitsPerTarget: 1, MaxHitsPerTarget: 1, HitChancePerDistanceMultiplier: 1},
	skillLightningShockStr: {UseTargetAreaFilter: true, TargetAreaDiameter: 14, MinHitsPerAttack: 5, MaxHitsPerAttack: 12, MinHitsPerTarget: 1, MaxHitsPerTarget: 1, HitChancePerDistanceMultiplier: 1},
}

// areaSkillVariants 把大师/强化技能指向其基础技能，复制基础形状设置
// （对照原版初始化末尾 ReplacedSkill.AreaSkillSettings 的深拷贝）。
var areaSkillVariants = map[int]int{
	skillFlameStrengDW:      skillFlame,
	skillFlameStrengMK:      skillFlame,
	skillEvilSpiritStreng:   skillEvilSpirit,
	skillEvilSpiritStrengDM: skillEvilSpirit,
	skillTripleShotStreng:   skillTripleShot,
	skillTripleShotMastery:  skillTripleShot,
	skillPenetrationStreng:  skillPenetration,
	skillPowerSlashStreng:   skillPowerSlash,
	skillForceWaveStreng:    skillForceWave,
	skillFireScreamStreng:   skillFireScream,
}

// skillsByNumber 返回 number → *Skill 索引（注意：返回的是 c.Skills 切片的元素指针，可回写）。
func (c *GameConfig) skillsByNumber() map[int]*Skill {
	m := make(map[int]*Skill, len(c.Skills))
	for i := range c.Skills {
		m[c.Skills[i].Number] = &c.Skills[i]
	}
	return m
}

// loadAreaSkillSettings 读取 goldenconfig 导出的 32_area_skill_settings.json（若存在），
// 把 area_skill_settings 覆盖到对应 Skill.Area——这是权威源（goldenconfig 显式应用
// 4 个 AreaSkillSettings 更新插件后得到，含基础 + 全部大师/强化继承）。缺文件时静默跳过。
func (c *GameConfig) loadAreaSkillSettings(fsys fs.FS) error {
	raw, err := fs.ReadFile(fsys, "32_area_skill_settings.json")
	if err != nil {
		return nil // 旧导出件缺此文件：交由内置表兜底
	}
	var entries []struct {
		Number int                `json:"number"`
		Area   *AreaSkillSettings `json:"area"`
	}
	if err := json.Unmarshal(raw, &entries); err != nil {
		return fmt.Errorf("config: 解析 32_area_skill_settings.json: %w", err)
	}
	byNumber := c.skillsByNumber()
	for _, e := range entries {
		if sk := byNumber[e.Number]; sk != nil && e.Area != nil {
			sk.Area = e.Area
		}
	}
	return nil
}

// applyAreaSkillSettings 在 Load 解析 30_skills.json 后兜底回填形状设置。
// 只填 Skill.Area 仍为 nil 的技能（若导出件已带 area_skill_settings 则以导出件为准）。
// 变体继承必须在基础表之后（依赖基础技能已赋值）。
func (c *GameConfig) applyAreaSkillSettings() {
	byNumber := c.skillsByNumber()
	// 基础表（深拷贝，避免调用方改动共享指针指向的全局表）。仅在导出件未带时兜底。
	for num, st := range areaSkillSettingsTable {
		if sk := byNumber[num]; sk != nil && sk.Area == nil {
			cp := *st
			sk.Area = &cp
		}
	}
	// 变体 → 基础技能（ReplacedSkill 深拷贝等价）。
	for variant, base := range areaSkillVariants {
		v, b := byNumber[variant], byNumber[base]
		if v != nil && b != nil && b.Area != nil && v.Area == nil {
			cp := *b.Area
			v.Area = &cp
		}
	}
}
