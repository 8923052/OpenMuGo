package s2c

// 与官方 NuGet 包 MUnique.OpenMU.Network.Packets 0.9.10 的 C# 生成物做逐字节差分。
// 权威向量由 tools/goldengen 产出，提交在 testdata/golden；
// 本测试不依赖 dotnet，日常 CI 可直接运行。

import (
	"bytes"
	"embed"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
)

//go:embed testdata/golden/golden.json testdata/golden/*.hex
var goldenFS embed.FS

type goldenMeta struct {
	File   string `json:"file"`
	Packet string `json:"packet"`
	Note   string `json:"note"`
}

func loadGolden(t *testing.T, file string) []byte {
	t.Helper()
	raw, err := goldenFS.ReadFile("testdata/golden/" + file)
	if err != nil {
		t.Fatalf("读取 golden 向量 %s: %v", file, err)
	}
	got, err := hex.DecodeString(strings.TrimSpace(string(raw)))
	if err != nil {
		t.Fatalf("hex 解码 %s: %v", file, err)
	}
	return got
}

func buildByName(t *testing.T, packet string) []byte {
	t.Helper()
	switch packet {
	case "ChatMessage":
		p := NewChatMessage(ChatMessageRequiredSize(2))
		p.SetType(ChatMessageType_Normal)
		p.SetSender("Alice")
		p.SetMessage("Hi")
		return p.Bytes()

	case "WeatherStatusUpdate":
		p := NewWeatherStatusUpdate()
		p.SetWeather(2)
		p.SetVariation(9)
		return p.Bytes()

	case "FriendInvitationResult":
		p := NewFriendInvitationResult()
		p.SetSuccess(true)
		p.SetRequestId(0x01020304)
		return p.Bytes()

	case "GameServerEntered":
		p := NewGameServerEntered()
		p.SetPlayerId(0x0102)
		p.SetVersionString("1.04d")
		return p.Bytes()

	case "MasterSkillLevelUpdate":
		p := NewMasterSkillLevelUpdate()
		p.SetSuccess(true)
		p.SetMasterLevelUpPoints(0x1122)
		p.SetMasterSkillIndex(0x33)
		p.SetMasterSkillNumber(0x4455)
		p.SetLevel(0x66)
		p.SetDisplayValue(1.5)
		p.SetDisplayValueOfNextLevel(2.5)
		return p.Bytes()

	case "MasterStatsUpdate":
		p := NewMasterStatsUpdate()
		p.SetMasterLevel(0x1122)
		p.SetMasterExperience(0x0102030405060708)
		p.SetMasterExperienceOfNextLevel(0x1112131415161718)
		return p.Bytes()

	case "MuHelperConfigurationData":
		return NewMuHelperConfigurationData().Bytes()

	case "CharacterList":
		p := NewCharacterList(CharacterListRequiredSize(2))
		p.SetUnlockFlags(CharacterCreationUnlockFlags_None)
		p.SetMoveCnt(1)
		p.SetCharacterCount(2)
		p.SetIsVaultExtended(true)
		c0 := p.Characters(0)
		c0.SetSlotIndex(0)
		c0.SetName("Bob")
		c0.SetLevel(100)
		c0.SetStatus(CharacterStatus_Normal)
		c0.SetIsItemBlockActive(true)
		c0.SetGuildPosition(GuildMemberRole_NormalMember)
		c1 := p.Characters(1)
		c1.SetSlotIndex(1)
		c1.SetName("Charlie123")
		c1.SetLevel(400)
		c1.SetStatus(CharacterStatus_Banned)
		c1.SetIsItemBlockActive(false)
		c1.SetGuildPosition(GuildMemberRole_GuildMaster)
		return p.Bytes()

	default:
		t.Fatalf("未实现的 golden 构造分支: %s", packet)
		return nil
	}
}

