package api

// SpecialServerIds 对应 Interfaces/SpecialServerIds.cs。
// 0..0xFFFF 留给游戏服，故 CS/CS 用高位区间。
const (
	SpecialServerIdConnectServer = 0x10000
	SpecialServerIdChatServer    = 0x20000
)
