package gameserver

// handler_experience_test.go —— T2-7 闭环测试：击杀怪物 → 经验下发（C3 16 扩展形态）→
// 升级（C1 F3 05）+ 升级光效（C1 48）→ 掉落金钱按**计算经验**结算。
//
// 期望值用独立实现的原版公式手算（不外调被测函数），否则公式写错时测试会一起错。

import (
	"math"
	"testing"

	"mugo/internal/gamelogic/config"
	"mugo/internal/gamelogic/entity"
	"mugo/internal/gamelogic/npc"
	"mugo/internal/gamelogic/world"
	"mugo/internal/persistence"
	c2s "mugo/internal/proto/c2s"
	s2c "mugo/internal/proto/s2c"
	"mugo/internal/server/loginserver"
	"mugo/internal/util"
	"mugo/internal/version"
	"mugo/internal/view/remote"
)

// newExpScaffold 构造带怪物生成器的 GS 与指定等级的会话（已进世界）。
func newExpScaffold(t *testing.T, level uint16) (*Server, *session, *world.Player, *entity.Character, *packetRecorder) {
	t.Helper()
	cfg, err := config.LoadSeason6()
	if err != nil {
		t.Fatal(err)
	}
	sp := npc.NewSpawner(cfg, util.NewRand(0x5EED), nil)
	sp.SpawnAll()
	store := persistence.NewMemoryStore()
	login := loginserver.NewLoginService(store, loginserver.NewSessionRegistry())
	eps := []Endpoint{{ListenAddr: "127.0.0.1:0", Client: version.MuMain()}}
	srv := New(0, "exp", eps, nil, login, login, Config{GameConfig: cfg, NPCs: sp, Drops: world.NewDropRegistry()})
	srv.dropDelay = 0 // 同上：经验测试里的金币掉落需同步可见

	rec := &packetRecorder{}
	c := &entity.Character{
		Name: "leveler", ClassNumber: 4, Level: level,
		MapNumber: 0, X: 139, Y: 127, Rotation: 0,
		AppearanceExt: make([]byte, 27),
	}
	sess := newSession(nil, 7, Endpoint{})
	sess.setAccount(&entity.Account{Name: "leveler"})
	sess.setSelected(c)
	sess.setState(entity.StateEnteringMap)
	sess.setVersion(version.MuMain().ClientVersion())
	sess.mu.Lock()
	sess.playerView = remote.NewPlayerView(rec, true, muMainClient, nil)
	sess.mu.Unlock()
	srv.enterWorld(sess, c)
	if sess.getState() != entity.StateEnteredWorld {
		t.Fatalf("进世界失败: %d", sess.getState())
	}
	wp := sess.getWorldPlayer()
	if wp == nil {
		t.Fatal("世界玩家缺失")
	}
	return srv, sess, wp, c, rec
}

// firstAliveMonster 返回该地图上第一个**可被玩家击杀**的存活对象。
// Lorencia 的生成表里前面就是商人/守卫（PassiveNpc/Guard），原版不可攻击，
// 故必须按可打性过滤——早先版本能"打死守卫"本身就是缺陷。
func firstAliveMonster(t *testing.T, srv *Server, mapNumber uint16) *npc.Npc {
	t.Helper()
	for _, n := range srv.deps.cfg.NPCs.ByMap(mapNumber) {
		if n.Alive() && n.IsAttackableByPlayer() {
			return n
		}
	}
	t.Fatal("地图上无可击杀怪物")
	return nil
}

// killNpc 把会话贴到怪身上连击直到死亡（handleHit 要求切比雪夫距离 ≤2）。
func killNpc(t *testing.T, srv *Server, sess *session, wp *world.Player, c *entity.Character, target *npc.Npc) {
	t.Helper()
	wp.X, wp.Y = target.X, target.Y
	c.X, c.Y = wp.X, wp.Y
	for i := 0; i < 5000 && target.Alive(); i++ {
		req := c2s.NewHitRequest()
		req.SetTargetId(target.ID)
		srv.handleHit(sess, req.Bytes())
	}
	if target.Alive() {
		t.Fatalf("怪物 #%d 未被击杀", target.ID)
	}
}

