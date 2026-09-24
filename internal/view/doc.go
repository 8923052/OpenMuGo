// Package view 是出站轴，对应 OpenMU GameLogic/Views 的 IViewPlugIn：
// 把"玩家身上发生了什么"抽象成事件，让业务逻辑在不知道协议的前提下通知客户端。
//
// 与 gamelogic 的关系：业务逻辑只调用本包的接口，因此 gamelogic **版本无关**——
// 原版 GameLogic 下没有任何 ClientVersion 判断，版本差异全部落在 view/remote 与 gameserver。
// 具体的封包序列化与按版本选择实现在 view/remote；View 接口随 action 层一起落地。
package view
