// magiceffect.go —— 魔法效果基础设施（T2-6「MagicEffect 基础」）。
//
// 对照原版：
//   - GameLogic/MagicEffect.cs（效果实例：Definition / Duration / PowerUpElements / 定时到期）
//   - GameLogic/MagicEffectsList.cs（每对象的**有序**效果表 + 同号更新 + 到期移除 + 死亡清除）
//   - GameServer/RemoteView/World/DeActivateMagicEffectPlugIn.cs（C1 07 激活/去激活出站判据）
//
// 与出站相关的两条判据（**必须**逐字保留，客户端据此显示/隐藏 buff 图标）：
//   - `effect.Id < InvisibleEffectStartIndex`（200）→ 才发给**自己**；
//     `effect.Definition.Number <= 0` 时插件直接 return（不发光效包）；
//   - 发给**观察者**还额外要求 `Definition.InformObservers`；去激活时再要求 owner 存活。
//
// 裁剪登记（doc/15 T2-6）：原版把 PowerUp 作为 IElement 挂进属性系统的事件流，
// Go 侧属性系统是"按需重建"（BuildCharacterAttributeSystem 吃 overrides），
// 因此本表只维护效果实例与聚合值，由调用方在重建属性时把 PowerUpsFor 并入 overrides。
// 另：**本仓目前没有效果生产者**——导出件未包含 MagicEffectDefinition，药水类消耗品
// 在原版走各自专用 handler（ConsumeEffect 为空）。第一个真实生产者随技能系统（T3）接入。
package action

import "sort"

// InvisibleEffectStartIndex 对照原版 MagicEffectsList.InvisibleEffectStartIndex：
// 编号 >= 200 的效果不下发给客户端。
const InvisibleEffectStartIndex = 200

// MagicEffectDefinition 是魔法效果的定义面（对照原版 DataModel MagicEffectDefinition 的子集）。
type MagicEffectDefinition struct {
	// Number 是效果编号（原版 Definition.Number / MagicEffect.Id）。
	Number int16
	// SubType 用于"同子类互斥"（原版 ApplyMagicEffectConsumeHandlerPlugIn 先按 SubType 顶替）。
	SubType byte
	// DurationMs 是持续时间（原版 Duration 秒 → 毫秒）。
	DurationMs int
	// SendDuration 为 true 时激活包带剩余秒数（原版 SendDuration）。
	SendDuration bool
	// InformObservers 为 true 时还要广播给世界观察者。
	InformObservers bool
	// StopByDeath 为 true 时角色死亡即移除（原版 ClearEffectsAfterDeathAsync）。
	StopByDeath bool
}

// MagicEffectPowerUp 是效果对某个属性的加成（对照原版 MagicEffect.ElementWithTarget）。
type MagicEffectPowerUp struct {
	Attribute string // 属性 designation（导出件口径）
	Value     float32
	// AggType 为注入目标属性时的聚合形态（数值与 attribute.AggregateType 对齐：
	// 0 AddRaw、1 Multiplicate、2 AddFinal、3 Maximum；对照 boost.ConstantValue.AggregateType）。
	AggType byte
}

// MagicEffect 是一个已激活的效果实例（对照原版 MagicEffect）。
type MagicEffect struct {
	Definition MagicEffectDefinition
	PowerUps   []MagicEffectPowerUp
	// CastSkill 是"这个效果由哪个技能带来"（0 = 非技能来源，如药水）。
	// 原版把 power-up 缓存在 SkillEntry 上并随大师等级失效重算（AddMasterPointAction.cs:82-83），
	// 本仓属性按需重建，因此把来源记在实例上，重建时才能重做同一套大师混算。
	CastSkill int
	// ExpiresAtMs 为到期时刻（毫秒时间戳）。由 MagicEffectsList 维护，调用方只读。
	ExpiresAtMs int64
}

// Value 对照原版 MagicEffect.Value：取**第一个** PowerUp 元素的值（无元素为 0）。
// 原版用它做"不允许减益"的比较。
func (e MagicEffect) Value() float32 {
	if len(e.PowerUps) == 0 {
		return 0
	}
	return e.PowerUps[0].Value
}

// SendsToSelf 报告是否要给自己发激活/去激活包（原版 `Id < 200` + `Number <= 0` 早退）。
func (e MagicEffect) SendsToSelf() bool {
	return e.Definition.Number > 0 && int(e.Definition.Number) < InvisibleEffectStartIndex
}

// SendsToObservers 报告是否要广播给世界观察者（原版 `InformObservers` 且编号可见）。
// 去激活路径上原版还额外要求 owner 存活——由调用方判断。
func (e MagicEffect) SendsToObservers() bool {
	return e.SendsToSelf() && e.Definition.InformObservers
}

// RemainingSeconds 返回剩余秒数（启用 SendDuration 时进激活包）。
func (e MagicEffect) RemainingSeconds(nowMs int64) int {
	if e.ExpiresAtMs <= nowMs {
		return 0
	}
	return int((e.ExpiresAtMs - nowMs) / 1000)
}

// MagicEffectsList 是单个对象的效果表（对照原版 MagicEffectsList）。
// 非并发安全：调用方（会话/玩家）负责串行化。
type MagicEffectsList struct {
	effects map[int16]*MagicEffect
}

// NewMagicEffectsList 构造空表。
func NewMagicEffectsList() *MagicEffectsList {
	return &MagicEffectsList{effects: make(map[int16]*MagicEffect)}
}