// expFrames 返回 C3 16（经验下发）帧。
func expFrames(rec *packetRecorder) [][]byte {
	var out [][]byte
	for _, f := range rec.frames {
		if len(f) >= 4 && f[0] == 0xC3 && frameCode(f) == 0x16 {
			out = append(out, f)
		}
	}
	return out
}

// levelUpFrames 返回 C1 F3 05（等级更新）帧。
func levelUpFrames(rec *packetRecorder) [][]byte {
	var out [][]byte
	for _, f := range rec.frames {
		if len(f) >= 4 && f[0] == 0xC1 && f[2] == 0xF3 && f[3] == 0x05 {
			out = append(out, f)
		}
	}
	return out
}

// effectFrames 返回 C1 48（光效）帧。
func effectFrames(rec *packetRecorder) [][]byte {
	var out [][]byte
	for _, f := range rec.frames {
		if len(f) >= 6 && f[0] == 0xC1 && f[2] == 0x48 {
			out = append(out, f)
		}
	}
	return out
}

// oracleBaseExperience 是原版 CalculateBaseExperience 的**独立**实现（测试用基准）。
// 刻意不复用 action.CalculateBaseExperience：被测函数算错时这里必须仍然正确。
func oracleBaseExperience(targetLevel, killerLevel float32) int64 {
	tl := float64(targetLevel)
	temp := float64(float32(tl+25)*float32(tl)) / 3.0
	if killerLevel > targetLevel+10 {
		temp *= float64(float32(tl+10) / float32(killerLevel))
	}
	if targetLevel >= 65 {
		temp += float64(float32(tl-64) * float32(tl/4))
	}
	if temp < 0 {
		temp = 0
	}
	return int64(math.Max(temp, 0) * 1.25)
}

// TestKillSendsExperiencePacket 锁定 T2-7 主路径：击杀下发一帧 C3 16 扩展形态，
// 字段按接收者视角（自己击杀 → KillerObjectId = 0x200 哨兵、DamageOfLastHit = 0），
// 且经验确实入账到角色状态。
func TestKillSendsExperiencePacket(t *testing.T) {
	srv, sess, wp, c, rec := newExpScaffold(t, 200)
	target := firstAliveMonster(t, srv, 0)

	beforeExp := c.Stats.Experience
	beforeLevel := c.Level
	killNpc(t, srv, sess, wp, c, target)

	frames := expFrames(rec)
	if len(frames) != 1 {
		t.Fatalf("200 级击杀低级怪应恰有 1 帧经验包, got %d", len(frames))
	}
	frame := frames[0]
	if len(frame) != s2c.ExperienceGainedExtendedLength {
		t.Fatalf("必须发 16B 扩展形态（9B 紧凑形态会被客户端当 16B 错位解析）, got %d", len(frame))
	}
	p := s2c.AsExperienceGainedExtended(frame)
	if p.Type() != s2c.AddResult_Normal {
		t.Fatalf("类型应为 Normal(1), got %d", p.Type())
	}
	if p.DamageOfLastHit() != 0 {
		t.Fatalf("自己击杀的 DamageOfLastHit 应为 0, got %d", p.DamageOfLastHit())
	}
	if p.KilledObjectId() != target.ID {
		t.Fatalf("KilledObjectId 应为怪物对象 ID %d, got %d", target.ID, p.KilledObjectId())
	}
	if p.KillerObjectId() != 0x0200 {
		t.Fatalf("KillerObjectId 应为哨兵 0x200, got %#x", p.KillerObjectId())
	}
	// 入账与下发一致（从状态差推期望，避免与公式互为循环论证）。
	if got, want := c.Stats.Experience-beforeExp, uint64(p.AddedExperience()); got != want {
		t.Fatalf("角色经验增量=%d, 包内经验=%d", got, want)
	}
	if p.AddedExperience() == 0 {
		t.Fatal("经验应为正数")
	}
	if c.Level != beforeLevel {
		t.Fatalf("不该升级: %d → %d", beforeLevel, c.Level)
	}
	if n := len(levelUpFrames(rec)); n != 0 {
		t.Fatalf("不升级不该发等级更新, got %d", n)
	}
	if n := len(effectFrames(rec)); n != 0 {
		t.Fatalf("不升级不该发光效, got %d", n)
	}
	// 锁死**小端**：AddedExperience 在 [4..8)，Damage 在 [8..12)，ID 在 [12..16)。
	// 本包与同码的 9B 紧凑形态（大端）字节序相反，写反会让客户端经验数字乱跳。
	got := uint32(frame[4]) | uint32(frame[5])<<8 | uint32(frame[6])<<16 | uint32(frame[7])<<24
	if got != p.AddedExperience() {
		t.Fatalf("AddedExperience 必须小端: 原始 % X → %d", frame[4:8], got)
	}
	if frame[3] != byte(s2c.AddResult_Normal) {
		t.Fatalf("Type 应落在偏移 3, got %#x", frame[3])
	}
}

