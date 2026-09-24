package pathfinding

import "math"

// core.go —— A* 的数据骨架，逐字对照 OpenMU `src/Pathfinding` 的 Point.cs / Node.cs /
// PathResultNode.cs。本包是**纯算法叶子包**：不 import 任何 internal 包（对齐原版零游戏依赖），
// 由 world（提供 AIgrid）与 server/gamelogic 消费。

// MapSize 是地图边长（原版 GameMapTerrain.MapSize = 256；AIgrid 为 256×256）。
const MapSize = 256

// Point 是地图上一个坐标（原版 record struct Point(byte X, byte Y)）。
type Point struct {
	X byte
	Y byte
}

// EuclideanDistanceSquaredTo 返回到另一点的欧氏距离平方（避免开方；原版同名）。
func (p Point) EuclideanDistanceSquaredTo(o Point) int {
	dx := int(p.X) - int(o.X)
	dy := int(p.Y) - int(o.Y)
	return dx*dx + dy*dy
}

// EuclideanDistanceTo 返回到另一点的欧氏距离。
func (p Point) EuclideanDistanceTo(o Point) float64 {
	return math.Sqrt(float64(p.EuclideanDistanceSquaredTo(o)))
}

// PathResultNode 是路径结果中的一步：Point 为本步到达的坐标，
// PreviousPoint 为上一步坐标（原版 record struct PathResultNode(Point, PreviousPoint)）。
type PathResultNode struct {
	Point         Point
	PreviousPoint Point
}

// NodeStatus 是节点在开/闭表中的状态（原版 enum NodeStatus : byte）。
type NodeStatus byte

const (
	// StatusUndefined 未定义（尚未被本次搜索访问）。
	StatusUndefined NodeStatus = iota
	// StatusOpen 在 open 表（已发现、待定）。
	StatusOpen
	// StatusClosed 在 closed 表（已展开）。
	StatusClosed
)

// Node 是路径网络中的一个节点（原版 Node）。
type Node struct {
	// PredictedTotalCost 是预计总代价 F = G + H。
	PredictedTotalCost int
	// CostUntilNow 是到本节点的实际代价 G。
	CostUntilNow int
	// Position 是本节点坐标。
	Position Point
	// PreviousNode 是回溯用的前驱节点。
	PreviousNode *Node
	// Status 是开/闭表状态。
	Status NodeStatus
}

// X 返回本节点 x 坐标（原版 `byte X => Position.X`）。
func (n *Node) X() byte { return n.Position.X }

// Y 返回本节点 y 坐标。
func (n *Node) Y() byte { return n.Position.Y }
