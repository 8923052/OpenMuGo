package action

// chat_command.go —— 聊天命令的参数解析（TRIM-08d），对照
// GameLogic/PlugIns/ChatCommands/CommandExtensions.cs:33-158 与 TrySetPropertyValueAsync:205-228。
//
// 三处与直觉不同的原版事实：
//   - token 取法是 `command.Split(' ')` 后**丢掉含 '/' 的项**（命令词自身因此被丢），
//     连续空格会留下空串并占掉一个位置 → 位置参数错位，本仓照抄；
//   - 位置模式下，只有当命令存在"带 [Argument] 特性的参数"时才按必填数校验个数；
//     否则 requiredCount = 给出的个数，即**永远不会**因个数不足被拒（多给的被忽略）；
//   - 位置模式下类型转换失败**不阻断命令**：原版对每个失败发一条蓝字、继续解析，
//     最后仍返回 instance（失败的属性保持默认值 0）→ 命令照旧执行。
//     （只有"含 = 的短名模式"与"短名模式缺必填"会让命令不执行。）
//
// 导出表里 short_name 非空 ⇔ 原版该属性带 [Argument]（ChatCommandParameterInfo 文档口径），
// 故"是否参与必填计数/短名匹配"直接看 ShortName。

import (
	"strconv"
	"strings"

	"mugo/internal/gamelogic/config"
)

// ChatArgTypeFailure 是一次类型转换失败（原版就地发 CommandExtensionsArgumentInvalidType）。
type ChatArgTypeFailure struct {
	Parameter string
	TypeName  string
}

// ChatArgs 是一条命令的解析结果。
type ChatArgs struct {
	// Values 按参数名存**原始字符串**（未给到的参数不在表里）。
	Values map[string]string
	// TypeFailures 是转换失败项（顺序 = 参数顺序）；命令仍会执行。
	TypeFailures []ChatArgTypeFailure
	// TooFewArguments 对应 CommandExtensionsInvalidArgumentCount，为 true 时命令不执行。
	TooFewArguments bool
	RequiredCount   int
	GivenCount      int
	// MissingRequired 对应逐条 CommandExtensions_RequiredArgumentMissing，非空时命令不执行。
	MissingRequired []string
	// Runnable 为 false ⇔ 原版 TryParseArgumentsAsync 返回 null。
	Runnable bool
}

// String 取某个参数的原值（未提供时为空串）。
func (a ChatArgs) String(parameter string) string { return a.Values[parameter] }

// Uint16 按原版 UInt16 语义取值（未提供/越界 → 0）。
func (a ChatArgs) Uint16(parameter string) uint16 {
	v, ok := chatParseUint(a.Values[parameter], 16)
	if !ok {
		return 0
	}
	return uint16(v)
}

// Int32 按原版 Int32 语义取值（未提供/越界 → 0）。
func (a ChatArgs) Int32(parameter string) int32 {
	v, ok := chatParseInt(a.Values[parameter], 32)
	if !ok {
		return 0
	}
	return int32(v)
}

// ParseChatArguments 解析一条完整命令串（含命令词本身，如 "/add agi 5"）。
func ParseChatArguments(cmd *config.ChatCommand, command string) ChatArgs {
	out := ChatArgs{Values: map[string]string{}}
	tokens := chatTokens(command)
	if strings.Contains(command, "=") {
		return parseNamedArguments(cmd, tokens, out)
	}
	return parsePositionalArguments(cmd, tokens, out)
}

// chatTokens 复刻 `command.Split(' ').Where(x => !x.Contains("/"))`。
func chatTokens(command string) []string {
	parts := strings.Split(command, " ")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if !strings.Contains(p, "/") {
			out = append(out, p)
		}
	}
	return out
}

