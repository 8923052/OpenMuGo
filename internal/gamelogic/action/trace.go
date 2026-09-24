package action

// trace.go —— 动作 trace 回放骨架（doc/10 防线 3 的基建，T2-0 交付）。
//
// 语义：RecordingView 包装任意 PlayerView，把每个出站调用记为一条语义 trace
// （方法名 + 关键入参）。"录制" = 生产视图跑真实流程；"回放" = Go 侧同输入跑
// 同 action 序列，比对两条 trace 完全一致——即"动作级与原版一致"的最强保证
// （doc/10 §7.2 防线 3：同输入 → 同状态 → 同出站）。
//
// 字节级比对由 view/remote 的 golden 测试承担（防线 4）；本层只锁语义序列。

import (
	"fmt"
)

// TraceEntry 是一条语义级 trace。
type TraceEntry struct {
	Method string
	Detail string
}

// String 便于测试比对。
func (t TraceEntry) String() string { return t.Method + "(" + t.Detail + ")" }

// RecordingView 记录全部出站调用的 PlayerView 装饰器（透传给内层视图）。
type RecordingView struct {
	Inner  PlayerView
	Traces []TraceEntry
}

// NewRecordingView 包装一个 PlayerView。
func NewRecordingView(inner PlayerView) *RecordingView {
	return &RecordingView{Inner: inner}
}

func (r *RecordingView) trace(method, detail string) error {
	r.Traces = append(r.Traces, TraceEntry{Method: method, Detail: detail})
	return nil
}

// ShowLoginResult 实现 PlayerView。
func (r *RecordingView) ShowLoginResult(result LoginResult) error {
	return r.trace("LoginResult", fmt.Sprint(int(result)))
}

// ShowLogoutResponse 实现 PlayerView。
func (r *RecordingView) ShowLogoutResponse(t LogoutType) error {
	return r.trace("LogoutResponse", fmt.Sprint(int(t)))
}

// ShowSkillList 实现 PlayerView。
func (r *RecordingView) ShowSkillList(skills []SkillListView) error {
	detail := ""
	for _, s := range skills {
		detail += fmt.Sprintf("%d(L%d),", s.SkillNumber, s.Level)
	}
	return r.trace("SkillList", detail)
}

// ShowCharacterList 实现 PlayerView。
func (r *RecordingView) ShowCharacterList(entries []CharacterListEntry, classUnlockFlags byte) error {
	detail := ""
	for _, e := range entries {
		detail += e.Name + ","
	}
	return r.trace("CharacterList", detail)
}

// ShowCharacterInformation 实现 PlayerView。
func (r *RecordingView) ShowCharacterInformation(info CharacterInformation) error {
	return r.trace("CharacterInfo", info.Character.Name)
}

// ShowCharacterCreationSuccess 实现 PlayerView。
func (r *RecordingView) ShowCharacterCreationSuccess(v CreatedCharacterView) error {
	return r.trace("CreationSuccess", fmt.Sprintf("%s@%d", v.Name, v.Slot))
}

// ShowCharacterCreationFailed 实现 PlayerView。
func (r *RecordingView) ShowCharacterCreationFailed() error {
	return r.trace("CreationFailed", "")
}

// ShowCharacterDeleteResponse 实现 PlayerView。
func (r *RecordingView) ShowCharacterDeleteResponse(result CharacterDeleteResponseResult) error {
	return r.trace("DeleteResponse", fmt.Sprint(int(result)))
}

// ShowCharacterInScope 实现 PlayerView。
func (r *RecordingView) ShowCharacterInScope(entry ScopeEntry) error {
	return r.trace("ScopeIn", entry.Name)
}

// ShowNpcsInScope 实现 PlayerView。
func (r *RecordingView) ShowNpcsInScope(entries []NpcScopeEntry) error {
	detail := ""
	for _, e := range entries {
		detail += fmt.Sprint(e.TypeNumber) + ","
	}
	return r.trace("NpcsInScope", detail)
}

