// Package pathfinding 是 OpenMU `src/Pathfinding` 的 Go 侧落位。
//
// 原版 33 个 .cs，分两部分：
//
//	A* 本体：PathFinder / IPathFinder / INetwork / FullGridNetwork / ScopedGridNetwork /
//	         BaseGridNetwork / Node / NodeIndexer / NodeComparer / IPriorityQueue{T} /
//	         BinaryMinHeap{T} / IndexedLinkedList{T} / IIndexer{T} / IHeuristic /
//	         ManhattanHeuristic / EuclideanHeuristic / MaximumDistanceOfXorYHeuristic /
//	         NoHeuristic / Point / PathResultNode。
//
//	预计算路径（PreCalculation/）：PreCalculator / PreCalculatedPathFinder / PathInfo /
//	         CompactPathsSerializer / NormalPathsSerializer / PathsSerializeExtensions /
//	         PointCombination。原版在启动时把地图可行走点两两组合的最短路预先算好并序列化，
//	         运行时怪物走位直接查表，不再实时搜路。
//
// 依赖关系：gamelogic/NPC/*Intelligence（怪物 AI）与 gamelogic/Bots/BotNavigator 都要用它，
// 因此本包是 `NPC`、`Bots` 两个域的前置。它本身只依赖地图的可行走格信息（GameMapTerrain），
// 不依赖 proto / version，属于纯算法包。
//
// 实现状态（T1-f）：A* 本体（Point/Node/PathFinder/FullGridNetwork/堆/启发式）、
// PreCalculator + PreCalculatedPathFinder + Compact/Normal 序列化均已落地并单测锁定。
// 怪物追击遇障经 gamelogic/world 的 AIgrid + server 层 worldAIView.NextStep 走实时 A*
// 绕行（原版 Monster.WalkToAsync 等价）；预计算表 + 序列化为可选能力（供守卫/机器人
// 与离线复用，全图预计算代价高，不在默认启动路径执行）。落地顺序见 doc/10 §5 的 T1。
package pathfinding