// TestKillExperienceGoldenValue 锁定经验数值：用独立实现的原版公式对拍下发值。
func TestKillExperienceGoldenValue(t *testing.T) {
	srv, sess, wp, c, rec := newExpScaffold(t, 200)
	target := firstAliveMonster(t, srv, 0)

	want := oracleBaseExperience(target.Attribute("Level"), float32(c.Level))
	if want <= 0 {
		t.Fatalf("基准经验应为正: %d", want)
	}
	killNpc(t, srv, sess, wp, c, target)

	frames := expFrames(rec)
	if len(frames) != 1 {
		t.Fatalf("应恰有 1 帧, got %d", len(frames))
	}
	if got := s2c.AsExperienceGainedExtended(frames[0]).AddedExperience(); int64(got) != want {
		t.Fatalf("下发经验=%d, 独立公式基准=%d（怪物等级 %.0f, 角色 200 级）",
			got, want, target.Attribute("Level"))
	}
}

// TestKillLevelUpSendsUpdateAndEffect 锁定升级闭环：刚好差 1 点经验时击杀 →
// 两帧经验包（补齐 + 余量）+ 一帧 C1 F3 05 + 一帧 C1 48（LevelUp，自身视角 0x200），
// 且升级即把四项当前值补满（SetReclaimableAttributesToMaximum）。
func TestKillLevelUpSendsUpdateAndEffect(t *testing.T) {
	srv, sess, wp, c, rec := newExpScaffold(t, 200)
	gc := srv.deps.cfg.GameConfig
	if int(c.Level)+1 >= gc.Experience.MaximumLevel {
		t.Skipf("等级上限 %d 太低，无法构造升级场景", gc.Experience.MaximumLevel)
	}
	// 把经验调到**差 1 点**升级：升级步金额必为 1（这是可独立推导的黄金值）。
	boundary := gc.ExperienceForLevel(int(c.Level) + 1)
	if boundary <= 1 {
		t.Fatalf("经验表边界异常: %d", boundary)
	}
	c.Stats.Experience = uint64(boundary - 1)
	beforeLevel := c.Level

	target := firstAliveMonster(t, srv, 0)
	wantTotal := oracleBaseExperience(target.Attribute("Level"), float32(c.Level))
	if wantTotal < 2 {
		t.Fatalf("经验不足 2 点，无法同时验证升级与余量: %d", wantTotal)
	}
	killNpc(t, srv, sess, wp, c, target)

	frames := expFrames(rec)
	if len(frames) != 2 {
		t.Fatalf("跨级应拆成 2 帧（升级补齐 + 余量）, got %d", len(frames))
	}
	first := s2c.AsExperienceGainedExtended(frames[0])
	second := s2c.AsExperienceGainedExtended(frames[1])
	if first.AddedExperience() != 1 {
		t.Fatalf("升级帧应恰为差的 1 点, got %d", first.AddedExperience())
	}
	if second.AddedExperience() != uint32(wantTotal-1) {
		t.Fatalf("余量帧=%d, want %d", second.AddedExperience(), wantTotal-1)
	}
	// 两帧都是 Normal 类型，且都指向同一只怪。
	for i, p := range []*s2c.ExperienceGainedExtended{first, second} {
		if p.Type() != s2c.AddResult_Normal || p.KilledObjectId() != target.ID {
			t.Fatalf("第 %d 帧字段异常: type=%d killed=%d", i, p.Type(), p.KilledObjectId())
		}
	}

	ups := levelUpFrames(rec)
	if len(ups) != 1 {
		t.Fatalf("应恰有 1 帧等级更新, got %d", len(ups))
	}
	up := s2c.AsCharacterLevelUpdateExtended(ups[0])
	if len(ups[0]) != s2c.CharacterLevelUpdateExtendedLength {
		t.Fatalf("C1 F3 05 应为 32B, got %d", len(ups[0]))
	}
	if up.Level() != beforeLevel+1 {
		t.Fatalf("等级更新包等级=%d, want %d", up.Level(), beforeLevel+1)
	}
	if c.Level != beforeLevel+1 {
		t.Fatalf("角色等级=%d, want %d", c.Level, beforeLevel+1)
	}
	// 升级包里的上限必须与角色重建后的属性一致（客户端据此刷新血条上限）。
	if up.MaximumHealth() != c.Stats.MaximumHealth || up.MaximumMana() != c.Stats.MaximumMana ||
		up.MaximumShield() != c.Stats.MaximumShield || up.MaximumAbility() != c.Stats.MaximumAbility {
		t.Fatalf("上限字段与角色属性不一致: 包(%d/%d/%d/%d) 角色(%d/%d/%d/%d)",
			up.MaximumHealth(), up.MaximumMana(), up.MaximumShield(), up.MaximumAbility(),
			c.Stats.MaximumHealth, c.Stats.MaximumMana, c.Stats.MaximumShield, c.Stats.MaximumAbility)
	}
	if up.LevelUpPoints() != c.Stats.LevelUpPoints {
		t.Fatalf("升级点=%d, want %d", up.LevelUpPoints(), c.Stats.LevelUpPoints)
	}
	// SetReclaimableAttributesToMaximum：升级即补满四项。
	if c.Stats.CurrentHealth != c.Stats.MaximumHealth ||
		c.Stats.CurrentMana != c.Stats.MaximumMana ||
		c.Stats.CurrentAbility != c.Stats.MaximumAbility ||
		c.Stats.CurrentShield != c.Stats.MaximumShield {
		t.Fatalf("升级后四项当前值应补满: 血 %d/%d 法 %d/%d BP %d/%d 盾 %d/%d",
			c.Stats.CurrentHealth, c.Stats.MaximumHealth,
			c.Stats.CurrentMana, c.Stats.MaximumMana,
			c.Stats.CurrentAbility, c.Stats.MaximumAbility,
			c.Stats.CurrentShield, c.Stats.MaximumShield)
	}
	// 世界态持同一份属性引用（重建后必须同步，否则受击读旧上限）。
	if wp.Stats != c.Stats {
		t.Fatal("升级后 wp.Stats 必须指向重建后的属性")
	}

	effects := effectFrames(rec)
	if len(effects) != 1 {
		t.Fatalf("应恰有 1 帧升级光效, got %d", len(effects))
	}
	ef := s2c.AsShowEffect(effects[0])
	if ef.Effect() != s2c.EffectType2_LevelUp {
		t.Fatalf("光效类型应为 LevelUp(16), got %d", ef.Effect())
	}
	if ef.PlayerId() != 0x0200 {
		t.Fatalf("自身视角光效 PlayerId 应为 0x200, got %#x", ef.PlayerId())
	}
}