// Len 返回活动效果数。
func (l *MagicEffectsList) Len() int {
	if l == nil {
		return 0
	}
	return len(l.effects)
}

// Add 对照原版 MagicEffectsList.AddEffectAsync：
//
//	同编号已存在 → UpdateEffect（新旧值比较，新值不大于旧值则**什么都不做**——不允许减益；
//	否则刷新时长并替换 PowerUp）；否则新增并把到期时刻设为 nowMs + Duration。
//
// 返回 added，对应原版"只有 added 为 true 才下发激活包"的语义。
func (l *MagicEffectsList) Add(e MagicEffect, nowMs int64) (added bool) {
	if l == nil {
		return false
	}
	if l.effects == nil {
		l.effects = make(map[int16]*MagicEffect)
	}
	old, ok := l.effects[e.Definition.Number]
	if !ok {
		cp := e
		cp.ExpiresAtMs = nowMs + int64(e.Definition.DurationMs)
		l.effects[e.Definition.Number] = &cp
		return true
	}
	// UpdateEffect：不允许减益（原版 `if (magicEffect.Value > effect.Value) return;`）。
	if old.Value() > e.Value() {
		return false
	}
	// 原版只刷新 Duration（ResetTimer）+ 按需替换 PowerUp，Definition 沿用旧实例。
	old.ExpiresAtMs = nowMs + int64(e.Definition.DurationMs)
	old.PowerUps = e.PowerUps
	return false
}

// ReplaceBySubType 移除并返回同 SubType 的既有效果（对照原版
// ApplyMagicEffectConsumeHandlerPlugIn：先 TryGetActiveEffectOfSubTypeAsync 再 DisposeAsync）。
// 调用方负责对被移除的效果出去激活包。
func (l *MagicEffectsList) ReplaceBySubType(subType byte) (MagicEffect, bool) {
	if l == nil {
		return MagicEffect{}, false
	}
	for id, e := range l.effects {
		if e.Definition.SubType == subType {
			delete(l.effects, id)
			return *e, true
		}
	}
	return MagicEffect{}, false
}

// RemoveByNumber 按效果编号移除并返回该效果（对照原版
// AntidoteConsumeHandlerPlugIn：`ActiveEffects.TryGetValue(PoisonEffectNumber, out var effect)`
// → `effect.Dispose()`——Dispose 走的就是 MagicEffectsList 的移除路径）。
// 调用方负责对被移除的效果出去激活包。
func (l *MagicEffectsList) RemoveByNumber(number int16) (MagicEffect, bool) {
	if l == nil {
		return MagicEffect{}, false
	}
	e, ok := l.effects[number]
	if !ok {
		return MagicEffect{}, false
	}
	delete(l.effects, number)
	return *e, true
}

// Expire 对照原版 OnEffectTimeOutAsync：移除并返回所有已到期的效果。
//
// 到期判据为 `nowMs >= ExpiresAtMs`（原版由 Timer 在 Duration 后触发）。
// 返回顺序按编号升序，保证出站顺序稳定可断言。
func (l *MagicEffectsList) Expire(nowMs int64) []MagicEffect {
	if l == nil || len(l.effects) == 0 {
		return nil
	}
	var out []MagicEffect
	for id, e := range l.effects {
		if nowMs >= e.ExpiresAtMs {
			out = append(out, *e)
			delete(l.effects, id)
		}
	}
	sortEffects(out)
	return out
}

// ClearByDeath 对照原版 ClearEffectsAfterDeathAsync：移除并返回 StopByDeath 的效果。
func (l *MagicEffectsList) ClearByDeath() []MagicEffect {
	if l == nil || len(l.effects) == 0 {
		return nil
	}
	var out []MagicEffect
	for id, e := range l.effects {
		if e.Definition.StopByDeath {
			out = append(out, *e)
			delete(l.effects, id)
		}
	}
	sortEffects(out)
	return out
}

// Snapshot 返回当前活动效果的快照（按编号升序，对应原版 SortedList 的遍历顺序）。
func (l *MagicEffectsList) Snapshot() []MagicEffect {
	if l == nil || len(l.effects) == 0 {
		return nil
	}
	out := make([]MagicEffect, 0, len(l.effects))
	for _, e := range l.effects {
		out = append(out, *e)
	}
	sortEffects(out)
	return out
}

// VisibilitySnapshot 返回"可下发给观察者"的效果（对照原版 VisibleEffects）。
func (l *MagicEffectsList) VisibilitySnapshot() []MagicEffect {
	var out []MagicEffect
	for _, e := range l.Snapshot() {
		if e.Definition.InformObservers {
			out = append(out, e)
		}
	}
	return out
}

// PowerUpsFor 汇总当前活动效果对某属性的加成（原版把每个 PowerUp 作为独立元素加入
// 属性系统，聚合时相加——故这里求和）。
func (l *MagicEffectsList) PowerUpsFor(designation string) float32 {
	var sum float32
	for _, e := range l.Snapshot() {
		for _, p := range e.PowerUps {
			if p.Attribute == designation {
				sum += p.Value
			}
		}
	}
	return sum
}

// sortEffects 按效果编号升序（同编号时保持原顺序，实际不会重复）。
func sortEffects(list []MagicEffect) {
	sort.SliceStable(list, func(i, j int) bool {
		return list[i].Definition.Number < list[j].Definition.Number
	})
}
