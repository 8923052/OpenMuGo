// frustum.go —— 移植原版 GameLogic/PlayerActions/Skills/FrustumBasedTargetFilter.cs。
//
// 扇形/锥形区域技能的命中判定：以**施法者位置**为顶点，沿 rotation（0..255 → 0..360°）
// 朝向展开一个梯形——近端半宽 StartWidth、远端半宽 EndWidth、长度 Distance。
// 只有落在梯形内的目标才受击，所以背后的怪打不到（这正是 Go 侧之前丢失的方向语义）。
//
// 数学与原版逐条对齐（含 rotation→角度的 /255 与相对位置里的 /256 两处口径不一致——
// 原版如此，保持忠实，不做"修正"）：
//   - CalculateRotationVectors / GetFrustum：梯形四角 = 旋转矩阵·基准向量 + 施法者坐标（取整）。
//   - IsWithinFrustum：四边叉积的"点在凸多边形内"判定。
package action

import "math"

// distanceEpsilon 对齐原版 FrustumBasedTargetFilter.DistanceEpsilon。
const distanceEpsilon = 0.001

// FrustumFilter 是一个扇形命中过滤器（原版 FrustumBasedTargetFilter）。不可变，可并发共享。
type FrustumFilter struct {
	startWidth, endWidth, distance float64
	projectileCount                int
}

// NewFrustumFilter 构造过滤器；projectileCount<1 归一为 1（单弹道=整体扇形）。
func NewFrustumFilter(startWidth, endWidth, distance float64, projectileCount int) *FrustumFilter {
	if projectileCount < 1 {
		projectileCount = 1
	}
	return &FrustumFilter{startWidth: startWidth, endWidth: endWidth, distance: distance, projectileCount: projectileCount}
}

// corners 计算 rotation 下梯形四角的绝对坐标（顺序与原版一致：远左、远右、近右、近左）。
func (f *FrustumFilter) corners(casterX, casterY int, rotation byte) (x, y [4]float64) {
	// 原版 CalculateRotationVectors：degrees = rotation*360/byte.MaxValue + 180。
	deg := float64(rotation)*360.0/255.0 + 180.0
	rad := deg * math.Pi / 180.0
	cos, sin := math.Cos(rad), math.Sin(rad)
	// 基准向量（局部系：X 右、Y 前）；z 分量恒 0。distanceOffset=0.99 让近端总在角色前方。
	const nearOffset = 0.99
	temp := [4][2]float64{
		{-f.endWidth, f.distance},
		{f.endWidth, f.distance},
		{f.startWidth, nearOffset},
		{-f.startWidth, nearOffset},
	}
	for i, v := range temp {
		// VectorRotate = R(deg)·v： (vx·cos − vy·sin, vx·sin + vy·cos)。
		rx := v[0]*cos - v[1]*sin
		ry := v[0]*sin + v[1]*cos
		// 原版 GetFrustum：(int) 截断后加施法者坐标。
		x[i] = float64(int(rx)) + float64(casterX)
		y[i] = float64(int(ry)) + float64(casterY)
	}
	return x, y
}

// WithinBounds 判定目标 (tx,ty) 是否落在施法者朝向的扇形内（原版 IsTargetWithinBounds 单发版）。
func (f *FrustumFilter) WithinBounds(casterX, casterY int, rotation byte, tx, ty int) bool {
	if casterX == tx && casterY == ty {
		return true // 与原版一致：同点恒可命中
	}
	x, y := f.corners(casterX, casterY, rotation)
	// 原版 IsWithinFrustum：四条边叉积同号 → 在凸四边形内。
	outOfRange := ((x[0]-float64(tx))*(y[3]-float64(ty))-(x[3]-float64(tx))*(y[0]-float64(ty))) < 0.0 ||
		((x[1]-float64(tx))*(y[0]-float64(ty))-(x[0]-float64(tx))*(y[1]-float64(ty))) < 0.0 ||
		((x[2]-float64(tx))*(y[1]-float64(ty))-(x[1]-float64(tx))*(y[2]-float64(ty))) < 0.0 ||
		((x[3]-float64(tx))*(y[2]-float64(ty))-(x[2]-float64(tx))*(y[3]-float64(ty))) < 0.0
	return !outOfRange
}

// WithinBoundsProjectile 判定目标是否被第 projectileIndex 条弹道命中（多弹道扇形，
// 原版 IsTargetWithinBounds(attacker,target,rotation,projectileIndex,extraProjectiles)）。
// S6 基础数据 projectileCount 均为 1，此处直接回落单发判定。
func (f *FrustumFilter) WithinBoundsProjectile(casterX, casterY int, rotation byte, tx, ty, projectileIndex, extraProjectiles int) bool {
	if f.projectileCount <= 1 {
		return f.WithinBounds(casterX, casterY, rotation, tx, ty)
	}
	total := f.projectileCount + extraProjectiles
	if projectileIndex < 0 || projectileIndex >= total {
		return false
	}
	if !f.WithinBounds(casterX, casterY, rotation, tx, ty) {
		return false
	}
	rel := f.relativePosition(casterX, casterY, tx, ty, rotation)
	sectionWidth := 2.0 / float64(total)
	start := -1.0 + float64(projectileIndex)*sectionWidth
	end := start + sectionWidth
	ov := f.overlap(casterX, casterY, tx, ty, extraProjectiles)
	start -= ov
	end += ov
	return rel >= start && rel <= end
}

// relativePosition 计算目标在扇形横向的归一化位置 [-1,1]（-1 左沿、0 中心、1 右沿）。
func (f *FrustumFilter) relativePosition(casterX, casterY, tx, ty int, rotation byte) float64 {
	dx := float64(tx - casterX)
	dy := float64(ty - casterY)
	// 原版 CalculateRelativePositionInFrustum：这里角度用 /256.0（与 corners 的 /255 不同，忠实保留）。
	angle := (float64(rotation) * 360.0 / 256.0) + 180.0
	rad := angle * math.Pi / 180.0
	cos := math.Cos(-rad)
	sin := math.Sin(-rad)
	rx := dx*cos - dy*sin
	ry := dx*sin + dy*cos
	if ry <= 0 {
		return 0 // 目标在身后或过近
	}
	ratio := ry / f.distance
	if ratio > 1 {
		ratio = 1
	}
	width := f.startWidth + (f.endWidth-f.startWidth)*ratio
	if math.Abs(width) < distanceEpsilon {
		return 0
	}
	n := rx / width
	if n < -1 {
		return -1
	}
	if n > 1 {
		return 1
	}
	return n
}

// overlap 计算相邻弹道分段的容差重叠（距离越远重叠越小）。
func (f *FrustumFilter) overlap(casterX, casterY, tx, ty, extra int) float64 {
	dist := math.Hypot(float64(tx-casterX), float64(ty-casterY))
	if dist == 0 {
		return 1
	}
	ov := (1.0 / math.Floor(dist)) / float64(f.projectileCount+extra)
	return ov + 0.001
}
