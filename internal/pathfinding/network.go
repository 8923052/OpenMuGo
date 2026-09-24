package pathfinding

// network.go —— 网格网络，对照 INetwork.cs / BaseGridNetwork.cs / FullGridNetwork.cs。
// AIgrid 每格是一个字节：低 7 位为"踏入该格的代价"（0=不可达），最高位 0x80 为安全区旗标
// （原版 UpdateAiGridValue：AIgrid[x,y] = (walkable?1:0) | (safezone?0x80:0)）。

// Grid 是一张地图的 AI 代价网格（原版 byte[,] AIgrid，索引 [x][y]）。
type Grid [MapSize][MapSize]byte

const (
	safezoneBitFlag = 0b1000_0000
	costBitMask     = 0b0111_1111
)

// 方向偏移（原版 DirectionOffsetsX/Y）。前 4 个为正交，后 4 个为对角；
// allowDiagonals=false 时只用前 4 个。
var (
	directionOffsetsX = [8]int{0, 1, 0, -1, 1, 1, -1, -1}
	directionOffsetsY = [8]int{-1, 0, 1, 0, -1, 1, 1, -1}
)

// Network 是 PathFinder 操作的网络（原版 INetwork）。
type Network interface {
	// Prepare 为一次新搜索重置网络状态并绑定网格；返回是否可继续搜索。
	Prepare(start, end Point, grid *Grid, includeSafezone bool) bool
	// GetNodeAt 返回指定坐标的节点（无则 nil）。
	GetNodeAt(p Point) *Node
	// GetPossibleNextNodes 返回从 node 可达的后续节点（已按代价/状态过滤）。
	GetPossibleNextNodes(node *Node) []*Node
}

// baseGridNetwork 是网格网络基类（原版 BaseGridNetwork）。
type baseGridNetwork struct {
	numberOfDirections int
	gridWidth          int
	gridHeight         int
	grid               *Grid
	includeSafezone    bool
}

func newBaseGridNetwork(allowDiagonals bool) baseGridNetwork {
	n := 4
	if allowDiagonals {
		n = 8
	}
	return baseGridNetwork{numberOfDirections: n}
}

// prepare 绑定网格并记录边界（原版 BaseGridNetwork.Prepare；Go 里网格恒为 MapSize×MapSize）。
func (b *baseGridNetwork) prepare(grid *Grid, includeSafezone bool) bool {
	b.grid = grid
	b.gridWidth = MapSize
	b.gridHeight = MapSize
	b.includeSafezone = includeSafezone
	return true
}

func (b *baseGridNetwork) isWithinBounds(x, y byte) bool {
	return int(x) < b.gridWidth && int(y) < b.gridHeight
}

// getPossibleNextNodes 复刻 BaseGridNetwork.GetPossibleNextNodes 的邻居筛选与代价更新。
// getAt 由具体网络提供节点取用（FullGridNetwork 惰性建节点）。
func (b *baseGridNetwork) getPossibleNextNodes(node *Node, getAt func(Point) *Node) []*Node {
	if b.grid == nil {
		return nil
	}
	var out []*Node
	for i := 0; i < b.numberOfDirections; i++ {
		cx := int(node.X()) + directionOffsetsX[i]
		cy := int(node.Y()) + directionOffsetsY[i]
		if cx < 0 || cx > 255 || cy < 0 || cy > 255 {
			continue
		}
		nx, ny := byte(cx), byte(cy)
		if !b.isWithinBounds(nx, ny) {
			continue
		}
		gridValue := b.grid[nx][ny]
		if !b.includeSafezone && gridValue&safezoneBitFlag > 0 {
			continue
		}
		costToNode := int(gridValue & costBitMask)
		if costToNode == 0 { // UnreachableGridNodeValue
			continue
		}
		newNode := getAt(Point{X: nx, Y: ny})
		if newNode == nil || newNode.Status == StatusClosed {
			continue
		}
		newG := node.CostUntilNow + costToNode
		if newNode.Status == StatusOpen && newNode.CostUntilNow <= newG {
			continue // 已有更优或等价路径，跳过
		}
		newNode.CostUntilNow = newG
		out = append(out, newNode)
	}
	return out
}

// FullGridNetwork 覆盖整张网格的网络（原版 FullGridNetwork：0x10000 个槽位惰性建节点）。
type FullGridNetwork struct {
	baseGridNetwork
	nodes [MapSize * MapSize]*Node
}

// NewFullGridNetwork 构造（allowDiagonals 决定是否可走对角）。
func NewFullGridNetwork(allowDiagonals bool) *FullGridNetwork {
	return &FullGridNetwork{baseGridNetwork: newBaseGridNetwork(allowDiagonals)}
}

// Prepare 复位全部已建节点状态后绑定网格（原版 FullGridNetwork.Prepare 先清状态）。
func (f *FullGridNetwork) Prepare(start, end Point, grid *Grid, includeSafezone bool) bool {
	for i := range f.nodes {
		if f.nodes[i] != nil {
			f.nodes[i].Status = StatusUndefined
		}
	}
	return f.baseGridNetwork.prepare(grid, includeSafezone)
}

// GetNodeAt 返回（必要时惰性创建）指定坐标的节点。
func (f *FullGridNetwork) GetNodeAt(p Point) *Node {
	idx := int(p.Y)<<8 + int(p.X)
	node := f.nodes[idx]
	if node == nil {
		node = &Node{Position: p}
		f.nodes[idx] = node
	}
	return node
}

// GetPossibleNextNodes 返回后续节点。
func (f *FullGridNetwork) GetPossibleNextNodes(node *Node) []*Node {
	return f.getPossibleNextNodes(node, f.GetNodeAt)
}

var _ Network = (*FullGridNetwork)(nil)
