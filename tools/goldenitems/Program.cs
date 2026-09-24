// goldenitems：物品 12 字节与角色外观 18 字节编码的权威参考向量。
// 公式逐行移植 OpenMU GameServer/RemoteView 的 ItemSerializer / AppearanceSerializer。
// 外观参考实现的正确性由 OpenMU 官方单元测试 AppearanceSerializerTest.NewDarkKnightWithSmallAxe
// 的硬编码 18 字节锚点自校验（启动时断言）。
using System.Text;
using System.Text.Json;
using GoldenItems;

if (args.Length < 1)
{
    Console.Error.WriteLine("用法: goldenitems <输出目录>");
    return 1;
}
Directory.CreateDirectory(args[0]);
var utf8 = new UTF8Encoding(false);
var opts = new JsonSerializerOptions { WriteIndented = true, Encoder = System.Text.Encodings.Web.JavaScriptEncoder.UnsafeRelaxedJsonEscaping };

// ---- 官方锚点自校验：Dark Knight + SmallAxe(RightHand group=1 number=0) ----
var anchor = new AppearanceRef
{
    ClassNumber = 0x20 >> 3,
    Equipment = new EquipRef?[12],
};
anchor.Equipment[0] = new EquipRef { Number = 0, Group = 1 }; // 官方测试 ItemSlot 默认 0（左手）
var anchorBytes = AppearanceSerializerRef.Serialize(anchor);
var anchorExpect = new byte[] { 0x20, 0x00, 0xFF, 0xFF, 0xFF, 0xF3, 0x00, 0x00, 0x00, 0xF8, 0x00, 0x00, 0x20, 0xFF, 0xFF, 0xFF, 0x00, 0x00 };
if (!anchorBytes.SequenceEqual(anchorExpect))
{
    Console.Error.WriteLine($"外观参考实现未通过官方 SmallAxe 锚点:\n got {Convert.ToHexString(anchorBytes)}\nwant {Convert.ToHexString(anchorExpect)}");
    return 2;
}

// ---- 物品向量 ----
var items = new (string Name, ItemRef Item)[]
{
    ("plain_sword", new ItemRef { Number = 0, Group = 0, Durability = 100 }),
    ("level9_skill_luck", new ItemRef { Number = 4, Group = 0, Level = 9, Durability = 255, HasSkill = true, Luck = true }),
    ("option_level7", new ItemRef { Number = 6, Group = 2, Level = 3, Durability = 50, OptionLevel = 7 }),
    ("excellent_full", new ItemRef { Number = 8, Group = 4, Level = 11, Durability = 200, ExcellentBits = 0x3F, Luck = true }),
    ("item512_fenrir_blue", new ItemRef { Number = 256, Group = 12, Durability = 1, Is512Item = true, FenrirBits = 0x02 }),
    ("ancient", new ItemRef { Number = 10, Group = 7, Level = 6, Durability = 80, AncientDiscriminator = 2, AncientBonusLevel = 3 }),
    ("guardian380", new ItemRef { Number = 12, Group = 5, Level = 0, Durability = 120, GuardianOption = true }),
    ("harmony", new ItemRef { Number = 14, Group = 3, Level = 7, Durability = 60, HarmonyNumber = 9, HarmonyLevel = 12 }),
    ("sockets_3", new ItemRef
    {
        Number = 20, Group = 8, Level = 13, Durability = 250, HasSockets = true,
        HasSocketBonus = true, SocketBonusNumber = 20,
        Sockets = new[] { 1, 11, 21, -2, -1 },
    }),
    ("sockets_empty_bonus_absent", new ItemRef
    {
        Number = 21, Group = 8, Durability = 250, HasSockets = true,
        HasSocketBonus = false,
        Sockets = new[] { -2, -2, -2, -2, -2 },
    }),
};
var itemVectors = items.Select(x => new
{
    name = x.Name,
    bytes = Convert.ToHexString(ItemSerializerRef.Serialize(x.Item)),
}).ToArray();

// ---- 外观向量 ----
AppearanceRef With(Action<AppearanceRef> f)
{
    var a = new AppearanceRef { ClassNumber = 0x20 >> 3 };
    f(a);
    return a;
}

