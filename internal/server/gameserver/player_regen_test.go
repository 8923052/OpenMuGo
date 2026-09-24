package gameserver

// player_regen_test.go —— 真机三症状的回归锁定：
//  ① 死亡重生后 HP/蓝/AG/SD 不回满（原版 SetReclaimableAttributesToMaximum）；
//  ② 打死怪物不回红蓝（原版 AfterKilledMonsterAsync）；
//  ③ 城里不回红蓝（原版 RecoverTimer + 安全区 AG 加成 + 护盾 hiatus）。
//
// 三条共用一条硬口径：当前值**只能**走 C1 26 FF（24B 扩展）下发——
// 客户端 `case 0x26` 只认 ReceiveStatsExtended，9B 的 CurrentHealthAndShield
// 会被按 24B 解析而读到越界垃圾。

import (
	"math"
	"testing"
	"time"

	"mugo/internal/gamelogic/action"
	"mugo/internal/gamelogic/entity"
	"mugo/internal/gamelogic/world"
	s2c "mugo/internal/proto/s2c"
)

// currentStatsFrames 返回 C1 26 FF 帧。
func currentStatsFrames(rec *packetRecorder) [][]byte {
	var out [][]byte
	for _, f := range rec.frames {
		if len(f) >= 4 && f[0] == 0xC1 && f[2] == 0x26 && f[3] == 0xFF {
			out = append(out, f)
		}
	}
	return out
}

// lastCurrentStats 取最后一帧 C1 26 FF 并校验是 24B 扩展形态。
func lastCurrentStats(t *testing.T, rec *packetRecorder) *s2c.CurrentStatsExtended {
	t.Helper()
	frames := currentStatsFrames(rec)
	if len(frames) == 0 {
		t.Fatal("未下发 C1 26 FF（客户端血/蓝/AG/SD 会停在旧值）")
	}
	f := frames[len(frames)-1]
	if len(f) != s2c.CurrentStatsExtendedLength {
		t.Fatalf("C1 26 FF 必须是 %dB 扩展形态, got %d: %X", s2c.CurrentStatsExtendedLength, len(f), f)
	}
	return s2c.AsCurrentStatsExtended(f)
}

// regenScaffold 在脚手架之上补两件周期恢复必需的事：会话登记（trackedSessions 源）
// 与合成地形（脚手架不加载真实 .att，安全区必须造出来）。
func regenScaffold(t *testing.T) (*Server, *session, *world.Player, *entity.Character, *packetRecorder, byte, byte, byte, byte) {
	t.Helper()
	srv, sess, wp, c, rec := newExpScaffold(t, 200)
	sx, sy, wx, wy := setTestTerrain(t, srv)
	srv.trackSession(sess) // 未登记的会话不在 trackedSessions 里，周期恢复跑不到它
	return srv, sess, wp, c, rec, sx, sy, wx, wy
}

// setTestTerrain 铺一张合成地形：全部可走，(safeX,safeY) 一格为安全区。
// 脚手架不加载真实 .att，安全区必须这样造出来才能验证"城里回得快"。
func setTestTerrain(t *testing.T, srv *Server) (safeX, safeY, wildX, wildY byte) {
	t.Helper()
	safeX, safeY, wildX, wildY = 10, 10, 20, 20
	data := make([]byte, 3+256*256)
	for i := 3; i < len(data); i++ {
		data[i] = 0 // 0 = 普通可走
	}
	data[3+int(safeY)*256+int(safeX)] = 1 // 1 = 可走且安全区
	srv.world.Map(0).SetTerrain(world.ParseTerrain(data))
	return safeX, safeY, wildX, wildY
}