// verifyRead 从权威字节解码回视图并抽查关键字段。
func verifyRead(t *testing.T, packet string, want []byte) {
	t.Helper()
	switch packet {
	case "ChatMessage":
		p := AsChatMessage(want)
		if p.Type() != ChatMessageType_Normal || p.SenderString() != "Alice" || p.MessageString() != "Hi" {
			t.Fatalf("ChatMessage 回读失败: %v %q %q", p.Type(), p.SenderString(), p.MessageString())
		}
	case "WeatherStatusUpdate":
		p := AsWeatherStatusUpdate(want)
		if p.Weather() != 2 || p.Variation() != 9 {
			t.Fatalf("WeatherStatusUpdate 回读失败: %d %d", p.Weather(), p.Variation())
		}
	case "FriendInvitationResult":
		p := AsFriendInvitationResult(want)
		if !p.Success() || p.RequestId() != 0x01020304 {
			t.Fatalf("FriendInvitationResult 回读失败")
		}
	case "GameServerEntered":
		p := AsGameServerEntered(want)
		if !p.Success() || p.PlayerId() != 0x0102 || string(p.VersionString()) != "1.04d" {
			t.Fatalf("GameServerEntered 回读失败")
		}
	case "MasterSkillLevelUpdate":
		p := AsMasterSkillLevelUpdate(want)
		if !p.Success() || p.MasterSkillIndex() != 0x33 || p.Level() != 0x66 ||
			p.DisplayValue() != 1.5 || p.DisplayValueOfNextLevel() != 2.5 {
			t.Fatalf("MasterSkillLevelUpdate 回读失败")
		}
	case "MasterStatsUpdate":
		p := AsMasterStatsUpdate(want)
		if p.MasterExperience() != 0x0102030405060708 ||
			p.MasterExperienceOfNextLevel() != 0x1112131415161718 {
			t.Fatalf("MasterStatsUpdate 回读失败")
		}
	case "MuHelperConfigurationData":
		p := AsMuHelperConfigurationData(want)
		if p.Len() != 261 || want[0] != 0xC2 || want[1] != 0x01 || want[2] != 0x05 || want[3] != 0xAE {
			t.Fatalf("MuHelperConfigurationData 帧头/长度错误")
		}
	case "CharacterList":
		p := AsCharacterList(want)
		if p.CharacterCount() != 2 || !p.IsVaultExtended() {
			t.Fatalf("CharacterList 计数回读失败")
		}
		c0 := p.Characters(0)
		c1 := p.Characters(1)
		if c0.NameString() != "Bob" || c0.Level() != 100 || !c0.IsItemBlockActive() {
			t.Fatalf("CharacterList[0] 回读失败")
		}
		if c1.NameString() != "Charlie123" || c1.Level() != 400 ||
			c1.Status() != CharacterStatus_Banned || c1.GuildPosition() != GuildMemberRole_GuildMaster {
			t.Fatalf("CharacterList[1] 回读失败")
		}
		if p.Characters(2) != nil {
			t.Fatalf("CharacterList 越界索引未返回 nil")
		}
	}
}

func TestGoldenVectors(t *testing.T) {
	manifestRaw, err := goldenFS.ReadFile("testdata/golden/golden.json")
	if err != nil {
		t.Fatalf("读取 golden.json: %v", err)
	}
	var metas []goldenMeta
	if err := json.Unmarshal(manifestRaw, &metas); err != nil {
		t.Fatalf("解析 golden.json: %v", err)
	}
	if len(metas) < 8 {
		t.Fatalf("golden 向量数量 %d，期望至少 8", len(metas))
	}
	for _, m := range metas {
		t.Run(m.Packet, func(t *testing.T) {
			want := loadGolden(t, m.File)
			got := buildByName(t, m.Packet)
			if !bytes.Equal(got, want) {
				t.Fatalf("与 C# 权威字节不一致\n got: %X\nwant: %X", got, want)
			}
			verifyRead(t, m.Packet, want)
			info, ok := LookupPacket(want)
			if !ok || info.Name != m.Packet {
				t.Fatalf("路由解析失败: ok=%v info=%+v", ok, info)
			}
		})
	}
}
