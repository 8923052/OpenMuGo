package remote

// npc_window.go —— NPC 对话窗口的"配置枚举 → 线协议号"映射与开窗包。
// 对应 OpenMU `GameServer/RemoteView/NPC/OpenNpcWindowPlugIn.cs`：
//   :31-44 分支 —— NpcDialog 走 C3 F9 01 OpenNpcDialog，其余走 C3 30 NpcWindowResponse；
//   :46-79 Convert —— DataModel 的 NpcWindow（MonsterDefinition.cs:17-178 的隐式序号）到
//          线协议 NpcWindow（ServerToClientPackets.cs:10641-10762）的**逐项映射**。
//
// 为什么放在视图层：DataModel 枚举序号 ≠ 线协议号（Merchant 1→0、VaultStorage 4→2、
// ElphisRefinery 11→17、SeedMaster 17→23…），这张表就是协议知识（铁律：编码差异只在 view/remote）。
// 原版对下表的"无对应"分支是 `throw ArgumentException`；Go 侧以 ErrNoNpcWindow 表达，
// 由编排层记日志——**不是**改发别的包。

import (
	"errors"

	s2c "mugo/internal/proto/s2c"
)

// DataModel NpcWindow 枚举值（MonsterDefinition.cs:17-178，隐式序号，0 起）。
const (
	NpcWindowUndefined = 0
	NpcWindowMerchant  = 1
	NpcWindowStorage   = 3
	// NpcWindowVaultStorage 是仓库窗（原版号 4）。
	NpcWindowVaultStorage = 4
	// NpcWindowChaosMachine 是混沌之锅窗（原版号 5）。
	NpcWindowChaosMachine = 5
	// NpcWindowRemoveJohOption 与混沌锅同一条 talk 分支（原版号 13）。
	NpcWindowRemoveJohOption = 13
	// NpcWindowLahap 是宝石合成师 Lahap 的窗口（原版号 9）；C1 BC 的前置。
	NpcWindowLahap            = 9
	NpcWindowGuildMaster      = 25
	NpcWindowNpcDialog        = 27
	NpcWindowLegacyQuest      = 28
	NpcWindowCastleSiegeGate  = 29
	NpcWindowCastleSiegeLever = 30
)

// ErrNoNpcWindow 表示该配置窗口在线协议里没有对应值（原版 Convert 的 throw 分支）。
var ErrNoNpcWindow = errors.New("remote: 该 NpcWindow 无线协议对应值")

// npcWindowWire 是 OpenNpcWindowPlugIn.Convert 的完整映射表（24 项）。
// 未列入的号即原版的 throw 分支：Undefined、Storage、GuildMaster、NpcDialog、
// LegacyQuest、CastleSiegeGate/Lever。
var npcWindowWire = map[int]s2c.NpcWindow{
	NpcWindowMerchant:        s2c.NpcWindow_Merchant,                      // 1 → 0
	2:                        s2c.NpcWindow_Merchant1,                     // 2 → 1
	NpcWindowVaultStorage:    s2c.NpcWindow_VaultStorage,                  // 4 → 2
	NpcWindowChaosMachine:    s2c.NpcWindow_ChaosMachine,                  // 5 → 3
	6:                        s2c.NpcWindow_DevilSquare,                   // 6 → 4
	7:                        s2c.NpcWindow_BloodCastle,                   // 7 → 6
	8:                        s2c.NpcWindow_PetTrainer,                    // 8 → 7
	NpcWindowLahap:           s2c.NpcWindow_Lahap,                         // 9 → 9
	10:                       s2c.NpcWindow_CastleSeniorNPC,               // 10 → 12
	11:                       s2c.NpcWindow_ElphisRefinery,                // 11 → 17
	12:                       s2c.NpcWindow_RefineStoneMaking,             // 12 → 18
	NpcWindowRemoveJohOption: s2c.NpcWindow_RemoveJohOption,               // 13 → 19
	14:                       s2c.NpcWindow_IllusionTemple,                // 14 → 20
	15:                       s2c.NpcWindow_ChaosCardCombination,          // 15 → 21
	16:                       s2c.NpcWindow_CherryBlossomBranchesAssembly, // 16 → 22
	17:                       s2c.NpcWindow_SeedMaster,                    // 17 → 23
	18:                       s2c.NpcWindow_SeedResearcher,                // 18 → 24
	19:                       s2c.NpcWindow_StatReInitializer,             // 19 → 25
	20:                       s2c.NpcWindow_DelgadoLuckyCoinRegistration,  // 20 → 32
	21:                       s2c.NpcWindow_DoorkeeperTitusDuelWatch,      // 21 → 33
	22:                       s2c.NpcWindow_LugardDoppelgangerEntry,       // 22 → 35
	23:                       s2c.NpcWindow_JerintGaionEvententry,         // 23 → 36
	24:                       s2c.NpcWindow_JuliaWarpMarketServer,         // 24 → 37
	26:                       s2c.NpcWindow_CombineLuckyItem,              // 26 → 38
}

// HasNpcWindowResponse 报告该配置窗口能否用 C3 30 开窗（编排层据此区分"有窗但本层不发"
// 的 NpcDialog/LegacyQuest/GuildMaster 与真正无对应的窗口）。
func HasNpcWindowResponse(configWindow int) bool {
	_, ok := npcWindowWire[configWindow]
	return ok
}

// ShowNpcWindow 下发 C3 30 NpcWindowResponse；窗口无协议对应值时返回 ErrNoNpcWindow 且不发包。
func (v *PlayerView) ShowNpcWindow(configWindow int) error {
	wire, ok := npcWindowWire[configWindow]
	if !ok {
		return ErrNoNpcWindow
	}
	p := s2c.NewNpcWindowResponse()
	p.SetWindow(wire)
	return v.send.Send(p.Bytes())
}

// ShowNpcDialog 下发 C3 F9 01 OpenNpcDialog（NpcWindow.NpcDialog 专用）。
// 原版把 NPC 定义号写进去、公勋点恒 0（OpenNpcWindowPlugIn.cs:37）。
func (v *PlayerView) ShowNpcDialog(npcNumber uint16) error {
	p := s2c.NewOpenNpcDialog()
	p.SetNpcNumber(npcNumber)
	p.SetGensContributionPoints(0)
	return v.send.Send(p.Bytes())
}
