package muhelper

import (
	"testing"
	"time"
)

func TestZenCostStageProgression(t *testing.T) {
	cfg := DefaultConfiguration() // cost [20,50,80,100,120], stageInterval 200m
	if got := ZenCost(cfg, 20, 0); got != 400 {
		t.Fatalf("stage0 应为 20*20=400, got %d", got)
	}
	if got := ZenCost(cfg, 20, 200*time.Minute); got != 1000 { // 50*20
		t.Fatalf("stage1 应为 50*20=1000, got %d", got)
	}
	if got := ZenCost(cfg, 20, 10000*time.Hour); got != 120*20 { // 末阶段钳制
		t.Fatalf("应钳到最后阶段 120*20, got %d", got)
	}
}

func TestZenCostGuards(t *testing.T) {
	cfg := DefaultConfiguration()
	cfg.CostPerStage = nil
	if ZenCost(cfg, 20, time.Hour) != 0 {
		t.Fatal("无费率应返回 0")
	}
	cfg.CostPerStage = []int{10}
	cfg.StageInterval = 0
	if ZenCost(cfg, 20, time.Hour) != 0 {
		t.Fatal("StageInterval<=0 应返回 0")
	}
}

func TestStatusFromPause(t *testing.T) {
	if StatusFromPause(true) != StatusDisabled || StatusFromPause(false) != StatusEnabled {
		t.Fatal("pause→status 映射错误")
	}
}