// ShowDropsInScope 实现 PlayerView。
func (r *RecordingView) ShowDropsInScope(items []DropEntry, money []MoneyEntry) error {
	detail := ""
	for _, e := range items {
		detail += fmt.Sprintf("#%d@%d.%d,", e.ID, e.X, e.Y)
	}
	detail += "|"
	for _, m := range money {
		detail += fmt.Sprintf("#%d@%d.%d=%d,", m.ID, m.X, m.Y, m.Amount)
	}
	return r.trace("DropsInScope", detail)
}

// ShowObjectsOutOfScope 实现 PlayerView。
func (r *RecordingView) ShowObjectsOutOfScope(ids []uint16) error {
	detail := ""
	for _, id := range ids {
		detail += fmt.Sprint(id) + ","
	}
	return r.trace("OutOfScope", detail)
}

// ShowObjectHit 实现 PlayerView。
func (r *RecordingView) ShowObjectHit(targetID uint16, healthStatus, shieldStatus byte, damage, shieldDamage uint32, kind DamageKind) error {
	return r.trace("ObjectHit", fmt.Sprintf("#%d=%d(%d)hp%d", targetID, damage, kind, healthStatus))
}

// ShowObjectGotKilled 实现 PlayerView。
func (r *RecordingView) ShowObjectGotKilled(victimID uint16) error {
	return r.trace("GotKilled", fmt.Sprint(victimID))
}

// ShowCurrentStatsExtended 实现 PlayerView（当前值四项的统一出口，C1 26 FF 24B）。
func (r *RecordingView) ShowCurrentStatsExtended(stats CurrentStats) error {
	return r.trace("CurrentStats", fmt.Sprintf("hp=%d sd=%d mp=%d ag=%d",
		stats.Health, stats.Shield, stats.Mana, stats.Ability))
}

// ShowItemAddedToInventory 实现 PlayerView。
func (r *RecordingView) ShowItemAddedToInventory(slot byte, itemData []byte) error {
	return r.trace("ItemAdded", fmt.Sprintf("slot=%d len=%d", slot, len(itemData)))
}

// ShowInventoryMoneyUpdate 实现 PlayerView。
func (r *RecordingView) ShowInventoryMoneyUpdate(money uint32) error {
	return r.trace("MoneyUpdate", fmt.Sprint(money))
}

// ShowItemDropRemoved 实现 PlayerView。
func (r *RecordingView) ShowItemDropRemoved(ids []uint16) error {
	detail := ""
	for _, id := range ids {
		detail += fmt.Sprint(id) + ","
	}
	return r.trace("DropRemoved", detail)
}

// ShowItemDropResponse 实现 PlayerView。
func (r *RecordingView) ShowItemDropResponse(success bool, slot byte) error {
	return r.trace("DropResult", fmt.Sprintf("%v@%d", success, slot))
}

// ShowObjectMovedInstant 实现 PlayerView。
func (r *RecordingView) ShowObjectMovedInstant(objectID uint16, x, y byte) error {
	return r.trace("MovedInstant", fmt.Sprintf("#%d@%d.%d", objectID, x, y))
}

// ShowMapChanged 实现 PlayerView。
func (r *RecordingView) ShowMapChanged(mapNumber uint16, x, y, rotation byte) error {
	return r.trace("MapChanged", fmt.Sprintf("map=%d@%d.%d", mapNumber, x, y))
}

// ShowMapChangeFailed 实现 PlayerView。
func (r *RecordingView) ShowMapChangeFailed(mapNumber uint16, x, y, rotation byte) error {
	return r.trace("MapChangeFailed", fmt.Sprintf("map=%d@%d.%d", mapNumber, x, y))
}

// ShowObjectWalked 实现 PlayerView。
func (r *RecordingView) ShowObjectWalked(move WalkedMove) error {
	return r.trace("Walked", fmt.Sprint(move.ObjectID))
}

// TracesEqual 比较两条 trace 序列是否完全一致。
func TracesEqual(a, b []TraceEntry) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
