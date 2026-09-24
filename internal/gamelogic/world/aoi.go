package world

// aoi.go —— 地图容器与 AoI 分桶（doc/15 §10 T1-4，对应原版
// GameLogic/BucketMap{T}.cs / Bucket{T}.cs / BucketAreaOfInterestManager.cs）。
//
// 忠实复刻的参数与语义：
//   - 桶边长 8（原版 MapInitializer.ChunkSize = 8 → BucketSideLength），32×32 桶覆盖 256×256 地图；
//   - 桶索引 = x/8 + (y/8)*侧向桶数（原版 GetListIndex）；
//   - GetInRange：先取范围内桶、再按 **切比雪夫距离**（|dx|<=range && |dy|<=range）过滤
//     （原版 LocateableExtensions.IsInRange 语义）；
//   - InfoRange = 12（原版 GameConfiguration.InfoRange = 12，GameConfigurationInitializerBase）。
//
// 与原版的差异（M7 裁剪）：原版用桶级事件订阅（IBucketMapObserver + ObservingBuckets）
// 推送进出视野；Go 侧在 Walk/Enter/Leave 时**拉取**计算进出集合——结果等价，
// 免去事件簿记。移动跨桶判定（MoveObjectOnMapAsync 的 differentBucket 语义）保留。

const (
	// BucketSideLength 是桶边长（原版 chunkSize = 8）。
	BucketSideLength = 8
	// BucketsPerRow 是每行/列的桶数（256 / 8）。
	BucketsPerRow = 256 / BucketSideLength
	// InfoRange 是视野范围（原版 GameConfiguration.InfoRange）。
	InfoRange = 12
)

// bucket 是单个桶内的定位对象集合。
type bucket struct {
	items []*Player
}

func (b *bucket) add(p *Player) { b.items = append(b.items, p) }

func (b *bucket) remove(p *Player) {
	for i, it := range b.items {
		if it == p {
			b.items = append(b.items[:i], b.items[i+1:]...)
			return
		}
	}
}

// bucketGrid 是整张地图的分桶索引（玩家在桶间的进出由调用方维护）。
type bucketGrid struct {
	buckets [BucketsPerRow * BucketsPerRow]bucket
}

func bucketIndex(x, y byte) int {
	return int(x/BucketSideLength) + int(y/BucketSideLength)*BucketsPerRow
}

func (g *bucketGrid) add(p *Player) {
	b := &g.buckets[bucketIndex(p.X, p.Y)]
	b.items = append(b.items, p)
}

func (g *bucketGrid) remove(p *Player) {
	b := &g.buckets[bucketIndex(p.X, p.Y)]
	b.remove(p)
}

// inRange 返回 (x,y) 的**桶订阅覆盖集**内的玩家。
// 原版语义（BucketMap.GetBucketsInRange + ObservingBuckets）：订阅覆盖 [pos±InfoRange]
// 的全部桶，桶内**所有**对象都进入视野——不是对每个对象再做切比雪夫过滤。
// 因此实际可见距离最小 InfoRange(12)、最大约 InfoRange+2*8-1（随在桶内位置浮动），
// 对象在屏幕边缘外就提前进入视野；此前按切比雪夫 ≤12 逐对象过滤会导致
// "走到很近才刷新 NPC"（真机事故）。
func (g *bucketGrid) inRange(x, y byte) []*Player {
	var out []*Player
	bx0, bx1, by0, by1 := coveredBuckets(x, y)
	for bx := bx0; bx <= bx1; bx++ {
		for by := by0; by <= by1; by++ {
			b := &g.buckets[bx+by*BucketsPerRow]
			out = append(out, b.items...)
		}
	}
	return out
}

// coveredBuckets 返回以 (x,y) 为中心、InfoRange=12 覆盖到的桶坐标范围
// （原版 BucketMap.GetBucketsInRange：min=Max(x-range,0)/8，max=Min(x+range,248)/8）。
func coveredBuckets(x, y byte) (bx0, bx1, by0, by1 int) {
	bx0 = (int(x) - InfoRange) / 8
	if bx0 < 0 {
		bx0 = 0
	}
	bx1 = (int(x) + InfoRange) / 8
	if bx1 > BucketsPerRow-1 {
		bx1 = BucketsPerRow - 1
	}
	by0 = (int(y) - InfoRange) / 8
	if by0 < 0 {
		by0 = 0
	}
	by1 = (int(y) + InfoRange) / 8
	if by1 > BucketsPerRow-1 {
		by1 = BucketsPerRow - 1
	}
	return bx0, bx1, by0, by1
}

// InScope 报告对象 (ox,oy) 是否在观察者 (px,py) 的桶订阅覆盖集内
// （原版 ObservingBuckets 语义；NPC/掉落物的视野判定同样用它）。
func InScope(px, py, ox, oy byte) bool {
	bx0, bx1, by0, by1 := coveredBuckets(px, py)
	bx := int(ox) / BucketSideLength
	by := int(oy) / BucketSideLength
	return bx >= bx0 && bx <= bx1 && by >= by0 && by <= by1
}