var nakedDk = With(_ => { });
var armored = With(a =>
{
    a.Equipment[0] = new EquipRef { Number = 0, Group = 0, Level = 9 };                 // 左手武器
    a.Equipment[2] = new EquipRef { Number = 7, Group = 7, Level = 11, Excellent = true }; // 头
    a.Equipment[3] = new EquipRef { Number = 8, Group = 8, Level = 10, Ancient = true };   // 铠
});
var withWingsPet = With(a =>
{
    a.Equipment[7] = new EquipRef { Number = 37, Group = 12, Level = 13, IsWing = true }; // WingOfEternal
    a.Equipment[8] = new EquipRef { Number = 37, Group = 13, IsPet = true, BlackFenrir = true, GoldFenrir = true };
});

var appearanceVectors = new[]
{
    new { name = "smallaxe_anchor", bytes = Convert.ToHexString(anchorBytes) },
    new { name = "naked_darkknight", bytes = Convert.ToHexString(AppearanceSerializerRef.Serialize(nakedDk)) },
    new { name = "armored", bytes = Convert.ToHexString(AppearanceSerializerRef.Serialize(armored)) },
    new { name = "wings_fenrir", bytes = Convert.ToHexString(AppearanceSerializerRef.Serialize(withWingsPet)) },
};

// ---- 27 字节进图外观向量（AppearanceSerializerExtended）----
var nakedDkExt = AppearanceSerializerExtRef.Serialize(new ExtAppearanceRef { ClassNumber = 4 });
var nakedDkExtExpect = new byte[]
{
    0x04, 0x00,
    0xFF, 0xFF, 0x00, 0xFF, 0xFF, 0x00, 0xFF, 0xFF, 0x00, 0xFF, 0xFF, 0x00,
    0xFF, 0xFF, 0x00, 0xFF, 0xFF, 0x00, 0xFF, 0xFF, 0x00,
    0xFF, 0xFF, 0xFF, 0xFF,
};
if (!nakedDkExt.SequenceEqual(nakedDkExtExpect))
{
    Console.Error.WriteLine($"扩展外观裸装 DK 自校验失败:\n got {Convert.ToHexString(nakedDkExt)}\nwant {Convert.ToHexString(nakedDkExtExpect)}");
    return 3;
}

var elfExt = new ExtAppearanceRef { ClassNumber = 8 };
var gmExt = new ExtAppearanceRef { ClassNumber = 4, GameMaster = true };
var armoredExt = new ExtAppearanceRef
{
    ClassNumber = 4,
    Equipment = new EquipRef?[12],
};
armoredExt.Equipment[0] = new EquipRef { Number = 0, Group = 1, Level = 9, Excellent = true, Ancient = true };
armoredExt.Equipment[7] = new EquipRef { Number = 37, Group = 12, IsWing = true };
armoredExt.Equipment[8] = new EquipRef { Number = 37, Group = 13, IsPet = true, GoldFenrir = true };

var appearanceExtVectors = new[]
{
    new { name = "naked_darkknight_ext", bytes = Convert.ToHexString(nakedDkExt) },
    new { name = "naked_elf_ext", bytes = Convert.ToHexString(AppearanceSerializerExtRef.Serialize(elfExt)) },
    new { name = "gamemaster_ext", bytes = Convert.ToHexString(AppearanceSerializerExtRef.Serialize(gmExt)) },
    new { name = "armored_ext", bytes = Convert.ToHexString(AppearanceSerializerExtRef.Serialize(armoredExt)) },
};

var doc = new
{
    source = "OpenMU src/GameServer/RemoteView ItemSerializer.cs + AppearanceSerializer.cs + AppearanceSerializerExtended.cs (Season6Episode3)",
    anchor_test = "AppearanceSerializerTest.NewDarkKnightWithSmallAxe",
    items = itemVectors,
    appearances = appearanceVectors,
    appearances_ext = appearanceExtVectors,
};
File.WriteAllText(Path.Combine(args[0], "golden.json"), JsonSerializer.Serialize(doc, opts), utf8);
Console.WriteLine($"goldenitems: items={items.Length} appearances=4 appearances_ext=4（锚点校验通过）");
return 0;