// parsePositionalArguments 按参数表顺序取值（原版 :56-70）。
func parsePositionalArguments(cmd *config.ChatCommand, tokens []string, out ChatArgs) ChatArgs {
	attributed := false
	required := 0
	for _, p := range cmd.Parameters {
		if p.ShortName != "" {
			attributed = true
			if p.IsRequired {
				required++
			}
		}
	}
	if !attributed {
		required = len(tokens) // 原版：无 [Argument] 特性时 required = 给出的个数
	}
	if len(tokens) < required {
		out.TooFewArguments = true
		out.RequiredCount = required
		out.GivenCount = len(tokens)
		return out
	}
	out.Runnable = true
	for i := 0; i < len(tokens) && i < len(cmd.Parameters); i++ {
		p := cmd.Parameters[i]
		if !chatValueOfType(p.TypeName, tokens[i]) {
			out.TypeFailures = append(out.TypeFailures,
				ChatArgTypeFailure{Parameter: p.Name, TypeName: p.TypeName})
			continue // 转换失败：该属性保持默认（不进 Values）
		}
		out.Values[p.Name] = tokens[i]
	}
	return out
}

// parseNamedArguments 处理 `shortName=value` 记法（原版 ReadNamedArgumentsAsync:159-203）：
// 只认带短名的参数；转换失败或缺必填 → 命令不执行。
func parseNamedArguments(cmd *config.ChatCommand, tokens []string, out ChatArgs) ChatArgs {
	missing := make([]string, 0, len(cmd.Parameters))
	for _, p := range cmd.Parameters {
		if p.ShortName == "" {
			continue
		}
		var hit string
		found := false
		for _, t := range tokens {
			// 原版：x.Split('=').First().Trim() == attribute.ShortName
			if strings.TrimSpace(strings.SplitN(t, "=", 2)[0]) != p.ShortName {
				continue
			}
			parts := strings.SplitN(t, "=", 2)
			if len(parts) < 2 {
				hit = "" // 原版 Replace($"{ShortName}=") 落空 → 值为空串，仍算命中
			} else {
				hit = strings.ReplaceAll(parts[1], p.ShortName+"=", "")
			}
			found = true
			break
		}
		if !found {
			if p.IsRequired {
				missing = append(missing, p.Name)
			}
			continue
		}
		if !chatValueOfType(p.TypeName, hit) {
			out.TypeFailures = append(out.TypeFailures, ChatArgTypeFailure{Parameter: p.Name, TypeName: p.TypeName})
			return out // 原版 return false：命令不执行
		}
		out.Values[p.Name] = hit
	}
	if len(missing) > 0 {
		out.MissingRequired = missing
		return out
	}
	out.Runnable = true
	return out
}

// chatValueOfType 判原值能否按原版的 Convert.ChangeType 语义转成该 CLR 类型。
func chatValueOfType(typeName, raw string) bool {
	switch typeName {
	case "String":
		return true
	case "Boolean":
		if raw == "0" || raw == "1" { // 原版对 bool 特判 0/1
			return true
		}
		_, err := strconv.ParseBool(strings.TrimSpace(raw))
		return err == nil
	case "Byte":
		_, ok := chatParseUint(raw, 8)
		return ok
	case "SByte":
		_, ok := chatParseInt(raw, 8)
		return ok
	case "UInt16":
		_, ok := chatParseUint(raw, 16)
		return ok
	case "Int16":
		_, ok := chatParseInt(raw, 16)
		return ok
	case "UInt32":
		_, ok := chatParseUint(raw, 32)
		return ok
	case "Int32":
		_, ok := chatParseInt(raw, 32)
		return ok
	case "UInt64":
		_, ok := chatParseUint(raw, 64)
		return ok
	case "Int64":
		_, ok := chatParseInt(raw, 64)
		return ok
	}
	return true
}

func chatParseUint(raw string, bits int) (uint64, bool) {
	v, err := strconv.ParseUint(strings.TrimSpace(raw), 10, bits)
	if err != nil {
		return 0, false
	}
	return v, true
}

func chatParseInt(raw string, bits int) (int64, bool) {
	v, err := strconv.ParseInt(strings.TrimSpace(raw), 10, bits)
	if err != nil {
		return 0, false
	}
	return v, true
}
