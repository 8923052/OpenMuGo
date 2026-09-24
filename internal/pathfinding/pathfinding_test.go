package pathfinding

import "testing"

// pathfinding_test.go —— A*/预计算/序列化的行为锁定。用小张量网格（大部分空间可走、
// 局部造墙）验证：直线可达、绕墙、完全隔断无路径、SearchLimit 生效、预计算表与实时
// A* 一致、序列化精确往返。这些是"怪物能绕过墙"这一目标的正确性地基。

// openGrid 返回一张全可走（代价 1）的网格。
func openGrid() *Grid {
	g := &Grid{}
	for x := range g {
		for y := range g[x] {
			g[x][y] = 1
		}
	}
	return g
}

func walkFrom(g *Grid) [MapSize][MapSize]bool {
	var w [MapSize][MapSize]bool
	for x := range g {
		for y := range g[x] {
			w[x][y] = g[x][y]&costBitMask != 0
		}
	}
	return w
}

// setWall 把 (x,y) 设为不可达（0）。
func setWall(g *Grid, x, y byte) { g[x][y] = 0 }

func newFinder() *PathFinder {
	return NewPathFinder(NewFullGridNetwork(true), nil)
}

func TestFindPathStraightLine(t *testing.T) {
	g := openGrid()
	path := newFinder().FindPath(Point{0, 0}, Point{3, 3}, g, false)
	if path == nil {
		t.Fatal("开阔地应找到路径")
	}
	last := path[len(path)-1]
	if last.Point != (Point{3, 3}) {
		t.Fatalf("路径终点应为 (3,3)，得 %v", last.Point)
	}
	// 对角 8 邻域：0,0 → 3,3 至少 3 步。
	if len(path) < 3 {
		t.Fatalf("步数过少: %d", len(path))
	}
	// 每步都与前一点相邻（切比雪夫距离 1）。
	prev := Point{0, 0}
	for _, n := range path {
		if cheb(n.Point, prev) != 1 {
			t.Fatalf("非法步 %v（距上一点 %v 不为 1）", n.Point, prev)
		}
		prev = n.Point
	}
}

func TestFindPathAroundWall(t *testing.T) {
	g := openGrid()
	// 在 x=2 处竖一堵墙 (y=0..2)，逼迫绕行；y=3 开口。
	for y := byte(0); y <= 2; y++ {
		setWall(g, 2, y)
	}
	path := newFinder().FindPath(Point{0, 0}, Point{4, 0}, g, false)
	if path == nil {
		t.Fatal("存在绕行路径时应找到路径")
	}
	if path[len(path)-1].Point != (Point{4, 0}) {
		t.Fatalf("终点错误: %v", path[len(path)-1].Point)
	}
	// 路径不得穿过任何墙格。
	for _, n := range path {
		if g[n.Point.X][n.Point.Y]&costBitMask == 0 {
			t.Fatalf("路径穿过墙格 %v", n.Point)
		}
	}
}

func TestNoPathWhenSealed(t *testing.T) {
	g := openGrid()
	// 把起点 (1,1) 四邻全封，令其不可达任何远处目标。
	setWall(g, 0, 1)
	setWall(g, 2, 1)
	setWall(g, 1, 0)
	setWall(g, 1, 2)
	setWall(g, 0, 0)
	setWall(g, 2, 0)
	setWall(g, 0, 2)
	setWall(g, 2, 2)
	// 起点自身仍可站，但被完全隔断 → 目标不可达。
	if p := newFinder().FindPath(Point{1, 1}, Point{10, 10}, g, false); p != nil {
		t.Fatalf("被完全隔断时不应有路径，得 %d 步", len(p))
	}
}

func TestSearchLimitGivesUp(t *testing.T) {
	g := openGrid()
	f := newFinder()
	f.SearchLimit = 1 // 极小：几乎必然放弃
	if p := f.FindPath(Point{0, 0}, Point{200, 200}, g, false); p != nil {
		t.Fatal("SearchLimit=1 时远距离搜索应放弃")
	}
}