// TestRespawnRestoresAllReclaimableAttributes 锁定症状①：重生必须把**四项**当前值
// 全部顶到上限（旧实现只回血），且重入世界时下发 C1 26 FF 告知客户端。
func TestRespawnRestoresAllReclaimableAttributes(t *testing.T) {
	srv, sess, _, c, rec := newExpScaffold(t, 200)
	st := c.Stats
	// 死亡态：四项当前值清零（原版复活前就是这个状态）。
	st.CurrentHealth, st.CurrentMana, st.CurrentAbility, st.CurrentShield = 0, 0, 0, 0

	srv.respawnDeadPlayer(sess)

	if st.CurrentHealth != st.MaximumHealth || st.CurrentMana != st.MaximumMana ||
		st.CurrentAbility != st.MaximumAbility || st.CurrentShield != st.MaximumShield {
		t.Fatalf("重生应把四项都顶到上限: 血 %d/%d 蓝 %d/%d AG %d/%d 盾 %d/%d",
			st.CurrentHealth, st.MaximumHealth, st.CurrentMana, st.MaximumMana,
			st.CurrentAbility, st.MaximumAbility, st.CurrentShield, st.MaximumShield)
	}
	if sess.getState() != entity.StateEnteringMap {
		t.Fatalf("重生后应回到 EnteringMap 等客户端 F3 12, got %d", sess.getState())
	}

	// 客户端 F3 12 重入 → 必须下发当前值，否则客户端血条仍显示死亡时的 0。
	rec.frames = nil
	srv.handleClientReady(sess)
	p := lastCurrentStats(t, rec)
	if p.Health() != st.MaximumHealth || p.Mana() != st.MaximumMana ||
		p.Ability() != st.MaximumAbility || p.Shield() != st.MaximumShield {
		t.Fatalf("重入下发值应全满: 包(血%d 盾%d 蓝%d AG%d) 上限(血%d 盾%d 蓝%d AG%d)",
			p.Health(), p.Shield(), p.Mana(), p.Ability(),
			st.MaximumHealth, st.MaximumShield, st.MaximumMana, st.MaximumAbility)
	}
}

// TestKillRecoversAttributesAndSendsCurrentStats 锁定症状②：击杀后按
// AfterMonsterKillRegenerationAttributes 一次性入账，并下发 C1 26 FF。
// 倍率/绝对值默认全 0，这里注入非 0 值才能验证链路（默认不回血蓝与原版一致）。
func TestKillRecoversAttributesAndSendsCurrentStats(t *testing.T) {
	srv, sess, wp, c, rec := newExpScaffold(t, 200)
	cv := *sess.getCombatValues()
	cv.ManaAfterKillMult = 0.5
	cv.HealthAfterKillAbs = 50
	sess.setCombatValues(&cv)

	st := c.Stats
	st.CurrentMana, st.CurrentHealth = 0, 0
	// 独立手算：蓝 = min(max, 0 + (uint)(0.5×max))；血 = min(max, 0 + 50)。
	wantMana := uint32(0.5 * float64(st.MaximumMana))
	wantHealth := uint32(50)

	rec.frames = nil
	target := firstAliveMonster(t, srv, 0)
	killNpc(t, srv, sess, wp, c, target)

	if st.CurrentMana != wantMana {
		t.Fatalf("击杀后蓝=%d, want %d (0.5×%d)", st.CurrentMana, wantMana, st.MaximumMana)
	}
	if st.CurrentHealth != wantHealth {
		t.Fatalf("击杀后血=%d, want %d", st.CurrentHealth, wantHealth)
	}
	p := lastCurrentStats(t, rec)
	if p.Mana() != wantMana || p.Health() != wantHealth {
		t.Fatalf("下发值应等于入账值: 包(血%d 蓝%d) want(血%d 蓝%d)",
			p.Health(), p.Mana(), wantHealth, wantMana)
	}
}

// TestKillWithoutRecoveryAttributesSendsNothing 锁定"默认不回血蓝"：
// 卓越/镶嵌给值前，击杀不得产生任何恢复帧（与原版默认属性一致）。
func TestKillWithoutRecoveryAttributesSendsNothing(t *testing.T) {
	srv, sess, wp, c, rec := newExpScaffold(t, 200)
	st := c.Stats
	st.CurrentMana, st.CurrentHealth = 0, 0

	rec.frames = nil
	target := firstAliveMonster(t, srv, 0)
	killNpc(t, srv, sess, wp, c, target)

	if st.CurrentMana != 0 || st.CurrentHealth != 0 {
		t.Fatalf("默认属性下击杀不应回血蓝: 血%d 蓝%d", st.CurrentHealth, st.CurrentMana)
	}
	if n := len(currentStatsFrames(rec)); n != 0 {
		t.Fatalf("无恢复时不应下发 C1 26 FF, got %d", n)
	}
}

