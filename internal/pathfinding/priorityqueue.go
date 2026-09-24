package pathfinding

// priorityqueue.go —— 二叉最小堆，对照 BinaryMinHeap{T}.cs + IPriorityQueue{T}.cs +
// NodeComparer.cs。原版注释直言这是 A* 的性能瓶颈，故手写堆而非用 container/heap
// （行为与原版 Push/Pop 逐条一致：上浮 sift-up / 下沉 sift-down，比较器为
// PredictedTotalCost 之差）。**非并发安全**——同一 finder 一次只允许一次搜索。

// PriorityQueue 是按优先级取最小者的队列（原版 IPriorityQueue<T>）。
type PriorityQueue interface {
	Push(n *Node)
	Pop() *Node
	Len() int
	Clear()
}

// BinaryMinHeap 是 *Node 的二叉最小堆，按 PredictedTotalCost 排序（原版 BinaryMinHeap<Node>）。
type BinaryMinHeap struct {
	inner []*Node
}

// NewBinaryMinHeap 构造空堆。
func NewBinaryMinHeap() *BinaryMinHeap { return &BinaryMinHeap{} }

// Len 返回元素数。
func (h *BinaryMinHeap) Len() int { return len(h.inner) }

// Clear 清空堆（原版 Clear；每次 FindPath 前 Reset 调用）。
func (h *BinaryMinHeap) Clear() { h.inner = h.inner[:0] }

// Push 入堆并上浮（原版 Push）。
func (h *BinaryMinHeap) Push(n *Node) {
	h.inner = append(h.inner, n)
	index := len(h.inner) - 1
	for index > 0 {
		parent := (index - 1) >> 1
		if h.compare(index, parent) >= 0 {
			break
		}
		h.swap(index, parent)
		index = parent
	}
}

// Pop 弹出最小者并下沉（原版 Pop）。调用方须保证非空（先判 Len）。
func (h *BinaryMinHeap) Pop() *Node {
	if len(h.inner) == 0 {
		return nil
	}
	result := h.inner[0]
	last := len(h.inner) - 1
	h.inner[0] = h.inner[last]
	h.inner = h.inner[:last]
	index := 0
	for {
		smallest := index
		left := index<<1 + 1
		right := index<<1 + 2
		if left < len(h.inner) && h.compare(index, left) > 0 {
			index = left
		}
		if right < len(h.inner) && h.compare(index, right) > 0 {
			index = right
		}
		if index == smallest {
			break
		}
		h.swap(smallest, index)
	}
	return result
}

func (h *BinaryMinHeap) swap(i, j int) { h.inner[i], h.inner[j] = h.inner[j], h.inner[i] }

// compare 复刻 NodeComparer.Compare：a.PredictedTotalCost - b.PredictedTotalCost。
func (h *BinaryMinHeap) compare(i, j int) int {
	return h.inner[i].PredictedTotalCost - h.inner[j].PredictedTotalCost
}
