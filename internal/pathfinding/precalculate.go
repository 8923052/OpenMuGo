package pathfinding

// precalculate.go —— 预计算路径，对照 PreCalculation/ 的 PreCalculator.cs /
// PreCalculatedPathFinder.cs / PathInfo.cs / PointCombination.cs。
//
// 设计：启动期把"每个可行走点 → 其 maximumRange 邻域内每个点"的最短路首步预表化，
// 运行时怪物走位 O(1) 查表，不再实时搜路（原版为省大量怪物时的 A* CPU）。
//
// 与原版差异登记：原版 PreCalculator.FindPaths 传入 FindPath 的 start/end 参数顺序
// 与 PreCalculatedPathFinder 的查表方向相反（疑似遗留 bug），Go 侧按**明确语义**实现
// ——表键 (start,end) 的值恒为"从 start 朝 end 的第一步"，与实时 A* 结果一致；
// 这样 PreCalculatedPathFinder 与 PathFinder 对同一地图给出同样的路径。

// PointCombination 是"起点 + 终点"的组合，作查表键（原版同名，4 字节可比较结构）。
type PointCombination struct {
	Start Point
	End   Point
}

// PathInfo 是一条预表项：从 Combination.Start 前往 Combination.End 的第一步 NextStep。
type PathInfo struct {
	Combination PointCombination
	NextStep    Point
}

// PreCalculator 预计算一张地图的所有路径首步（原版 PreCalculator）。
type PreCalculator struct{}

// PreCalculatePaths 对 aiGrid 上每个可行走点，计算其到 maximumRange 邻域内每个可达点的
// 最短路首步，返回全部表项。walkMap[x][y] 标记该点是否作为**起点**参与预计算
// （原版传入的 bool 网格；这里起点与终点都用 walkMap 过滤，与 aiGrid 的可达性一致）。
func (PreCalculator) PreCalculatePaths(aiGrid *Grid, walkMap [MapSize][MapSize]bool, maximumRange int) []PathInfo {
	network := NewFullGridNetwork(true)
	finder := NewPathFinder(network, nil)
	var result []PathInfo
	for x := 0; x < MapSize; x++ {
		for y := 0; y < MapSize; y++ {
			if !walkMap[x][y] {
				continue
			}
			start := Point{X: byte(x), Y: byte(y)}
			result = append(result, finder.findPathsFrom(start, walkMap, aiGrid, maximumRange)...)
		}
	}
	return result
}

// findPathsFrom 计算从 start 出发、maximumRange 邻域内所有可达点的首步。
func (p *PathFinder) findPathsFrom(start Point, walkMap [MapSize][MapSize]bool, aiGrid *Grid, maximumRange int) []PathInfo {
	toX := min(start.X+byte(maximumRange)-1, 0xFF)
	toY := min(start.Y+byte(maximumRange)-1, 0xFF)
	fromX := max(int(start.X)-maximumRange, 0)
	fromY := max(int(start.Y)-maximumRange, 0)
	var out []PathInfo
	for x := fromX; x <= int(toX); x++ {
		for y := fromY; y <= int(toY); y++ {
			if !walkMap[x][y] || (x == int(start.X) && y == int(start.Y)) {
				continue
			}
			end := Point{X: byte(x), Y: byte(y)}
			// 首步 = 从 start 朝 end 迈出的第一步（低 7 位代价网格，不含安全区）。
			nodes := p.FindPath(start, end, aiGrid, false)
			if len(nodes) > 0 {
				out = append(out, PathInfo{
					Combination: PointCombination{Start: start, End: end},
					NextStep:    nodes[0].Point,
				})
			}
		}
	}
	return out
}

// PreCalculatedPathFinder 用预表回答路径（原版 PreCalculatedPathFinder）。
// 建表后只读，可多 goroutine 并发查询。
type PreCalculatedPathFinder struct {
	nextSteps map[PointCombination]Point
}

// NewPreCalculatedPathFinder 从表项建索引。
func NewPreCalculatedPathFinder(infos []PathInfo) *PreCalculatedPathFinder {
	m := make(map[PointCombination]Point, len(infos))
	for _, info := range infos {
		m[info.Combination] = info.NextStep
	}
	return &PreCalculatedPathFinder{nextSteps: m}
}

// NextStep 返回从 start 朝 end 迈出的下一步（无表项时 ok=false）。
func (f *PreCalculatedPathFinder) NextStep(start, end Point) (Point, bool) {
	p, ok := f.nextSteps[PointCombination{Start: start, End: end}]
	return p, ok
}

// FindPath 沿表拼装完整路径（原版 FindPath）。表覆盖不到（步数超过建表 range 或
// 中途缺项）时返回 nil。步数上限防表数据异常导致死循环。
func (f *PreCalculatedPathFinder) FindPath(start, end Point) []PathResultNode {
	var result []PathResultNode
	cur := start
	for {
		next, ok := f.nextSteps[PointCombination{Start: cur, End: end}]
		if !ok {
			break
		}
		result = append(result, PathResultNode{Point: next, PreviousPoint: cur})
		cur = next
		if cur == end || len(result) > 2*MapSize {
			break
		}
	}
	if len(result) == 0 || cur != end {
		return nil
	}
	return result
}

func min(a, b byte) byte {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