// TestRegenInSafezoneRecoversManaAndAbility 锁定症状③：城里周期恢复蓝与 AG，
// 且走 24B 扩展下发；护盾因 hiatus <10s 本轮不回（由另一测试单独锁定）。
func TestRegenInSafezoneRecoversManaAndAbility(t *testing.T) {
	srv, sess, wp, c, rec, sx, sy, _, _ := regenScaffold(t)
	wp.X, wp.Y = sx, sy
	c.X, c.Y = sx, sy

	st := c.Stats
	st.CurrentMana, st.CurrentAbility, st.CurrentShield = 0, 0, 0
	cv := sess.getCombatValues()

	t0 := time.Unix(1_700_000_000, 0)
	sess.beginRegen(t0) // 把恢复基准时刻钉死（返回的上次 elapsed 忽略）
	rec.frames = nil

	srv.regeneratePlayers(t0.Add(3 * time.Second))

	// 独立手算：蓝 = (max×倍率+绝对值)×3/3；AG = 同式且安全区额外 +3 绝对值。
	wantMana := uint32(math.Floor(float64(st.MaximumMana)*float64(cv.ManaRegenMult) + float64(cv.ManaRegenAbs)))
	wantAbility := uint32(math.Floor(float64(st.MaximumAbility)*float64(cv.AbilityRegenMult) +
		float64(cv.AbilityRegenAbs) + action.AbilityRegenSafezoneBonus))
	if wantMana == 0 || wantAbility == 0 {
		t.Fatalf("固定等级下蓝/AG 恢复量应为正: mana=%d ability=%d", wantMana, wantAbility)
	}
	if st.CurrentMana != wantMana {
		t.Fatalf("城里回蓝=%d, want %d (max=%d mult=%v abs=%v)",
			st.CurrentMana, wantMana, st.MaximumMana, cv.ManaRegenMult, cv.ManaRegenAbs)
	}
	if st.CurrentAbility != wantAbility {
		t.Fatalf("城里回 AG=%d, want %d（含安全区 +3）", st.CurrentAbility, wantAbility)
	}
	// hiatus 未满 10s：护盾本轮不回。
	if st.CurrentShield != 0 {
		t.Fatalf("护盾静置未满 10s 不应回盾, got %d", st.CurrentShield)
	}
	p := lastCurrentStats(t, rec)
	if p.Mana() != wantMana || p.Ability() != wantAbility {
		t.Fatalf("下发值应等于入账值: 包(蓝%d AG%d) want(蓝%d AG%d)",
			p.Mana(), p.Ability(), wantMana, wantAbility)
	}
}

// TestRegenAbilityWithoutSafezoneBonus 锁定安全区加成的**边界**：城外没有 +3。
func TestRegenAbilityWithoutSafezoneBonus(t *testing.T) {
	srv, sess, wp, c, _, _, _, wx, wy := regenScaffold(t)
	wp.X, wp.Y = wx, wy
	c.X, c.Y = wx, wy

	st := c.Stats
	st.CurrentAbility = 0
	cv := sess.getCombatValues()

	t0 := time.Unix(1_700_000_000, 0)
	sess.beginRegen(t0)
	srv.regeneratePlayers(t0.Add(3 * time.Second))

	want := uint32(math.Floor(float64(st.MaximumAbility)*float64(cv.AbilityRegenMult) + float64(cv.AbilityRegenAbs)))
	if st.CurrentAbility != want {
		t.Fatalf("城外回 AG=%d, want %d（不得含安全区 +3）", st.CurrentAbility, want)
	}
}

// TestRegenShieldWaitsForHiatus 锁定护盾 hiatus：静置不足 10s 不回，够了才回满。
func TestRegenShieldWaitsForHiatus(t *testing.T) {
	srv, sess, wp, c, _, sx, sy, _, _ := regenScaffold(t)
	wp.X, wp.Y = sx, sy
	c.X, c.Y = sx, sy
	if c.Stats.MaximumShield == 0 {
		t.Skip("该职业/等级无护盾上限，无法验证")
	}

	st := c.Stats
	st.CurrentShield = 0
	t0 := time.Unix(1_700_000_000, 0)
	sess.beginRegen(t0)

	// 3s < 10s：不回。
	srv.regeneratePlayers(t0.Add(3 * time.Second))
	if st.CurrentShield != 0 {
		t.Fatalf("hiatus=3s 不应回盾, got %d", st.CurrentShield)
	}
	// 累计 3+9=12s ≥ 10s：开始回盾。
	// 真实倍率（修正聚合形态后）：baseMult=100×(1/75000)×ramp(4/3)≈0.001778，
	// hiatus=12s → ramp 缩放 ×1.6 → 每秒 1436×0.0028444≈4.08 点；
	// 9s 恢复 (uint)(4.0846×9)=36（原版回满需数分钟，"瞬间回满"是聚合 bug 时代的预期）。
	srv.regeneratePlayers(t0.Add(12 * time.Second))
	if st.CurrentShield != 36 {
		t.Fatalf("hiatus=12s 应按真实速率回 36 点盾: got %d/%d", st.CurrentShield, st.MaximumShield)
	}
}

