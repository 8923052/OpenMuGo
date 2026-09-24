package action

import (
	"math"
	"testing"
)

// axisPoints 按与 corners 相同的角度口径(deg = rot*360/255 + 180)，给出沿前向、
// 反向各 8 格的格点（前向单位向量 = R(deg)·(0,1) = (-sin, cos)）。
func axisPoints(cx, cy int, rot byte) (fx, fy, bx, by int) {
	rad := (float64(rot)*360.0/255.0 + 180.0) * math.Pi / 180.0

	dx := int(math.Round(-math.Sin(rad) * 8))
	dy := int(math.Round(math.Cos(rad) * 8))
	return cx + dx, cy + dy, cx - dx, cy - dy
}

// TestFrustumDirection 验证扇形过滤器只命中施法者朝向一侧、**不命中背后**
// ——这正是 OpenMUGo 之前丢失、被本修复补回的核心语义（天雷闪等扇形武器技能）。
//
// rotation=0 → 角度 180° → 局部前向 +Y 旋转后指向世界 -Y（地图上方）。
func TestFrustumDirection(t *testing.T) {
	f := NewFrustumFilter(1, 6, 20, 1) // 近端半宽 1、远端半宽 6、长度 20
	const cx, cy = 100, 100
	front := f.WithinBounds(cx, cy, 0, 100, 85)   // 正前方 15 格（朝向内）
	behind := f.WithinBounds(cx, cy, 0, 100, 115) // 正后方 15 格
	if !front {
		t.Fatalf("正前方目标应命中")
	}
	if behind {
		t.Fatalf("背后目标不应命中（扇形只覆盖朝向一侧）")
	}
}

// TestFrustumBeyondDistance：超出梯形长度的目标不命中。
func TestFrustumBeyondDistance(t *testing.T) {
	f := NewFrustumFilter(1, 2, 5, 1)
	if f.WithinBounds(100, 100, 0, 100, 90) { // 前方 10 格 > 距离 5
		t.Fatalf("超过 frustum 距离的目标不应命中")
	}
}

// TestFrustumLateralWidth：朝向内、横向在半宽内的目标命中；横向过宽不命中。
func TestFrustumLateralWidth(t *testing.T) {
	f := NewFrustumFilter(1, 6, 20, 1)
	// 前方 15 格，横向偏移在远端半宽(≈6)内 → 命中
	if !f.WithinBounds(100, 100, 0, 103, 85) {
		t.Fatalf("朝向内、半宽内的目标应命中")
	}
	// 横向偏移过大 → 不命中
	if f.WithinBounds(100, 100, 0, 140, 85) {
		t.Fatalf("横向超出半宽的目标不应命中")
	}
}

// TestFrustumSamePoint：目标与施法者同点恒命中（原版 IsTargetWithinBounds 短路）。
func TestFrustumSamePoint(t *testing.T) {
	f := NewFrustumFilter(1, 1, 3, 1)
	if !f.WithinBounds(50, 50, 0, 50, 50) {
		t.Fatalf("同点应恒命中")
	}
}

// TestFrustumAllRotationsNeverBehind：任意朝向下，"正后方"都不命中——覆盖 256 个方向字节。
func TestFrustumAllRotationsNeverBehind(t *testing.T) {
	f := NewFrustumFilter(1.5, 1.5, 12, 1)
	const cx, cy = 128, 128
	for rot := 0; rot <= 255; rot++ {
		// 取一个"前方"点：沿 forward 方向 8 格。forward=(-sin, cos)@deg。
		frontX, frontY, backX, backY := axisPoints(cx, cy, byte(rot))
		if !f.WithinBounds(cx, cy, byte(rot), frontX, frontY) {
			t.Fatalf("rot=%d 前方点(%d,%d)应命中", rot, frontX, frontY)
		}
		if f.WithinBounds(cx, cy, byte(rot), backX, backY) {
			t.Fatalf("rot=%d 背后点(%d,%d)不应命中", rot, backX, backY)
		}
	}
}