// TestLevelUpEffectBroadcastToObserver 锁定升级光效同时发给观察者，
// 且观察者看到的是**真实对象 ID**（自身 0x200 只对本人成立）。
func TestLevelUpEffectBroadcastToObserver(t *testing.T) {
	srv, sess, wp, c, rec := newExpScaffold(t, 200)
	gc := srv.deps.cfg.GameConfig
	boundary := gc.ExperienceForLevel(int(c.Level) + 1)
	c.Stats.Experience = uint64(boundary - 1)

	target := firstAliveMonster(t, srv, 0)
	// 观察者必须站在**怪物旁边**：killNpc 会把攻击者移到怪身上，光效是在那里广播的。
	obsRec := &packetRecorder{}
	obsSess, obsWP := newScopedSession(9, "observer", target.X+1, target.Y, obsRec)
	obsWP.View = obsSess.playerView
	srv.world.Map(0).Enter(obsWP)
	obsSess.setWorldPlayer(obsWP)

	killNpc(t, srv, sess, wp, c, target)

	if n := len(effectFrames(rec)); n != 1 {
		t.Fatalf("自己应收到 1 帧光效, got %d", n)
	}
	obs := effectFrames(obsRec)
	if len(obs) != 1 {
		t.Fatalf("观察者应收到 1 帧光效, got %d", len(obs))
	}
	p := s2c.AsShowEffect(obs[0])
	if p.Effect() != s2c.EffectType2_LevelUp {
		t.Fatalf("光效类型应为 LevelUp, got %d", p.Effect())
	}
	if p.PlayerId() != wp.ID {
		t.Fatalf("观察者视角 PlayerId 应为真实对象 ID %d, got %d", wp.ID, p.PlayerId())
	}
}

