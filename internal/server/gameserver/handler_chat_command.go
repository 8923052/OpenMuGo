package gameserver

// handler_chat_command.go —— 聊天命令分派与可达命令（TRIM-08d），对照
// ChatMessageCommandProcessor.cs:17-30 + 各 IChatCommandPlugIn 实现。
//
// 分派的三条原版口径：命令串未注册**或插件未激活**都走 `GetStrategy` 落空 → 静默返回
// （没有任何提示）；状态不足 → 只写日志；参数解析的失败消息见 action.ParseChatArguments。
//
// 本仓落地的命令（有元数据且效果可诚实实现）：/help、/list、/add、/goldnotice、/online。
// 其余（43 条已激活命令里依赖传送/怪物/事件启动/信件/红名/仓库/洗点的）在这里**只记日志**，
// 不造假实现；默认停用的 22 条（/addstr 一族、/get* 与 /set* 一族等）按原版根本不可解析。

import (
	"strings"

	"mugo/internal/gamelogic/action"
	"mugo/internal/gamelogic/config"
	"mugo/internal/gamelogic/entity"
	"mugo/internal/gamelogic/player"
)

// chatCommandKeyword 取命令的第一个空格前 token（原版 `content.Message.Split(' ').First()`）。
func chatCommandKeyword(message string) string {
	if i := strings.Index(message, " "); i >= 0 {
		return message[:i]
	}
	return message
}

// handleChatCommandListRequest 处理 C1 F5 00（ChatCommandListRequestHandlerPlugIn.cs:27-38）：
// 把"该角色可用的命令"（已激活 + 状态达标，见 config.AvailableChatCommands）整表交给视图。
// 原版命令元数据来自反射插件容器；本仓查 97_chat_commands.json 的导出表（口径见 doc/16）。
func (s *Server) handleChatCommandListRequest(sess *session) {
	gc := s.deps.cfg.GameConfig
	c := sess.getSelected()
	if gc == nil || c == nil { // 原版：SelectedCharacter 为 null 直接返回
		return
	}
	available := gc.AvailableChatCommands(byte(c.Status))
	entries := make([]action.ChatCommandView, 0, len(available))
	for _, cmd := range available {
		view := action.ChatCommandView{
			Command:                cmd.Command,
			Name:                   cmd.Name,
			Description:            cmd.Description,
			MinimumCharacterStatus: cmd.MinimumCharacterStatus,
		}
		for _, p := range cmd.Parameters {
			view.Parameters = append(view.Parameters, action.ChatCommandParameterView{
				Name: p.Name, ShortName: p.ShortName, IsRequired: p.IsRequired,
				TypeName: p.TypeName, ValidValues: p.ValidValues,
			})
		}
		entries = append(entries, view)
	}
	if view := s.viewFor(sess); view != nil {
		_ = view.ShowAvailableChatCommands(entries)
	}
}

// handleChatCommand 分派一条 '/' 命令（message 含命令词本身）。
func (s *Server) handleChatCommand(sess *session, c *entity.Character, message string) {
	gc := s.deps.cfg.GameConfig
	if gc == nil || c == nil {
		return
	}
	key := chatCommandKeyword(message)
	cmd, ok := gc.ChatCommandByCommand(key)
	if !ok || !cmd.EnabledByDefault {
		return // 原版 GetStrategy 落空：无提示、无日志级别差异
	}
	if byte(c.Status) < cmd.MinimumCharacterStatus {
		s.deps.logger.Printf("gameserver: %s is trying to execute %s command without meeting the requirements",
			c.Name, key)
		return
	}

	args := action.ParseChatArguments(cmd, message)
	if args.TooFewArguments {
		// 原版在逐个转换**之前**就 return null。
		s.showLocalizedMessage(sess, player.MsgCommandArgumentCount, args.RequiredCount, args.GivenCount)
		return
	}
	for _, f := range args.TypeFailures {
		s.showLocalizedMessage(sess, player.MsgCommandArgumentType, f.Parameter, f.TypeName)
	}
	for _, missing := range args.MissingRequired {
		s.showLocalizedMessage(sess, player.MsgCommandRequiredArgumentMissing, missing)
	}
	if !args.Runnable {
		return
	}

	switch key {
	case "/help":
		s.chatCommandHelp(sess, gc, c, args.String("CommandName"))
	case "/list":
		for _, avail := range gc.AvailableChatCommands(byte(c.Status)) {
			s.showBlueMessage(sess, avail.Usage)
		}
	case "/add":
		s.chatCommandAddStat(sess, gc, c, args)
	case "/goldnotice":
		s.chatCommandGoldNotice(message, key)
	case "/online":
		s.chatCommandOnline(sess, key)
	default:
		s.deps.logger.Printf("gameserver: 聊天命令 %s 未实现效果（%s）", key, c.Name)
	}
}