func TestSafezoneExcludedByDefault(t *testing.T) {
	g := openGrid()
	g[1][0] |= safezoneBitFlag // (1,0) 标为安全区
	// includeSafezone=false 时不得踏入安全区格。
	path := newFinder().FindPath(Point{0, 0}, Point{2, 0}, g, false)
	if path == nil {
		t.Fatal("应可从上方绕行到达 (2,0)")
	}
	for _, n := range path {
		if n.Point == (Point{1, 0}) {
			t.Fatal("includeSafezone=false 时路径不应经过安全区格")
		}
	}
}

// openBox 只在 [x0..x1]×[y0..y1] 内标记可走（其余不可达），用于把预计算限制在小区域，
// 避免全图 256×256 预计算在单测里跑成分钟级（原版靠启动期并行 + 序列化缓存复用）。
func openBox(x0, y0, x1, y1 byte) *Grid {
	g := &Grid{}
	for x := x0; x <= x1; x++ {
		for y := y0; y <= y1; y++ {
			g[x][y] = 1
		}
	}
	return g
}

func TestPreCalculatedMatchesAStar(t *testing.T) {
	g := openBox(2, 2, 14, 14) // 只在 13×13 盒内预计算
	setWall(g, 7, 7)           // 区域内一个小障碍
	walk := walkFrom(g)
	infos := PreCalculator{}.PreCalculatePaths(g, walk, 8)
	pre := NewPreCalculatedPathFinder(infos)
	real := newFinder()
	cases := [][2]Point{
		{{5, 5}, {9, 5}},
		{{5, 5}, {5, 9}},
		{{5, 5}, {8, 8}},
		{{10, 10}, {7, 11}},
	}
	for _, c := range cases {
		rp := real.FindPath(c[0], c[1], g, false)
		pp := pre.FindPath(c[0], c[1])
		if rp == nil || pp == nil {
			t.Fatalf("%v→%v: 一方无路径 real=%v pre=%v", c[0], c[1], rp != nil, pp != nil)
		}
		if len(rp) != len(pp) {
			t.Fatalf("%v→%v 步数不一致 real=%d pre=%d", c[0], c[1], len(rp), len(pp))
		}
		if pp[len(pp)-1].Point != c[1] {
			t.Fatalf("%v→%v 预计算未达终点", c[0], c[1])
		}
	}
}

func TestSerializerRoundTripNormal(t *testing.T) {
	infos := []PathInfo{
		{PointCombination{Point{10, 20}, Point{15, 22}}, Point{11, 20}},
		{PointCombination{Point{100, 100}, Point{90, 110}}, Point{99, 101}},
	}
	data := SerializePaths(infos, FormatNormal)
	got, err := DeserializePaths(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(infos) {
		t.Fatalf("数量不符 %d vs %d", len(got), len(infos))
	}
	for i := range infos {
		if got[i] != infos[i] {
			t.Fatalf("第 %d 条不符: %+v vs %+v", i, got[i], infos[i])
		}
	}
}

func TestSerializerRoundTripCompact(t *testing.T) {
	// 差值须在 ±8 内。
	infos := []PathInfo{
		{PointCombination{Point{10, 20}, Point{15, 22}}, Point{11, 20}},
		{PointCombination{Point{100, 100}, Point{95, 103}}, Point{99, 101}},
	}
	data, err := SerializePathsCompact(infos)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DeserializePaths(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(infos) {
		t.Fatalf("数量不符 %d vs %d", len(got), len(infos))
	}
	for i := range infos {
		if got[i] != infos[i] {
			t.Fatalf("第 %d 条不符: %+v vs %+v", i, got[i], infos[i])
		}
	}
}

func TestCompactRejectsOutOfRange(t *testing.T) {
	infos := []PathInfo{
		{PointCombination{Point{10, 20}, Point{40, 22}}, Point{11, 20}},
	}
	if _, err := SerializePathsCompact(infos); err != ErrCompactRange {
		t.Fatalf("超范围差值应报 ErrCompactRange，得 %v", err)
	}
}

func cheb(a, b Point) int {
	dx := int(a.X) - int(b.X)
	dy := int(a.Y) - int(b.Y)
	if dx < 0 {
		dx = -dx
	}
	if dy < 0 {
		dy = -dy
	}
	return max(dx, dy)
}