// TestRegenSkipsDeadPlayer 锁定防御分支：死亡（IsAlive=false）的玩家不参与周期恢复。
func TestRegenSkipsDeadPlayer(t *testing.T) {
	srv, sess, wp, c, rec, sx, sy, _, _ := regenScaffold(t)
	wp.X, wp.Y = sx, sy
	c.X, c.Y = sx, sy
	st := c.Stats
	st.CurrentMana = 0
	wp.IsAlive = false

	t0 := time.Unix(1_700_000_000, 0)
	sess.beginRegen(t0)
	rec.frames = nil
	srv.regeneratePlayers(t0.Add(3 * time.Second))

	if st.CurrentMana != 0 {
		t.Fatalf("死亡玩家不应回蓝, got %d", st.CurrentMana)
	}
	if n := len(currentStatsFrames(rec)); n != 0 {
		t.Fatalf("死亡玩家不应下发 C1 26 FF, got %d", n)
	}
}

// TestRestingEnablesHealthRegen 锁定"坐下才回血"：原版 HealthRecoveryMultiplier
// = 0.03×IsResting（关系图）+ 休息周期 5s（Stats.cs IntervalResting），未休息时
// regenHP=0.0000 是**原版行为**。0x18 坐下动画 → IsResting → 快照重算 → HP 回复。
func TestRestingEnablesHealthRegen(t *testing.T) {
	srv, sess, wp, c, _, sx, sy, _, _ := regenScaffold(t)
	wp.X, wp.Y = sx, sy
	c.X, c.Y = sx, sy
	st := c.Stats
	st.CurrentHealth = 0

	t0 := time.Unix(1_700_000_000, 0)
	// 未休息：HP 恒不回（倍率 0）。
	sess.beginRegen(t0)
	srv.regeneratePlayers(t0.Add(3 * time.Second))
	if st.CurrentHealth != 0 {
		t.Fatalf("未休息不应回血（原版 HP 倍率=0.03×IsResting）, got %d", st.CurrentHealth)
	}

	// C1 18 坐下动画（[3]=Rotation [4]=0x80）→ 休息态。
	srv.handleAnimation(sess, []byte{0xC1, 0x18, 0x05, 0x00, 0x80})
	if !sess.getResting() {
		t.Fatal("0x18 坐下动画后应处于休息态（IsResting=1）")
	}

	// 休息 5s：HP 回 (uint)(0.03×max×5/5) = (uint)(0.03×max)。
	srv.regeneratePlayers(t0.Add(8 * time.Second))
	want := uint32(0.03 * float64(st.MaximumHealth))
	if want == 0 || st.CurrentHealth != want {
		t.Fatalf("休息 5s 应回血 %d, got %d (max=%d)", want, st.CurrentHealth, st.MaximumHealth)
	}

	// 走路（真实移动）清休息态 → 停止回血。
	if !sess.setResting(false) {
		t.Fatal("移动后应清除休息态")
	}
	srv.refreshCombatValues(sess, c, false)
	before := st.CurrentHealth
	sess.beginRegen(t0.Add(8 * time.Second))
	srv.regeneratePlayers(t0.Add(14 * time.Second))
	if st.CurrentHealth != before {
		t.Fatalf("起身移动后不应继续回血: %d → %d", before, st.CurrentHealth)
	}
}

// TestRestingSpeedsManaRegen 锁定休息时蓝的叠加周期：原版 RegenerateAsync 中
// MP 休息时 factor = elapsed/3s（常规）+ elapsed/5s（休息），等效合成周期 1.875s。
func TestRestingSpeedsManaRegen(t *testing.T) {
	srv, sess, wp, c, _, sx, sy, _, _ := regenScaffold(t)
	wp.X, wp.Y = sx, sy
	c.X, c.Y = sx, sy
	st := c.Stats
	st.CurrentMana = 0

	t0 := time.Unix(1_700_000_000, 0)
	sess.beginRegen(t0)
	srv.handleAnimation(sess, []byte{0xC1, 0x18, 0x05, 0x00, 0x80})
	srv.regeneratePlayers(t0.Add(5 * time.Second))

	// 蓝倍率 = 类基值 0.037 + 0.03×IsResting（属性系统关系图）；
	// 5s 的合成周期 1.875s → factor=5/1.875=2.6667。
	want := uint32(float64(st.MaximumMana) * (0.037 + 0.03) * (5.0 / 1.875))
	if st.CurrentMana != want {
		t.Fatalf("休息 5s 回蓝=%d, want %d (max=%d)", st.CurrentMana, want, st.MaximumMana)
	}
}