// chatCommandHelp 回某条命令的用法（对照 HelpCommand.cs:37-46：只在"该角色可用的命令"里找，
// 匹配 "/"+名字且大小写不敏感；查无 → CommandDoesNotExist）。
func (s *Server) chatCommandHelp(sess *session, gc *config.GameConfig, c *entity.Character, name string) {
	if name == "" {
		s.showLocalizedMessage(sess, player.MsgCommandDoesNotExist, name)
		return
	}
	wanted := strings.ToLower(name)
	for _, avail := range gc.AvailableChatCommands(byte(c.Status)) {
		if strings.EqualFold(avail.Command, "/"+wanted) {
			s.showBlueMessage(sess, avail.Usage)
			return
		}
	}
	s.showLocalizedMessage(sess, player.MsgCommandDoesNotExist, name)
}

// chatStatDesignations 是 /add 的属性缩写 → 原版 Stats.Base* designation
// （对照 ChatCommandPlugInBase.TryGetAttributeAsync:212-220 的 switch）。
var chatStatDesignations = []struct {
	alias       string
	designation string
	stat        action.StatType
}{
	{"str", "Base Strength", action.StatStrength},
	{"agi", "Base Agility", action.StatAgility},
	{"vit", "Base Vitality", action.StatVitality},
	{"ene", "Base Energy", action.StatEnergy},
	{"cmd", "Base Leadership", action.StatLeadership},
}

// chatCommandAddStat 实现 /add <str|agi|vit|ene|cmd> <amount>
// （对照 AddStatChatCommandPlugIn：属性缩写未知 → UnknownAttribute；该职业没有这条
// StatAttribute → CharacterHasNoStatAttribute；随后走与点击加点同一条 IncreaseStats 链路。
// CurrentMiniGame 的"小游戏中不许批量加点"分支随小游戏域）。
func (s *Server) chatCommandAddStat(sess *session, gc *config.GameConfig, c *entity.Character, args action.ChatArgs) {
	alias := args.String("StatType")
	var (
		found       bool
		designation string
		stat        action.StatType
	)
	for _, m := range chatStatDesignations {
		if m.alias == alias {
			found, designation, stat = true, m.designation, m.stat
			break
		}
	}
	if !found {
		s.showLocalizedMessage(sess, player.MsgUnknownAttribute, alias)
		return
	}
	if !classHasStatAttribute(gc, c, designation) {
		s.showLocalizedMessage(sess, player.MsgCharacterHasNoStatAttribute, alias)
		return
	}
	// Amount 解析失败时为 0（原版仍执行命令）→ IncreaseStat 拒绝且无蓝字，与基类 catch 同形。
	s.allocateStatPoints(sess, c, stat, args.Uint16("Amount"))
}

// classHasStatAttribute 判该职业是否声明了这条可加点属性（原版看 SelectedCharacter.Attributes
// 里有没有该 Definition；本仓角色属性由职业表实例化，故查职业表）。
func classHasStatAttribute(gc *config.GameConfig, c *entity.Character, designation string) bool {
	class, ok := gc.Class(int(c.ClassNumber))
	if !ok {
		return false
	}
	for _, sa := range class.StatAttributes {
		if sa.Designation == designation {
			return true
		}
	}
	return false
}

// chatCommandGoldNotice 实现 /goldnotice <文本>（NoticeChatCommandPlugIn.cs:31-42：
// 去掉命令词后 Trim，空则什么都不发，否则走全局金字）。
func (s *Server) chatCommandGoldNotice(message, key string) {
	text := strings.TrimSpace(strings.Replace(message, key, "", 1))
	if text == "" {
		return
	}
	s.broadcastMessage(text, action.MessageGoldenCenter)
}

// chatCommandOnline 实现 /online（OnlineChatCommandPlugIn.cs:31-46：按 CharacterStatus 分类计数，
// Banned 两边都不计）。原版 ForEachPlayerAsync 只看本游戏服的玩家。
func (s *Server) chatCommandOnline(sess *session, key string) {
	gm, players := 0, 0
	for _, other := range s.trackedSessions() {
		oc := other.getSelected()
		if oc == nil {
			continue
		}
		switch oc.Status {
		case entity.CharacterStatusNormal:
			players++
		case entity.CharacterStatusGameMaster:
			gm++
		}
	}
	s.showLocalizedMessage(sess, player.MsgOnlineCountInfo, key, gm, players)
}
