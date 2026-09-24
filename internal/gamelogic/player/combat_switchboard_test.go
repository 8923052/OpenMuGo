package player

// combat_switchboard_test.go —— 大师树"按当前武器位选一条属性"的三块开关板
// （TRIM-06d，对照 AttackableExtensions.cs:958-1041）。用 map 伪造属性读取，
// 断言选择次序与"单手剑+锤取平均"等细节。

import "testing"

// vals 造一个按 designation 查表的属性读取器（表里没有即 0）。
func vals(m map[string]float32) valueFunc {
	return func(d string) float32 { return m[d] }
}

func TestMasterPhysicalPreBuffsWeaponSelection(t *testing.T) {
	cases := []struct {
		name string
		in   map[string]float32
		want int
	}{
		{"空手", map[string]float32{}, 0},
		{"弓", map[string]float32{"Is Bow Equipped": 1, "Bow Strengthener Bonus Damage (MST)": 7}, 7},
		{"弓优先于十字弓", map[string]float32{
			"Is Bow Equipped": 1, "Bow Strengthener Bonus Damage (MST)": 7,
			"Is Cross Bow Equipped": 1, "Cross Bow Strengthener Bonus Damage (MST)": 5,
		}, 7},
		{"十字弓", map[string]float32{
			"Is Cross Bow Equipped": 1, "Cross Bow Strengthener Bonus Damage (MST)": 5,
		}, 5},
		{"双手剑", map[string]float32{
			"Is Two Handed Sword Equipped": 1, "Two Handed Sword Strengthener Bonus Damage (MST)": 3,
		}, 3},
		// 前置段只认弓/十字弓/双手剑三条，装了矛也不加。
		{"矛不在前置段", map[string]float32{
			"Is Spear Equipped": 1, "Spear Bonus Damage (MST)": 99,
		}, 0},
		{"装备标志在但加成为 0", map[string]float32{"Is Bow Equipped": 1}, 0},
	}
	for _, tc := range cases {
		if got := masterPhysicalPreBuffs(vals(tc.in)); got != tc.want {
			t.Fatalf("%s：期望 %d，got %d", tc.name, tc.want, got)
		}
	}
}

func TestMasterPhysicalPostBuffsSelectionAndAveraging(t *testing.T) {
	const generic = "Master Skill Physical Bonus Damage (MST)"
	cases := []struct {
		name string
		in   map[string]float32
		want int
	}{
		{"只有通用桶", map[string]float32{generic: 4}, 4},
		{"矛提前返回并叠通用桶", map[string]float32{
			"Is Spear Equipped": 1, "Spear Bonus Damage (MST)": 11, generic: 2,
			"Is Glove Weapon Equipped": 1, "Glove Weapon Bonus Damage (MST)": 99,
		}, 13},
		{"权杖", map[string]float32{
			"Is Scepter Equipped": 1, "Scepter Strengthener Bonus Damage (MST)": 6, generic: 1,
		}, 7},
		{"拳套", map[string]float32{
			"Is Glove Weapon Equipped": 1, "Glove Weapon Bonus Damage (MST)": 5,
		}, 5},
		{"单手剑", map[string]float32{
			"Is One Handed Sword Equipped": 1, "One Handed Sword Bonus Damage (MST)": 7,
		}, 7},
		// 只有锤时直接用锤值。
		{"锤", map[string]float32{"Is Mace Equipped": 1, "Mace Bonus Damage (MST)": 9}, 9},
		// 单手剑与锤同时装备取平均：int((7+9)/2)=8。
		{"单手剑加锤取平均", map[string]float32{
			"Is One Handed Sword Equipped": 1, "One Handed Sword Bonus Damage (MST)": 7,
			"Is Mace Equipped": 1, "Mace Bonus Damage (MST)": 9,
		}, 8},
		// 单手剑加锤且除不尽时向下取整：int((7+8)/2)=7。
		{"平均向下取整", map[string]float32{
			"Is One Handed Sword Equipped": 1, "One Handed Sword Bonus Damage (MST)": 7,
			"Is Mace Equipped": 1, "Mace Bonus Damage (MST)": 8,
		}, 7},
		// 剑值为 0 时不取平均，直接用锤值。
		{"剑值为零则用锤", map[string]float32{
			"Is One Handed Sword Equipped": 1, "Is Mace Equipped": 1, "Mace Bonus Damage (MST)": 9,
		}, 9},
		{"平均后仍叠通用桶", map[string]float32{
			"Is One Handed Sword Equipped": 1, "One Handed Sword Bonus Damage (MST)": 7,
			"Is Mace Equipped": 1, "Mace Bonus Damage (MST)": 9, generic: 3,
		}, 11},
	}
	for _, tc := range cases {
		if got := masterPhysicalPostBuffs(vals(tc.in)); got != tc.want {
			t.Fatalf("%s：期望 %d，got %d", tc.name, tc.want, got)
		}
	}
}

func TestMasterMasteryPvPBonusSelection(t *testing.T) {
	cases := []struct {
		name string
		in   map[string]float32
		want int
	}{
		{"空手", map[string]float32{}, 0},
		{"双手剑优先", map[string]float32{
			"Is Two Handed Sword Equipped": 1, "Two Handed Sword Mastery PvP Bonus Damage (MST)": 5,
			"Is Scepter Equipped": 1, "Scepter Mastery PvP Bonus Damage (MST)": 99,
		}, 5},
		{"双手杖", map[string]float32{
			"Is Two Handed Staff Equipped": 1, "Two Handed Staff Mastery PvP Bonus Damage (MST)": 4,
		}, 4},
		{"十字弓", map[string]float32{
			"Is Cross Bow Equipped": 1, "Cross Bow Mastery PvP Bonus Damage (MST)": 3,
		}, 3},
		{"手杖", map[string]float32{"Is Stick Equipped": 1, "Stick Mastery PvP Bonus Damage (MST)": 2}, 2},
		{"权杖", map[string]float32{
			"Is Scepter Equipped": 1, "Scepter Mastery PvP Bonus Damage (MST)": 6,
		}, 6},
		// 弓/矛/拳套不在这块板上。
		{"弓不在板上", map[string]float32{"Is Bow Equipped": 1}, 0},
	}
	for _, tc := range cases {
		if got := masterMasteryPvPBonus(vals(tc.in)); got != tc.want {
			t.Fatalf("%s：期望 %d，got %d", tc.name, tc.want, got)
		}
	}
}