// maxLevelForTest 读取导出件的等级上限（用于构造"已达上限"场景）。
func maxLevelForTest(t *testing.T) int {
	t.Helper()
	cfg, err := config.LoadSeason6()
	if err != nil {
		t.Fatal(err)
	}
	return cfg.Experience.MaximumLevel
}

// TestKillAtMaxLevelSendsMaxLevelReached 锁定等级上限：仍回一帧 MaxLevelReached(0)，
// 等级与经验都不变，且不发升级包/光效。
//
// 注意脚手架必须**直接以满级构造**：会话的战斗属性（伤害上下限）在 enterWorld 时按当时
// 等级解析并缓存，中途改 c.Level 不会重算，否则伤害为 1 级值、怪永远打不死。
func TestKillAtMaxLevelSendsMaxLevelReached(t *testing.T) {
	maxLevel := maxLevelForTest(t)
	srv, sess, wp, c, rec := newExpScaffold(t, uint16(maxLevel))
	gc := srv.deps.cfg.GameConfig
	if int(c.Level) != maxLevel {
		t.Fatalf("脚手架等级=%d, want %d", c.Level, maxLevel)
	}
	c.Stats.Experience = uint64(gc.ExperienceForLevel(maxLevel))
	beforeExp := c.Stats.Experience

	target := firstAliveMonster(t, srv, 0)
	killNpc(t, srv, sess, wp, c, target)

	frames := expFrames(rec)
	if len(frames) != 1 {
		t.Fatalf("上限时应恰有 1 帧, got %d", len(frames))
	}
	p := s2c.AsExperienceGainedExtended(frames[0])
	if p.Type() != s2c.AddResult_MaxLevelReached {
		t.Fatalf("类型应为 MaxLevelReached(16), got %d", p.Type())
	}
	if p.AddedExperience() != 0 {
		t.Fatalf("上限时下发经验应为 0, got %d", p.AddedExperience())
	}
	if c.Level != uint16(maxLevel) || c.Stats.Experience != beforeExp {
		t.Fatalf("上限时不得改动等级/经验: lvl=%d exp=%d want %d/%d",
			c.Level, c.Stats.Experience, maxLevel, beforeExp)
	}
	if n := len(levelUpFrames(rec)); n != 0 {
		t.Fatalf("上限时不应发等级更新, got %d", n)
	}
	if n := len(effectFrames(rec)); n != 0 {
		t.Fatalf("上限时不应发光效, got %d", n)
	}
}

