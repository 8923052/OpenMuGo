namespace GoldenItems;

// 27 字节进图外观输入（对照 OpenMU IAppearanceData + ItemAppearance 的最小字段集）。
internal sealed class ExtAppearanceRef
{
    public int ClassNumber { get; init; }
    public byte Pose { get; init; }
    public bool FullAncientSetEquipped { get; init; }
    public bool GameMaster { get; init; }
    public EquipRef?[] Equipment { get; init; } = new EquipRef?[12];
}

// 逐行移植 OpenMU GameServer/RemoteView/AppearanceSerializerExtended.cs（S6E3 106.3+）。
// 输出 27 字节：[0] 职业原始编号、[1] pose/标记、7 件 3B 闪光装备、翅膀/宠物各 2B。
internal static class AppearanceSerializerExtRef
{
    public const int NeededSpace = 27;

    public static byte[] Serialize(ExtAppearanceRef a)
    {
        var t = new byte[NeededSpace];
        var items = a.Equipment;

        t[0] = (byte)a.ClassNumber;
        t[1] = a.Pose;
        if (a.FullAncientSetEquipped)
        {
            t[1] |= 0x10;
        }

        if (a.GameMaster)
        {
            t[1] |= 0x20;
        }

        SetShinyItem(t.AsSpan(2..5), items[0]);
        SetShinyItem(t.AsSpan(5..8), items[1]);
        SetShinyItem(t.AsSpan(8..11), items[2]);
        SetShinyItem(t.AsSpan(11..14), items[3]);
        SetShinyItem(t.AsSpan(14..17), items[4]);
        SetShinyItem(t.AsSpan(17..20), items[5]);
        SetShinyItem(t.AsSpan(20..23), items[6]);
        SetUnshinyItem(t.AsSpan(23..25), items[7]);
        SetUnshinyItem(t.AsSpan(25..27), items[8]);

        var pet = items[8];
        if (pet is not null)
        {
            if (pet.BlackFenrir)
            {
                t[25] |= 0b10;
            }

            if (pet.BlueFenrir)
            {
                t[25] |= 0b100;
            }

            if (pet.GoldFenrir)
            {
                t[25] |= 0b110;
            }
        }

        return t;
    }

    private static void SetShinyItem(Span<byte> data, EquipRef? item)
    {
        if (item is null)
        {
            data[0] = 0xFF;
            data[1] = 0xFF;
            data[2] = 0x00;
            return;
        }

        data[0] = (byte)(((int)item.Group & 0xF) << 4 | ((item.Number & 0x0F00) >> 8));
        data[1] = (byte)(item.Number & 0xFF);
        var glow = item.Level >= 1 ? (item.Level - 1) / 2 : 0;
        data[2] = (byte)((glow & 0xF) << 4);
        if (item.Excellent)
        {
            data[2] |= 0b00001000;
        }

        if (item.Ancient)
        {
            data[2] |= 0b00000100;
        }
    }

    private static void SetUnshinyItem(Span<byte> data, EquipRef? item)
    {
        if (item is null)
        {
            data[0] = 0xFF;
            data[1] = 0xFF;
            return;
        }

        data[0] = (byte)(((int)item.Group & 0xF) << 4 | ((item.Number >> 8) & 0xF));
        data[1] = (byte)(item.Number & 0xFF);
    }
}
