package world

import (
	"testing"
	"time"

	"mugo/internal/gamelogic/entity/item"
)

// TestDropRegistryLifecycle 锁定 T1-6：放置→拾取→超时清理。
func TestDropRegistryLifecycle(t *testing.T) {
	r := NewDropRegistry()
	now := time.Now()

	id := r.AddItem(0, 140, 130, &item.Item{Group: 0, Number: 3}, now)
	// 掉落物 ID 段 0..0x1FF（原版 dropIdGenerator 从 0 起），首个 ID = 0。
	if id != 0 {
		t.Fatalf("首个掉落物 ID=0（原版语义）, got %d", id)
	}
	taken, ok := r.TakeItem(id)
	if !ok || taken.It.Group != 0 {
		t.Fatalf("拾取失败: %v", ok)
	}
	// 已拾取的不可再拾。
	if _, ok := r.TakeItem(id); ok {
		t.Fatal("重复拾取应失败")
	}
}

// TestDropMoneyAndExpiry 金币放置与超时清理。
func TestDropMoneyAndExpiry(t *testing.T) {
	r := NewDropRegistry()
	now := time.Now()

	mid := r.AddMoney(0, 140, 130, 107, now)
	taken, ok := r.TakeMoney(mid)
	if !ok || taken.Amount != 107 {
		t.Fatalf("拾取金币失败: %v", ok)
	}

	// 超时清理：只在清早于 cutoff 的。
	r2 := NewDropRegistry()
	old := r2.AddItem(0, 140, 130, &item.Item{}, now.Add(-2*time.Minute))
	fresh := r2.AddMoney(0, 140, 130, 5, now)
	expired := r2.ExpireBefore(now)
	if len(expired) != 1 || expired[0].ID != old {
		t.Fatalf("应清理 1 个过期物: %v", expired)
	}
	if _, ok := r2.TakeItem(old); ok {
		t.Fatal("过期物应已被清理")
	}
	if _, ok := r2.TakeMoney(fresh); !ok {
		t.Fatal("未过期物应保留")
	}
}
