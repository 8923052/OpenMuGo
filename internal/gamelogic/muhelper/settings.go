// settings.go —— MU Helper 挂机设置的领域模型（对应 OpenMU GameLogic/MuHelper/IMuHelperSettings.cs）。
// 线上二进制解码在 view/remote/muhelper（对照 RemoteView/MuHelper/MuHelperSettingsSerializer.cs），
// 本包只携带字段语义、不含版本/协议知识。

package muhelper

// Settings 是一次解析后的挂机设置（阈值均为百分比）。
type Settings struct {
	// 治疗与药品
	AutoHeal               bool
	HealThresholdPercent   int
	UseHealPotion          bool
	PotionThresholdPercent int
	AutoHealParty          bool
	SupportParty           bool
	HealPartyThresholdPct  int
	UseDrainLife           bool
	// 技能循环
	BasicSkillId           int
	ActivationSkill1Id     int
	ActivationSkill2Id     int
	DelayMinSkill1         int
	DelayMinSkill2         int
	Skill1UseTimer         bool
	Skill1UseCondition     bool
	Skill1ConditionAttack  bool
	Skill1SubCondition     int
	Skill2UseTimer         bool
	Skill2UseCondition     bool
	Skill2ConditionAttack  bool
	Skill2SubCondition     int
	FallbackBasicAttack    bool
	UseCombo               bool
	LongRangeCounterAttack bool
	// Buff
	BuffSkill0Id            int
	BuffSkill1Id            int
	BuffSkill2Id            int
	BuffOnDuration          bool
	BuffDurationForParty    bool
	BuffCastIntervalSeconds int
	// 走位与拾取
	HuntingRange        int
	ObtainRange         int
	MaxSecondsAway      int
	ReturnToOriginalPos bool
	PickAllItems        bool
	PickSelectItems     bool
	PickJewel           bool
	PickZen             bool
	PickAncient         bool
	PickExcellent       bool
	PickExtraItems      bool
	ExtraItemNames      []string
	RepairItem          bool
	UseSelfDefense      bool
	// 宠物与自动接受
	UseDarkRaven     bool
	DarkRavenMode    int
	AutoAcceptFriend bool
	AutoAcceptGuild  bool
}
