package player

// messages.go —— 系统提示文本目录，对应 OpenMU
// GameLogic/PlayerMessageExtensions.cs + GameLogic/Properties/PlayerMessage.resx。
// 键名与中立英文文案逐字照抄 resx；差异（无卫星集、只有中立英文）登记在 doc/16 TRIM-11。

import (
	"fmt"
	"strconv"
	"strings"
)

// MessageKey 是原版 PlayerMessage.resx 的条目名。
type MessageKey string

const (
	// MsgUsingThisItemNotImplemented：未登记的消耗品（ItemConsumeAction 无策略分支）。
	MsgUsingThisItemNotImplemented MessageKey = "UsingThisItemNotImplemented"
	MsgItemUnknown                 MessageKey = "ItemUnknown"
	MsgInventoryFull               MessageKey = "InventoryFull"
	MsgNotEnoughMoney              MessageKey = "NotEnoughMoney"
	// MsgTalkingNotImplementedFormat：{0}=NPC 编号，{1}=NPC designation（TalkNpcAction）。
	MsgTalkingNotImplementedFormat MessageKey = "TalkingNotImplementedFormat"
	MsgNotEnoughLevelUpPoints      MessageKey = "NotEnoughLevelUpPointsAvailable"
	MsgAttributeNotAvailable       MessageKey = "AttributeNotAvailable"
	MsgNoItemToRepair              MessageKey = "NoItemToRepair"
	MsgNotEnoughMoneyToRepair      MessageKey = "NotEnoughMoneyToRepair"
	// MsgNotEnoughMoneyToProceed：接任务时起始费不够（QuestStartAction.cs:66）。
	MsgNotEnoughMoneyToProceed MessageKey = "NotEnoughMoneyToProceed"
	// MsgUnknownWarpIndex：原版不传参，占位符原样到达客户端（照抄，见 doc/16）。
	MsgUnknownWarpIndex   MessageKey = "UnknownWarpIndex"
	MsgLevelTooLowToEnter MessageKey = "LevelTooLowToEnterMap"
	// MsgVaultIsLocked：仓库上锁时从仓库取物（MoveItemAction.cs:241-244）。
	MsgVaultIsLocked MessageKey = "TheVaultIsLocked"
	// 宝石升档合成/降档拆分的四条蓝字（ItemStackAction.cs:63,99,35,44）。
	MsgLackingJewels        MessageKey = "YouLackOfJewels"
	MsgStackedJewelNotFound MessageKey = "StackedJewelNotFound"
	MsgNotStackedJewel      MessageKey = "SelectedItemIsNotStackedJewel"
	MsgNoInventorySpace     MessageKey = "InventoryNotEnoughSpace"
	// MsgLevelUpCongrats：{0}=新等级（UpdateLevelPlugIn 在等级包之后发）。
	MsgLevelUpCongrats MessageKey = "LevelUpCongrats"
	// MsgMasterLevelUpCongrats：{0}=新大师等级（UpdateLevelPlugIn.UpdateMasterLevelAsync，
	// 同在 F3 51 之后发）。
	MsgMasterLevelUpCongrats MessageKey = "MasterLevelUpCongrats"
	// MU Helper 的四道门（MuHelper.TryStartAsync；另两条 MuHelperIsDisabled /
	// MuHelperCantHandleStatus 在本仓模型下不可达，见 doc/16 TRIM-10）。
	MsgMuHelperAlreadyRunning MessageKey = "MuHelperAlreadyRunning"
	MsgMuHelperMinimumLevel   MessageKey = "MuHelperMinimumLevel"
	MsgMuHelperMaximumLevel   MessageKey = "MuHelperMaximumLevel"
	MsgMuHelperRequiresMoney  MessageKey = "MuHelperRequiresMoney"
	// TRIM-08 聊天命令族（CommandExtensions / HelpCommand / ChatCommandPlugInBase / Online 命令）。
	MsgCommandDoesNotExist MessageKey = "CommandDoesNotExist"
	// MsgCommandArgumentCount：{0}=必填参数数，{1}=实际给的个数。
	MsgCommandArgumentCount MessageKey = "CommandExtensionsInvalidArgumentCount"
	// MsgCommandArgumentType：{0}=参数名，{1}=期望的类型名。
	MsgCommandArgumentType MessageKey = "CommandExtensionsArgumentInvalidType"
	// MsgCommandRequiredArgumentMissing：{0}=未被赋值的必填参数名。
	MsgCommandRequiredArgumentMissing MessageKey = "CommandExtensions_RequiredArgumentMissing"
	// MsgUnknownAttribute：{0}=命令里写的属性缩写（/add 的 StatType）。
	MsgUnknownAttribute MessageKey = "UnknownAttribute"
	// MsgCharacterHasNoStatAttribute：{0}=属性缩写（该职业没有这条 StatAttribute）。
	MsgCharacterHasNoStatAttribute MessageKey = "CharacterHasNoStatAttribute"
	// MsgOnlineCountInfo：{0}=命令串，{1}=在线 GM 数，{2}=在线普通角色数。
	MsgOnlineCountInfo MessageKey = "OnlineCountInfo"
	// MsgMissingMapRequirement 是进图属性需求未满足（TRIM-11c，
	// GameMapDefinitionExtensions.TryGetRequirementError）：{0}=属性定义的 Description。
	MsgMissingMapRequirement MessageKey = "MissingMapRequirement"
)

