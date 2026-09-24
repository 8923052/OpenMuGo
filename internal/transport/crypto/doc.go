// Package crypto 实现 MU 传输层加密：C1-C4 帧的 SimpleModulus、Xor32，以及登录字段的 Xor3。
//
// 加密策略按客户端版本选择，对应 OpenMU Network/PlugIns 下的加密工厂插件
// （以 ClientVersion 为 Key；选版与两套插件容器见 doc/11-multi-version-adaptation.md）：
//
//	(0,75,Invariant) Version075NetworkEncryptionFactoryPlugIn      → 待实现
//	default（无赛季，0.95d 等）PreSeason6NetworkEncryptionFactory   → 待实现
//	(6,3,English)    Season6Episode3NetworkEncryptionFactory       → NewS6E3ServerCodec
//	(106,3,English)  OpenSourceClientNetworkEncryptionFactory      → NewS6E3ServerCodec
//
// 已核对原版源码：Season6Episode3 与 OpenSourceClient 两个工厂的实现**逐行相同**，仅 Key 不同，
// 因此一个 codec 同时覆盖 GMO 1.04d 与开源客户端 2.04d；0.75 与 pre-Season6 是不同实现，需各自补 Codec。
package crypto
