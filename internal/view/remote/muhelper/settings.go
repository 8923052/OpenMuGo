// settings.go —— MU Helper 设置 blob 解码，对应 OpenMU
// GameServer/RemoteView/MuHelper/MuHelperSettingsSerializer.cs。
// 布局全部小端、pack(1)；不足 MinBlobLength 视为无效（原版返回 null）。
package muhelper

import helper "mugo/internal/gamelogic/muhelper"

const (
	minBlobLength = 69

	pickupFlagsOffset   = 1
	rangeFlagsOffset    = 2
	distanceMinOffset   = 3
	basicSkillOffset    = 5
	activation1Offset   = 7
	delay1Offset        = 9
	activation2Offset   = 11
	delay2Offset        = 13
	castingBuffOffset   = 15
	buff0Offset         = 17
	buff1Offset         = 19
	buff2Offset         = 21
	hpThresholdOffset   = 23
	partyDrainOffset    = 24
	behaviorFlagsOffset = 25
	skill1FlagsOffset   = 26
	skill2FlagsOffset   = 27
	petAttackOffset     = 28
	helperFlagsOffset   = 29
	extraItemsOffset    = 65
	extraItemsEnd       = 245
	extraItemSlots      = 12
	extraItemLength     = 15

	nibble       = 0x0F
	thresholdMul = 10
)

// TryDeserialize 解码 257B 客户端 blob；nil 表示长度不足（原版同义）。
func TryDeserialize(blob []byte) *helper.Settings {
	if len(blob) < minBlobLength {
		return nil
	}
	pickup := blob[pickupFlagsOffset]
	ranges := blob[rangeFlagsOffset]
	hpThr := blob[hpThresholdOffset]
	partyDrain := blob[partyDrainOffset]
	behavior := blob[behaviorFlagsOffset]
	skill1 := blob[skill1FlagsOffset]
	skill2 := blob[skill2FlagsOffset]
	helperBits := blob[helperFlagsOffset]

	s := &helper.Settings{
		HuntingRange:   int(ranges & nibble),
		ObtainRange:    int((ranges >> 4) & nibble),
		MaxSecondsAway: word(blob, distanceMinOffset),
		BasicSkillId:   word(blob, basicSkillOffset),

		ActivationSkill1Id: word(blob, activation1Offset),
		DelayMinSkill1:     word(blob, delay1Offset),
		ActivationSkill2Id: word(blob, activation2Offset),
		DelayMinSkill2:     word(blob, delay2Offset),

		BuffCastIntervalSeconds: word(blob, castingBuffOffset),
		BuffSkill0Id:            word(blob, buff0Offset),
		BuffSkill1Id:            word(blob, buff1Offset),
		BuffSkill2Id:            word(blob, buff2Offset),

		PotionThresholdPercent: int(hpThr&nibble) * thresholdMul,
		HealThresholdPercent:   int((hpThr>>4)&nibble) * thresholdMul,
		HealPartyThresholdPct:  int(partyDrain&nibble) * thresholdMul,

		PickJewel:      pickup&(1<<3) != 0,
		PickAncient:    pickup&(1<<4) != 0, // 原版 setItem 位（远古套装）
		PickExcellent:  pickup&(1<<5) != 0,
		PickZen:        pickup&(1<<6) != 0,
		PickExtraItems: pickup&(1<<7) != 0,

		// byte25 的位序与字段名不是一一对应（原版即如此，照抄）：
		UseHealPotion:          behavior&(1<<0) != 0, // autoPotion
		AutoHeal:               behavior&(1<<1) != 0,
		UseDrainLife:           behavior&(1<<2) != 0,
		LongRangeCounterAttack: behavior&(1<<3) != 0,
		ReturnToOriginalPos:    behavior&(1<<4) != 0,
		UseCombo:               behavior&(1<<5) != 0,
		SupportParty:           behavior&(1<<6) != 0, // party
		AutoHealParty:          behavior&(1<<7) != 0, // prefPartyHeal

		BuffDurationForParty:  skill1&(1<<0) != 0,
		UseDarkRaven:          skill1&(1<<1) != 0, // dark spirits
		BuffOnDuration:        skill1&(1<<2) != 0,
		Skill1UseTimer:        skill1&(1<<3) != 0,
		Skill1UseCondition:    skill1&(1<<4) != 0,
		Skill1ConditionAttack: skill1&(1<<5) != 0,
		Skill1SubCondition:    int((skill1 >> 6) & 0x03),

		Skill2UseTimer:        skill2&(1<<0) != 0,
		Skill2UseCondition:    skill2&(1<<1) != 0,
		Skill2ConditionAttack: skill2&(1<<2) != 0,
		Skill2SubCondition:    int((skill2 >> 3) & 0x03),
		RepairItem:            skill2&(1<<5) != 0,
		PickAllItems:          skill2&(1<<6) != 0,
		PickSelectItems:       skill2&(1<<7) != 0,

		DarkRavenMode:       int(blob[petAttackOffset]),
		UseSelfDefense:      helperBits&(1<<0) != 0,
		AutoAcceptFriend:    helperBits&(1<<1) != 0,
		AutoAcceptGuild:     helperBits&(1<<2) != 0,
		FallbackBasicAttack: helperBits&(1<<3) != 0,
	}
	s.ExtraItemNames = extraItemNames(blob)
	return s
}

// extraItemNames 取 12 个 15 字节 NUL 结尾 ASCII 名字槽（blob 不足则留空）。
func extraItemNames(blob []byte) []string {
	if len(blob) < extraItemsEnd {
		return nil
	}
	var out []string
	for i := 0; i < extraItemSlots; i++ {
		start := extraItemsOffset + i*extraItemLength
		length := 0
		for length < extraItemLength && blob[start+length] != 0 {
			length++
		}
		if length > 0 {
			out = append(out, string(blob[start:start+length]))
		}
	}
	return out
}

func word(b []byte, offset int) int {
	return int(b[offset]) | int(b[offset+1])<<8
}
