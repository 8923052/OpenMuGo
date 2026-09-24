// Package attribute 是 OpenMU `src/AttributeSystem` 的 Go 侧落位。
//
// 原版 21 个 .cs：AttributeSystem / IAttributeSystem / IElement / IAttribute /
// IComposableAttribute / BaseAttribute / BaseStatAttribute / ConstValueAttribute /
// StatAttribute / ComposableAttribute / SimpleElement / ConstantElement /
// CombinedElement / AttributeDefinition / AttributeRelationship / AttributeRelationshipElement /
// AttributeRelationshipExtensions / AttributeSystemExtensions / AggregateType / Extensions。
//
// 它是全项目的地基之一，被 GameLogic / GameServer / Persistence / DataModel / Web 五处依赖：
// 等级、职业、装备、buff、果实、组队加成最终都归结为"属性 = 若干 element 的聚合"。
//
// 为什么必须独立成包（而不是塞在 gamelogic 下）：Go 的包边界即依赖边界，属性系统被
// view（序列化角色属性）、persistence（从配置构造）、server 同时需要；放在 gamelogic 下
// 会让 gamelogic 反向成为这些层的上游，破坏分层。
//
// 与 gamelogic/player 的分工：player 只做"从配置+装备装配出属性"，不再自己算公式；
// 公式（线性/二次/条件聚合）全部在本包。
//
// 落地顺序见 doc/10 的 T0-a；移植时逐文件照搬，不要合并类。
package attribute
