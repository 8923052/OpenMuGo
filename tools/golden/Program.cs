// goldengen 使用官方 NuGet 包 MUnique.OpenMU.Network.Packets 构造代表性封包，
// 输出权威字节（hex）作为 Go 侧 golden 向量。
// 仅在协议版本升级时运行一次：dotnet run --project tools/golden -- <输出目录>
using System.Text;
using MUnique.OpenMU.Network.Packets;
using MUnique.OpenMU.Network.Packets.ClientToServer;
using MUnique.OpenMU.Network.Packets.ServerToClient;

if (args.Length < 1)
{
    Console.Error.WriteLine("用法: goldengen <输出目录> [s2c|c2s|all]");
    return 1;
}

var outDir = args[0];
var mode = args.Length >= 2 ? args[1] : "all";
Directory.CreateDirectory(outDir);
var manifest = new List<(string File, string Packet, string Note)>();
var utf8NoBom = new UTF8Encoding(false);

static byte[] BytesOf(Memory<byte> packet) => packet.ToArray();

void Emit(string file, string packet, string note, byte[] bytes)
{
    File.WriteAllText(Path.Combine(outDir, file), Convert.ToHexString(bytes) + Environment.NewLine, utf8NoBom);
    manifest.Add((file, packet, note));
}

if (mode is "all" or "s2c")
{
// 1. ChatMessage：C1；Enum 字节；定长 String(10)；到包尾变长 String（含 1 字节 null）
var chat = new ChatMessage(new byte[ChatMessage.GetRequiredSize("Hi")]);
chat.Type = ChatMessage.ChatMessageType.Normal;
chat.Sender = "Alice";
chat.Message = "Hi";
Emit("01_chat_message.hex", "ChatMessage", "C1; enum byte; String[10]; trailing String + null", BytesOf(chat));

// 2. WeatherStatusUpdate：偏移 3 高 4 位 Weather + 低 4 位 Variation
var weather = new WeatherStatusUpdate(new byte[WeatherStatusUpdate.Length]);
weather.Weather = 2;
weather.Variation = 9;
Emit("02_weather_status.hex", "WeatherStatusUpdate", "C1; 4-bit + 4-bit shared byte", BytesOf(weather));

// 3. FriendInvitationResult：C3（加密帧头形态）；Boolean；IntegerBigEndian
var friend = new FriendInvitationResult(new byte[FriendInvitationResult.Length]);
friend.Success = true;
friend.RequestId = 0x01020304;
Emit("03_friend_invitation_result.hex", "FriendInvitationResult", "C3; boolean bit0; uint32 big-endian", BytesOf(friend));

// 4. GameServerEntered：C1+SubCode；Boolean 默认 true；ShortBigEndian；String(5) 与 Binary(5) 同位
var entered = new GameServerEntered(new byte[GameServerEntered.Length]);
entered.PlayerId = 0x0102;
entered.VersionString = "1.04d";
Emit("04_game_server_entered.hex", "GameServerEntered", "C1+sub; default bool; uint16 BE; String[5]", BytesOf(entered));

// 5. MasterSkillLevelUpdate：Boolean；ShortLittleEndian*2；Byte*2；Float*2（小端）
var masterSkill = new MasterSkillLevelUpdate(new byte[MasterSkillLevelUpdate.Length]);
masterSkill.Success = true;
masterSkill.MasterLevelUpPoints = 0x1122;
masterSkill.MasterSkillIndex = 0x33;
masterSkill.MasterSkillNumber = 0x4455;
masterSkill.Level = 0x66;
masterSkill.DisplayValue = 1.5f;
masterSkill.DisplayValueOfNextLevel = 2.5f;
Emit("05_master_skill_level.hex", "MasterSkillLevelUpdate", "C1+sub; bool; uint16 LE; float32 LE x2", BytesOf(masterSkill));

// 6. MasterStatsUpdate：ShortLittleEndian + 两个 LongBigEndian
var masterStats = new MasterStatsUpdate(new byte[MasterStatsUpdate.Length]);
masterStats.MasterLevel = 0x1122;
masterStats.MasterExperience = 0x0102030405060708UL;
masterStats.MasterExperienceOfNextLevel = 0x1112131415161718UL;
Emit("06_master_stats.hex", "MasterStatsUpdate", "C1+sub; uint16 LE; uint64 BE x2", BytesOf(masterStats));

// 7. MuHelperConfigurationData：C2 双字节大端长度；定长 Binary(257)，内容全零
var muHelper = new MuHelperConfigurationData(new byte[MuHelperConfigurationData.Length]);
Emit("07_muhelper_config.hex", "MuHelperConfigurationData", "C2; 2-byte BE length=261; Binary[257] zeros", BytesOf(muHelper));

// 8. CharacterList：C1+SubCode；Structure[] 固定跨步 34；结构内 4 位枚举 + 4 位 Boolean
var list = new CharacterList(new byte[CharacterList.GetRequiredSize(2)]);
list.UnlockFlags = CharacterCreationUnlockFlags.None;
list.MoveCnt = 1;
list.CharacterCount = 2;
list.IsVaultExtended = true;
var c0 = list[0];
c0.SlotIndex = 0;
c0.Name = "Bob";
c0.Level = 100;
c0.Status = CharacterStatus.Normal;
c0.IsItemBlockActive = true;
c0.GuildPosition = GuildMemberRole.NormalMember;
var c1 = list[1];
c1.SlotIndex = 1;
c1.Name = "Charlie123";
c1.Level = 400;
c1.Status = CharacterStatus.Banned;
c1.IsItemBlockActive = false;
c1.GuildPosition = GuildMemberRole.GuildMaster;
Emit("08_character_list.hex", "CharacterList", "C1+sub; Structure[2] stride=34; 4-bit enum; 4-bit bool; Binary[18] in struct", BytesOf(list));
}

if (mode is "all" or "c2s")
{
// C2S-1. Ping：C3+SubCode；IntegerLittleEndian + ShortLittleEndian
var ping = new Ping(new byte[Ping.Length]);
ping.TickCount = 0x01020304;
ping.AttackSpeed = 0x1122;
Emit("c2s_01_ping.hex", "Ping", "C3+sub; uint32 LE; uint16 LE", BytesOf(ping));

// C2S-2. PublicChatMessage：C1；定长 String(10) + 到包尾变长 String
var pubChat = new PublicChatMessage(new byte[PublicChatMessage.GetRequiredSize("Hi")]);
pubChat.Character = "Alice";
pubChat.Message = "Hi";
Emit("c2s_02_public_chat.hex", "PublicChatMessage", "C1; String[10]; trailing String + null", BytesOf(pubChat));
}

var sb = new StringBuilder();
sb.AppendLine("[");
for (var i = 0; i < manifest.Count; i++)
{
    var m = manifest[i];
    var comma = i < manifest.Count - 1 ? "," : "";
    sb.AppendLine("  {");
    sb.AppendLine($"    \"file\": {Json(m.File)},");
    sb.AppendLine($"    \"packet\": {Json(m.Packet)},");
    sb.AppendLine($"    \"note\": {Json(m.Note)}");
    sb.AppendLine("  }" + comma);
}
sb.AppendLine("]");
File.WriteAllText(Path.Combine(outDir, "golden.json"), sb.ToString(), utf8NoBom);

Console.WriteLine($"goldengen: 写出 {manifest.Count} 条向量到 {outDir}");
return 0;

static string Json(string s) => "\"" + s.Replace("\\", "\\\\").Replace("\"", "\\\"") + "\"";
