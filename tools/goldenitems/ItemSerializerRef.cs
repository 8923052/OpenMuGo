namespace GoldenItems;

// 逐行移植 MUnique.OpenMU.GameServer.RemoteView.ItemSerializer（S6E3，12 字节）
// 与 ItemSerializerHelper 的字节公式。仅保留字节级行为，去除 GameConfiguration 依赖。
internal static class ItemSerializerRef
{
    private const byte LuckFlag = 4;
    private const byte SkillFlag = 128;
    private const byte LevelMask = 0x78;
    private const byte GuardianOptionFlag = 0x08;
    private const byte AncientBonusLevelMask = 0b1100;
    private const byte AncientDiscriminatorMask = 0b0011;
    private const int MaximumSockets = 5;

    public const byte EmptySocket = 0xFE;
    public const byte NoSocket = 0xFF;

    public static byte[] Serialize(ItemRef item)
    {
        var t = new byte[12];

        t[0] = (byte)item.Number;
        t[1] = (byte)((item.Level << 3) & LevelMask);

        // 普通选项：optionLevel 低2位进 [1]，bit2 进 [3]=0x40；翅膀备选选项编号。
        if (item.OptionLevel != 0 || item.WingOptionNumber > 0)
        {
            t[1] += (byte)(item.OptionLevel & 3);
            t[3] = (byte)((item.OptionLevel & 4) << 4);
            if (item.WingOptionNumber > 0)
            {
                t[3] |= (byte)((item.WingOptionNumber & 0b11) << 4);
            }
        }

        t[2] = item.Durability;
        t[3] |= (byte)(item.ExcellentBits & 0x3F);

        if (item.Is512Item)
        {
            t[3] |= 0x80;
        }

        t[3] |= item.FenrirBits;

        if (item.Luck)
        {
            t[1] |= LuckFlag;
        }
        if (item.HasSkill)
        {
            t[1] |= SkillFlag;
        }

        if (item.AncientDiscriminator != 0 || item.AncientBonusLevel != 0)
        {
            t[4] |= (byte)(item.AncientDiscriminator & AncientDiscriminatorMask);
            t[4] |= (byte)((item.AncientBonusLevel << 2) & AncientBonusLevelMask);
        }

        t[5] = (byte)(item.Group << 4);
        if (item.GuardianOption)
        {
            t[5] |= GuardianOptionFlag;
        }

        // 第6字节：无孔物品走 Harmony，有孔物品走 SocketBonus（二者不共存）。
        if (item.HasSockets)
        {
            t[6] = item.HasSocketBonus ? item.SocketBonusNumber : (byte)0xFF;
        }
        else
        {
            t[6] = (byte)((item.HarmonyNumber << 4) | (item.HarmonyLevel & 0x0F));
        }

        for (var i = 0; i < MaximumSockets; i++)
        {
            var slot = item.Sockets[i];
            t[7 + i] = slot switch
            {
                -1 => NoSocket,
                -2 => EmptySocket,
                _ => (byte)slot,
            };
        }

        return t;
    }
}