// TestSettleKillExperienceReturnsCalculatedAtMaxLevel 锁定掉落金钱的输入契约：
// 原版 AddAfterKillAsync 返回的是**计算值**（不是实际入账值），
// 因此即使已达等级上限、入账 0，掉落金钱仍按计算值给（money = exp + 7）。
func TestSettleKillExperienceReturnsCalculatedAtMaxLevel(t *testing.T) {
	maxLevel := maxLevelForTest(t)
	srv, sess, wp, c, _ := newExpScaffold(t, uint16(maxLevel))
	gc := srv.deps.cfg.GameConfig
	if int(c.Level) != maxLevel {
		t.Fatalf("脚手架等级=%d, want %d", c.Level, maxLevel)
	}
	c.Stats.Experience = uint64(gc.ExperienceForLevel(maxLevel))

	target := firstAliveMonster(t, srv, 0)
	want := oracleBaseExperience(target.Attribute("Level"), float32(c.Level))
	if want <= 0 {
		t.Fatalf("基准经验应为正: %d", want)
	}
	got := srv.settleKillExperience(sess, c, wp, target)
	if got != want {
		t.Fatalf("上限时仍应返回计算经验 %d, got %d（掉落金钱=返回值+7）", want, got)
	}
}

// TestKillMoneyDropUsesCalculatedExperience 锁定"经验 → 掉落金钱"链路：
// 地面金币额必须等于该次击杀的**计算经验 + 7**（原版 BaseMoneyDrop）。
// 若沿用旧的 gainedExperience=0，金币会恒为 7。
func TestKillMoneyDropUsesCalculatedExperience(t *testing.T) {
	srv, sess, wp, c, _ := newExpScaffold(t, 200)
	reg := srv.deps.cfg.Drops

	verified := 0
	for _, target := range srv.deps.cfg.NPCs.ByMap(0) {
		// 商人与守卫按原版不可攻击（NonPlayerCharacter / Guard 拒绝），跳过它们。
		if !target.Alive() || !target.IsAttackableByPlayer() || verified > 0 {
			continue
		}
		want := oracleBaseExperience(target.Attribute("Level"), float32(c.Level))
		seen := map[uint16]bool{}
		for _, m := range reg.MoneyOnMap(0) {
			seen[m.ID] = true
		}
		killNpc(t, srv, sess, wp, c, target)
		for _, m := range reg.MoneyOnMap(0) {
			if seen[m.ID] {
				continue
			}
			if m.Amount != uint32(want)+7 {
				t.Fatalf("金币额=%d, want 计算经验 %d + 7 = %d", m.Amount, want, want+7)
			}
			verified++
		}
	}
	if verified == 0 {
		t.Fatal("固定种子下应至少掉落一次金币（否则该断言未生效）")
	}
}

// TestSettleKillExperienceGuards 锁定防御分支：缺角色/属性/目标时不 panic 且返回 0。
func TestSettleKillExperienceGuards(t *testing.T) {
	srv, sess, wp, c, _ := newExpScaffold(t, 200)
	target := firstAliveMonster(t, srv, 0)

	if got := srv.settleKillExperience(sess, nil, wp, target); got != 0 {
		t.Fatalf("角色为 nil 应返回 0, got %d", got)
	}
	if got := srv.settleKillExperience(sess, c, wp, nil); got != 0 {
		t.Fatalf("目标为 nil 应返回 0, got %d", got)
	}
	saved := c.Stats
	c.Stats = nil
	if got := srv.settleKillExperience(sess, c, wp, target); got != 0 {
		t.Fatalf("属性为 nil 应返回 0, got %d", got)
	}
	c.Stats = saved

	// 经验为零的怪物（等级 0）不产生任何包。
	zero := &npc.Npc{ID: 0x7F00, Def: &config.Monster{Number: 99999, Name: "dummy"}}
	if got := srv.settleKillExperience(sess, c, wp, zero); got != 0 {
		t.Fatalf("0 级怪应给 0 经验, got %d", got)
	}
}
