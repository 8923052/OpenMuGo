package pet

import "testing"

func TestBehaviourFromCommandMode(t *testing.T) {
	for m := byte(0); m <= 3; m++ {
		b, ok := BehaviourFromCommandMode(m)
		if !ok {
			t.Fatalf("命令 %d 应映射为有效行为", m)
		}
		if b.CommandMode() != m {
			t.Fatalf("行为↔命令往返不一致: mode=%d → %d", m, b.CommandMode())
		}
	}
	if _, ok := BehaviourFromCommandMode(4); ok {
		t.Fatal("命令 4 应判为无效")
	}
}

func TestPetTypeByte(t *testing.T) {
	if PetTypeByte(false) != 0 {
		t.Fatal("渡鸦类型字节应为 0")
	}
	if PetTypeByte(true) != 1 {
		t.Fatal("马类型字节应为 1")
	}
}

func TestAttackTypeByte(t *testing.T) {
	if AttackSingleTarget.SkillTypeByte() != 0 || AttackRange.SkillTypeByte() != 1 {
		t.Fatal("攻击类型字节应为 0/1")
	}
}