// playerMessages 是 resx 的中立英文条目（缺键按原版返回空串处理）。
var playerMessages = map[MessageKey]string{
	MsgUsingThisItemNotImplemented:    "Using this item is not implemented.",
	MsgItemUnknown:                    "Item Unknown",
	MsgInventoryFull:                  "Inventory is full",
	MsgNotEnoughMoney:                 "You don't have enough Money",
	MsgTalkingNotImplementedFormat:    "Talking to this NPC ({0}, {1}) is not implemented yet.",
	MsgNotEnoughLevelUpPoints:         "Not enough level up points available.",
	MsgAttributeNotAvailable:          "Attribute not available.",
	MsgNoItemToRepair:                 "No item there to repair.",
	MsgNotEnoughMoneyToRepair:         "You don't have enough money to repair.",
	MsgNotEnoughMoneyToProceed:        "Not enough money to proceed",
	MsgUnknownWarpIndex:               "Unknown warp index {0}",
	MsgLevelTooLowToEnter:             "Your level is too low to enter this map.",
	MsgVaultIsLocked:                  "The vault is locked.",
	MsgLackingJewels:                  "You are lacking of Jewels.",
	MsgStackedJewelNotFound:           "Stacked Jewel not found.",
	MsgNotStackedJewel:                "Selected Item is not a stacked Jewel.",
	MsgNoInventorySpace:               "Inventory got not enough space.",
	MsgLevelUpCongrats:                "Congratulations, you are Level {0} now.",
	MsgMasterLevelUpCongrats:          "Congratulations, you are Master Level {0} now.",
	MsgMuHelperAlreadyRunning:         "MU Helper is already running.",
	MsgMuHelperMinimumLevel:           "MU Helper can be used after level {0}.",
	MsgMuHelperMaximumLevel:           "MU Helper cannot be used after level {0}.",
	MsgMuHelperRequiresMoney:          "MU Helper requires {0} zen.",
	MsgCommandDoesNotExist:            "The command '{0}' does not exist.",
	MsgCommandArgumentCount:           "The command needs {0} arguments and was given {1}.",
	MsgCommandArgumentType:            "The argument {0} was given a invalid type, it expects the value to be of the type {1}.",
	MsgCommandRequiredArgumentMissing: "The required argument named {0} was not used.",
	MsgUnknownAttribute:               "Unknown attribute: '{0}'.",
	MsgCharacterHasNoStatAttribute:    "The character has no stat attribute '{0}'.",
	MsgOnlineCountInfo:                "[{0}] {1} GM(s) and {2} player(s) online",
	MsgMissingMapRequirement:          "Missing requirement to enter the map: {0}",
}

// LocalizedMessage 取条目文本，有参数时按 {0}/{1}… 代入（原版只在参数非空时 string.Format）。
// 原版按 player.Culture 查卫星程序集，查不到回退中立英文；本仓只有中立英文，等价于回退分支。
func LocalizedMessage(key MessageKey, args ...any) string {
	text, ok := playerMessages[key]
	if !ok {
		return ""
	}
	for i, arg := range args {
		text = strings.ReplaceAll(text, "{"+strconv.Itoa(i)+"}", fmt.Sprint(arg))
	}
	return text
}
