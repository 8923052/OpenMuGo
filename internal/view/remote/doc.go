// Package remote 是 view 的协议实现层，对应 OpenMU 的 GameServer/RemoteView：
// 把视图事件序列化成具体的 S2C 封包并通过连接下发。
//
// 这是版本差异最集中的一层。原版按"一个功能多个版本实现 + MinimumClient 标注 + 运行时选生效插件"
// 组织（ViewPlugInContainer），Go 端沿用同一组织方式，命名也沿用原版后缀：
//
//	<功能>.go          通用/较新版本实现      ← ObjectMovedPlugIn.cs
//	<功能>_075.go      0.75 变体              ← ObjectMovedPlugIn075.cs
//	<功能>_095.go      0.95d 变体             ← NewNpcsInScopePlugIn095.cs
//	<功能>_097.go      0.97d 变体             ← UpdateCharacterStatsPlugIn097.cs
//	<功能>_extended.go 106.3+ 扩展版          ← ItemSerializerExtended.cs
//
// 每个实现暴露一个 version.Constraint（对应 MinimumClient/MaximumClient 标注），
// 由 view 的注册表按连接当前版本选出"下限最紧的适用实现"（原版 ViewPlugInContainer.DetermineEffectivePlugIn）。
//
// 受影响的功能矩阵（原版 RemoteView 下约 30 个变体文件）：物品/外观序列化、角色列表、
// 角色属性与经验更新、视野进出（玩家/NPC）、移动与瞬移、攻击与技能动画、掉落与金钱、
// 魔法效果取消、传送与换图、公会/组队列表、任务状态、决斗、商店与信件。
// 完整清单与版本区间见 doc/11-multi-version-adaptation.md。
//
// 本包与 gamelogic **反向依赖**：gamelogic 只定义领域模型（entity/*），序列化一律在本包，
// 因为"同一实体在不同客户端版本下有不同字节形态"——把编码留在 gamelogic 就等于把版本判断
// 引进了版本无关层。
package remote
