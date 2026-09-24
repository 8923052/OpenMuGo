package pathfinding

// heuristic.go —— 启发式，对照 IHeuristic.cs / NoHeuristic.cs / ManhattanHeuristic.cs /
// EuclideanHeuristic.cs / MaximumDistanceOfXorYHeuristic.cs。原版启发式带一个
// HeuristicEstimateMultiplier（PathFinder 每次搜索前写入 HeuristicEstimate，默认 2）。

// Heuristic 估算从 location 到 target 的启发距离 H（原版 IHeuristic）。
type Heuristic interface {
	// SetMultiplier 设置启发估计倍率（原版 HeuristicEstimateMultiplier 的 set）。
	SetMultiplier(m int)
	// CalculateHeuristicDistance 返回 H 值。
	CalculateHeuristicDistance(location, target Point) int
}

// NoHeuristic 恒返回 0（等价 Dijkstra；原版 PathFinder 的默认启发式）。
type NoHeuristic struct{ multiplier int }

// SetMultiplier 无效果（保留以对齐接口）。
func (NoHeuristic) SetMultiplier(int) {}

// CalculateHeuristicDistance 恒为 0。
func (NoHeuristic) CalculateHeuristicDistance(Point, Point) int { return 0 }

// ManhattanHeuristic 取坐标差绝对值之和 × 倍率。
type ManhattanHeuristic struct{ multiplier int }

// SetMultiplier 设置倍率。
func (h *ManhattanHeuristic) SetMultiplier(m int) { h.multiplier = m }

// CalculateHeuristicDistance 返回 manhattan 距离。
func (h *ManhattanHeuristic) CalculateHeuristicDistance(location, target Point) int {
	return h.multiplier * (abs(int(location.X)-int(target.X)) + abs(int(location.Y)-int(target.Y)))
}

// EuclideanHeuristic 取欧氏距离 × 倍率。
type EuclideanHeuristic struct{ multiplier int }

// SetMultiplier 设置倍率。
func (h *EuclideanHeuristic) SetMultiplier(m int) { h.multiplier = m }

// CalculateHeuristicDistance 返回欧氏距离（取整）。
func (h *EuclideanHeuristic) CalculateHeuristicDistance(location, target Point) int {
	return int(float64(h.multiplier) * location.EuclideanDistanceTo(target))
}

// MaxDistanceOfXorYHeuristic 取 x/y 差绝对值的较大者 × 倍率。
type MaxDistanceOfXorYHeuristic struct{ multiplier int }

// SetMultiplier 设置倍率。
func (h *MaxDistanceOfXorYHeuristic) SetMultiplier(m int) { h.multiplier = m }

// CalculateHeuristicDistance 返回 max(|dx|,|dy|)。
func (h *MaxDistanceOfXorYHeuristic) CalculateHeuristicDistance(location, target Point) int {
	return h.multiplier * max(abs(int(location.X)-int(target.X)), abs(int(location.Y)-int(target.Y)))
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
