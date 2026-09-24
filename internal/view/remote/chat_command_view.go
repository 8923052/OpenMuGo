package remote

// chat_command_view.go —— 可用聊天命令列表（TRIM-08e），对照
// GameServer/RemoteView/ChatCommandListViewPlugIn.cs:35-86（C2 F5 01 AvailableChatCommand）。
//
// 原版把这个视图与它的请求处理一起挂在 [MinimumClient(106, 3)] 上：低版本客户端
// 既不会请求也不会被发（这里按同一下限守卫）。每条命令一包，Index 从 0 递增、
// Count 是总条数（客户端据此判断收齐）；参数块 102B/个。

import (
	"mugo/internal/gamelogic/action"
	s2c "mugo/internal/proto/s2c"
	"mugo/internal/version"
)

// chatCommandListMin 是该协议族的客户端下限（对照三个插件上的 MinimumClient(106, 3)）。
var chatCommandListMin = version.AtLeast(version.ClientVersion{
	Season: 106, Episode: 3, Language: version.LanguageInvariant,
})

// ShowAvailableChatCommands 实现 action.PlayerView（F5 01，逐条下发）。
func (v *PlayerView) ShowAvailableChatCommands(commands []action.ChatCommandView) error {
	if !chatCommandListMin.Suitable(v.clientVersion) {
		return nil
	}
	count := byte(len(commands)) // 原版 (byte)commands.Count：超 255 条同样截断
	for i, cmd := range commands {
		if err := v.sendAvailableChatCommand(cmd, byte(i), count); err != nil {
			return err
		}
	}
	return nil
}

// sendAvailableChatCommand 发一条命令。参数个数按原版上限截到 255
// （注释原话：客户端装不下更多，装不下的命令本来也不可用）。
func (v *PlayerView) sendAvailableChatCommand(cmd action.ChatCommandView, index, count byte) error {
	params := cmd.Parameters
	if len(params) > 255 {
		params = params[:255]
	}
	p := s2c.NewAvailableChatCommand(s2c.AvailableChatCommandRequiredSize(len(params)))
	p.SetIndex(index)
	p.SetCount(count)
	p.SetMinimumCharacterStatus(s2c.CharacterStatus(cmd.MinimumCharacterStatus))
	p.SetParameterCount(byte(len(params)))
	p.SetCommand(cmd.Command)
	p.SetName(cmd.Name)
	p.SetDescription(cmd.Description)
	for i, param := range params {
		slot := p.Parameters(i)
		if slot == nil {
			break
		}
		slot.SetIsRequired(param.IsRequired)
		slot.SetType(chatCommandParameterType(param.TypeName))
		slot.SetName(param.Name)
		slot.SetShortName(param.ShortName)
		slot.SetValidValues(param.ValidValues)
	}
	return v.send.Send(p.Bytes())
}

// chatCommandParameterType 是原版 GetParameterType 的 CLR 类型名 → wire 枚举映射。
func chatCommandParameterType(typeName string) s2c.ChatCommandParameterType {
	switch typeName {
	case "Boolean":
		return s2c.ChatCommandParameterType_Boolean
	case "Byte", "SByte", "Int16", "UInt16", "Int32", "UInt32", "Int64", "UInt64":
		return s2c.ChatCommandParameterType_Number
	default:
		return s2c.ChatCommandParameterType_Text
	}
}
