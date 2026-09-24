// settings_test.go —— 257B 挂机设置 blob 的逐字段解码对拍（对照 MuHelperSettingsSerializer 的位域表）。
package muhelper

import (
	"testing"
)

// blobOf 造一个 minLength 足够、指定偏移写过值的 blob。
func blobOf(patches map[int]byte, length int) []byte {
	b := make([]byte, length)
	for i := range b {
		b[i] = 0
	}
	for off, v := range patches {
		b[off] = v
	}
	return b
}

func TestTryDeserializeRejectsShortBlob(t *testing.T) {
	if s := TryDeserialize(nil); s != nil {
		t.Fatalf("nil blob 应返回 nil, got %+v", s)
	}
	if s := TryDeserialize(make([]byte, minBlobLength-1)); s != nil {
		t.Fatal("长度不足 MinBlobLength 应返回 nil")
	}
	if s := TryDeserialize(make([]byte, minBlobLength)); s == nil {
		t.Fatal("刚好 MinBlobLength 应可解析")
	}
}

func TestTryDeserializeThresholdsAreNibbleTimesTen(t *testing.T) {
	// byte23 = 0x58 → 下探 nibble 8 → 药品阈值 80%，高 nibble 5 → 治疗阈值 50%。
	s := TryDeserialize(blobOf(map[int]byte{hpThresholdOffset: 0x58, partyDrainOffset: 0x03}, minBlobLength))
	if s.PotionThresholdPercent != 80 {
		t.Fatalf("药品阈值应为 80, got %d", s.PotionThresholdPercent)
	}
	if s.HealThresholdPercent != 50 {
		t.Fatalf("治疗阈值应为 50, got %d", s.HealThresholdPercent)
	}
	if s.HealPartyThresholdPct != 30 {
		t.Fatalf("队友治疗阈值应为 30, got %d", s.HealPartyThresholdPct)
	}
}

func TestTryDeserializeRangesAndWords(t *testing.T) {
	// byte2 = 0x43 → 打怪半径低 nibble 3、拾取半径高 nibble 4。
	s := TryDeserialize(blobOf(map[int]byte{
		rangeFlagsOffset:     0x43,
		distanceMinOffset:    0x0A, // 10 秒
		basicSkillOffset:     0x3C, // 60 = 0x003C 小端
		basicSkillOffset + 1: 0x00,
		buff2Offset:          0x11,
		buff2Offset + 1:      0x22, // 0x2211
		petAttackOffset:      2,
	}, minBlobLength))
	if s.HuntingRange != 3 || s.ObtainRange != 4 {
		t.Fatalf("半径应为 3/4, got %d/%d", s.HuntingRange, s.ObtainRange)
	}
	if s.MaxSecondsAway != 10 {
		t.Fatalf("MaxSecondsAway=%d, want 10", s.MaxSecondsAway)
	}
	if s.BasicSkillId != 60 {
		t.Fatalf("BasicSkillId=%d, want 60", s.BasicSkillId)
	}
	if s.BuffSkill2Id != 0x2211 {
		t.Fatalf("BuffSkill2Id=%#x, want 0x2211", s.BuffSkill2Id)
	}
	if s.DarkRavenMode != 2 {
		t.Fatalf("DarkRavenMode=%d, want 2（同行）", s.DarkRavenMode)
	}
}

// TestTryDeserializeBehaviorFlagMisordering 锁原版 blob 的"错位"映射：
// byte25 bit0→UseHealPotion（不是 AutoHeal）、bit6→SupportParty、bit7→AutoHealParty。
func TestTryDeserializeBehaviorFlagMisordering(t *testing.T) {
	s := TryDeserialize(blobOf(map[int]byte{behaviorFlagsOffset: 1<<0 | 1<<6 | 1<<7}, minBlobLength))
	if !s.UseHealPotion || s.AutoHeal {
		t.Fatalf("bit0 应是 UseHealPotion 而非 AutoHeal: %+v", s)
	}
	if !s.SupportParty || !s.AutoHealParty {
		t.Fatalf("bit6/bit7 应为 SupportParty/AutoHealParty, got %+v", s)
	}
	s2 := TryDeserialize(blobOf(map[int]byte{behaviorFlagsOffset: 1 << 1}, minBlobLength))
	if !s2.AutoHeal || s2.UseHealPotion {
		t.Fatalf("bit1 应是 AutoHeal, got %+v", s2)
	}
}

func TestTryDeserializeSkillAndHelperBits(t *testing.T) {
	s := TryDeserialize(blobOf(map[int]byte{
		skill1FlagsOffset: 1<<0 | 1<<1 | 1<<3 | 3<<6, // buffParty + darkSpirits + skill1Timer + subCon=3
		skill2FlagsOffset: 1<<5 | 1<<6,               // repair + pickAll
		helperFlagsOffset: 1<<1 | 1<<3,               // autoAcceptFriend + fallbackBasicAttack
	}, minBlobLength))
	if !s.BuffDurationForParty || !s.UseDarkRaven || !s.Skill1UseTimer || s.Skill1SubCondition != 3 {
		t.Fatalf("byte26 解码不符: %+v", s)
	}
	if !s.RepairItem || !s.PickAllItems || s.PickSelectItems {
		t.Fatalf("byte27 解码不符: %+v", s)
	}
	if !s.AutoAcceptFriend || s.AutoAcceptGuild || !s.FallbackBasicAttack || s.UseSelfDefense {
		t.Fatalf("byte29 解码不符: %+v", s)
	}
}

func TestTryDeserializeExtraItemNames(t *testing.T) {
	blob := blobOf(nil, extraItemsEnd)
	copy(blob[extraItemsOffset:extraItemsOffset+4], "Noak")                                    // 槽 0
	copy(blob[extraItemsOffset+extraItemLength:extraItemsOffset+extraItemLength+7], "Ancient") // 槽 1
	s := TryDeserialize(blob)
	if len(s.ExtraItemNames) != 2 || s.ExtraItemNames[0] != "Noak" || s.ExtraItemNames[1] != "Ancient" {
		t.Fatalf("额外拾取名单解码不符: %#v", s.ExtraItemNames)
	}
	// blob 不足以覆盖名字区时留空（原版 if (blob.Length >= ExtraItemsEndOffset)）。
	if got := TryDeserialize(blobOf(nil, minBlobLength)); len(got.ExtraItemNames) != 0 {
		t.Fatalf("短 blob 不应解出名字: %#v", got.ExtraItemNames)
	}
}
