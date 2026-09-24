package pathfinding

import "math"

// pathfinder.go —— A* 主体，逐条对照 PathFinder.cs。
// **非线程安全**：同一实例同时只允许一次 FindPath（原版同注明）。怪物/GS 每端点各持一个。

// PathFinder 在二维网格上搜索路径（原版 PathFinder）。
type PathFinder struct {
	network           Network
	openList          PriorityQueue
	maximumDistance   int
	maximumDistanceSq int

	// SearchLimit 是允许展开的节点上限，超过即放弃（原版默认 500）。
	SearchLimit int
	// HeuristicEstimate 是启发倍率，写入 Heuristic（原版默认 2）。
	HeuristicEstimate int
	// Heuristic 决定 H（原版默认 NoHeuristic）。
	Heuristic Heuristic
}

// NewPathFinder 用给定网络与开表构造（原版双构造函数的合并；queue 传 nil 用默认二叉堆）。
func NewPathFinder(network Network, queue PriorityQueue) *PathFinder {
	if queue == nil {
		queue = NewBinaryMinHeap()
	}
	return &PathFinder{
		network:           network,
		openList:          queue,
		SearchLimit:       500,
		HeuristicEstimate: 2,
		Heuristic:         &NoHeuristic{},
	}
}

// SetMaximumDistance 设置最大搜索距离（0 关闭；原版 MaximumDistance，同步缓存平方）。
func (p *PathFinder) SetMaximumDistance(d int) {
	p.maximumDistance = d
	p.maximumDistanceSq = d * d
}

// ResetPathFinder 清空开表（原版 ResetPathFinder；怪物每次搜索前调用）。
func (p *PathFinder) ResetPathFinder() { p.openList.Clear() }

// FindPath 搜索 start→end 的路径（原版 FindPath；返回从 start 迈向 end 的逐步骤，
// 找不到返回 nil）。terrain 为 AIgrid。
func (p *PathFinder) FindPath(start, end Point, terrain *Grid, includeSafezone bool) []PathResultNode {
	if p.maximumDistanceExceededPair(start, end) {
		return nil
	}

	p.openList.Clear()
	if !p.network.Prepare(start, end, terrain, includeSafezone) {
		return nil
	}
	p.Heuristic.SetMultiplier(p.HeuristicEstimate)

	startNode := p.network.GetNodeAt(start)
	if startNode == nil {
		return nil
	}
	startNode.PredictedTotalCost = 2
	startNode.PreviousNode = startNode // 自环作为起点终止标记
	startNode.Status = StatusOpen
	p.openList.Push(startNode)

	closeNodeCounter := 0
	found := false
	for p.openList.Len() > 0 {
		node := p.openList.Pop()
		if node.Status == StatusClosed {
			continue
		}
		if node.X() == end.X && node.Y() == end.Y {
			node.Status = StatusClosed
			found = true
			break
		}
		if closeNodeCounter > p.SearchLimit {
			return nil
		}
		p.expandNodes(node, start, end)
		node.Status = StatusClosed
		closeNodeCounter++
	}

	if found {
		return p.getCalculatedPath(end)
	}
	return nil
}

func (p *PathFinder) expandNodes(node *Node, start, end Point) {
	for _, newNode := range p.network.GetPossibleNextNodes(node) {
		if p.maximumDistanceExceededNode(start, end, newNode) {
			continue
		}
		h := p.Heuristic.CalculateHeuristicDistance(newNode.Position, end)
		newNode.PredictedTotalCost = newNode.CostUntilNow + h
		newNode.Status = StatusOpen
		newNode.PreviousNode = node
		p.openList.Push(newNode)
	}
}

// getCalculatedPath 从 end 沿 PreviousNode 回溯到 start，再反转（原版 GetCalculatedPath）。
func (p *PathFinder) getCalculatedPath(end Point) []PathResultNode {
	var path []PathResultNode
	node := p.network.GetNodeAt(end)
	if node == nil {
		return nil
	}
	for node.PreviousNode != node {
		path = append(path, PathResultNode{Point: node.Position, PreviousPoint: node.PreviousNode.Position})
		node = node.PreviousNode
	}
	// 反转
	for i, j := 0, len(path)-1; i < j; i, j = i+1, j-1 {
		path[i], path[j] = path[j], path[i]
	}
	return path
}

func (p *PathFinder) maximumDistanceExceededPair(start, end Point) bool {
	if p.maximumDistance == 0 {
		return false
	}
	return start.EuclideanDistanceSquaredTo(end) > p.maximumDistanceSq
}

// maximumDistanceExceededNode 复刻原版：比较 (a+b)^2 与 max^2，用平方距离避免多次开方，
// 但仍对 start→node、node→end 各做一次开方求绕行总长以判定超距。
func (p *PathFinder) maximumDistanceExceededNode(start, end Point, node *Node) bool {
	if p.maximumDistance == 0 {
		return false
	}
	startToNode := start.EuclideanDistanceSquaredTo(node.Position)
	if startToNode > p.maximumDistanceSq {
		return true
	}
	nodeToEnd := node.Position.EuclideanDistanceSquaredTo(end)
	if nodeToEnd > p.maximumDistanceSq {
		return true
	}
	detour := math.Sqrt(float64(startToNode)) + math.Sqrt(float64(nodeToEnd))
	return detour > float64(p.maximumDistance)
}
