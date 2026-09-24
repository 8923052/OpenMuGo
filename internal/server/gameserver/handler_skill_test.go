package gameserver

// handler_skill_test.go —— S7 攻击技能目标选择：ExplicitWithImplicitInRange(target=6) 溅射
// 主目标附近活怪（对照 DetermineTargets），Explicit 只打主目标，range≤0 回落单目标。

import (
	"testing"

	"mugo/internal/gamelogic/action"
)

func TestDetermineSkillTargetsSplash(t *testing.T) {
	srv, _, wp, _, _ := newExpScaffold(t, 100)
	primary := firstAliveMonster(t, srv, 0)

	alive := 0
	for _, n := range srv.deps.cfg.NPCs.ByMap(0) {
		// 计数口径与目标筛选一致：商人/守卫/雕像等非 IAttackable 不参与溅射。
		if n.Alive() && n.IsAttackableByPlayer() {
			alive++
		}
	}

	// Explicit(1)：只主目标。
	got := srv.determineSkillTargets(wp, primary, &action.SkillDef{Target: action.SkillTargetExplicit, ImplicitTargetRange: 255})
	if len(got) != 1 || got[0] != primary {
		t.Fatalf("Explicit 应只命中主目标, got %d", len(got))
	}

	// target=6 但 ImplicitTargetRange≤0：回落单目标。
	got = srv.determineSkillTargets(wp, primary, &action.SkillDef{Target: action.SkillTargetExplicitWithImplicit, ImplicitTargetRange: 0})
	if len(got) != 1 {
		t.Fatalf("range0 应只主目标, got %d", len(got))
	}

	// target=6 大半径：主目标 + 其余全部活怪（证明溅射聚合附近目标）。
	got = srv.determineSkillTargets(wp, primary, &action.SkillDef{Target: action.SkillTargetExplicitWithImplicit, ImplicitTargetRange: 255})
	if len(got) != alive {
		t.Fatalf("大半径 splash 应命中全部 %d 只活怪, got %d", alive, len(got))
	}
	sawPrimary := false
	for _, n := range got {
		if n == primary {
			sawPrimary = true
		}
	}
	if !sawPrimary {
		t.Fatal("splash 结果应包含主目标")
	}
}
