namespace GoldenItems;

// 逐行移植 AppearanceSerializer.WritePreviewCharSet（S6E3，18 字节）。
// 槽位常量取自 InventoryConstants：
//   Left=0 Right=1 Helm=2 Armor=3 Pants=4 Gloves=5 Boots=6 Wings=7 Pet=8
internal static class AppearanceSerializerRef
{
    private const int LeftHandSlot = 0;
    private const int RightHandSlot = 1;
    private const int HelmSlot = 2;
    private const int ArmorSlot = 3;
    private const int PantsSlot = 4;
    private const int GlovesSlot = 5;
    private const int BootsSlot = 6;
    private const int WingsSlot = 7;
    private const int PetSlot = 8;

    public static byte[] Serialize(AppearanceRef a)
    {
        var t = new byte[18];
        var item = new EquipRef?[12];
        for (byte i = 0; i < 12; i++)
        {
            item[i] = a.Equipment[i];
        }

        t[0] = (byte)(a.ClassNumber << 3 & 0xF8);
        t[0] |= a.Pose;

        SetHand(t, item[LeftHandSlot], 1, 12);
        SetHand(t, item[RightHandSlot], 2, 13);
        SetArmorPiece(t, item[HelmSlot], 3, true, 0x80, 13, false);
        SetArmorPiece(t, item[ArmorSlot], 3, false, 0x40, 14, true);
        SetArmorPiece(t, item[PantsSlot], 4, true, 0x20, 14, false);
        SetArmorPiece(t, item[GlovesSlot], 4, false, 0x10, 15, true);
        SetArmorPiece(t, item[BootsSlot], 5, true, 0x08, 15, false);

        SetItemLevels(t, item);

        if (a.FullAncientSetEquipped)
        {
            t[11] |= 0x01;
        }

        AddWing(t, item[WingsSlot]);
        AddPet(t, item[PetSlot]);
        return t;
    }

    private static void SetHand(byte[] p, EquipRef? maybe, int indexIndex, int groupIndex)
    {
        if (maybe is { } e)
        {
            p[indexIndex] = (byte)e.Number;
            p[groupIndex] |= (byte)(e.Group << 5);
        }
        else
        {
            p[indexIndex] = 0xFF;
            p[groupIndex] |= 0xF0;
        }
    }

    private static void SetEmptyArmor(byte[] p, int firstIndex, bool firstHigh, byte secondMask, int thirdIndex, bool thirdHigh)
    {
        p[firstIndex] |= firstHigh ? High(0x0F) : Low(0x0F);
        p[9] |= secondMask;
        p[thirdIndex] |= thirdHigh ? High(0x0F) : Low(0x0F);
    }

    private static byte High(int v) => (byte)((v << 4) & 0xF0);
    private static byte Low(int v) => (byte)(v & 0x0F);

    private static void SetArmorItemIndex(byte[] p, EquipRef item, int firstIndex, bool firstHigh, byte secondMask, int thirdIndex, bool thirdHigh)
    {
        p[firstIndex] |= firstHigh ? High(item.Number) : Low(item.Number);
        var multi = (byte)(item.Number / 16);
        if (multi > 0)
        {
            var bit1 = (byte)(multi % 2);
            var byte2 = (byte)(multi / 2);
            if (bit1 == 1)
            {
                p[9] |= secondMask;
            }
            if (byte2 > 0)
            {
                p[thirdIndex] |= thirdHigh ? High(byte2) : Low(byte2);
            }
        }
    }

    private static void SetArmorPiece(byte[] p, EquipRef? maybe, int firstIndex, bool firstHigh, byte secondMask, int thirdIndex, bool thirdHigh)
    {
        if (maybe is { } item)
        {
            SetArmorItemIndex(p, item, firstIndex, firstHigh, secondMask, thirdIndex, thirdHigh);
            if (item.Excellent)
            {
                p[10] |= secondMask;
            }
            if (item.Ancient)
            {
                p[11] |= secondMask;
            }
        }
        else
        {
            SetEmptyArmor(p, firstIndex, firstHigh, secondMask, thirdIndex, thirdHigh);
        }
    }

    private static void SetItemLevels(byte[] p, EquipRef?[] item)
    {
        var levelIndex = 0;
        for (var i = 0; i < 7; i++)
        {
            if (item[i] is { } e)
            {
                var glow = (byte)((e.Level - 1) / 2);
                levelIndex |= glow << (i * 3);
            }
        }
        p[6] = (byte)((levelIndex >> 16) & 255);
        p[7] = (byte)((levelIndex >> 8) & 255);
        p[8] = (byte)(levelIndex & 255);
    }

    private static void AddWing(byte[] p, EquipRef? maybe)
    {
        if (maybe is not { } wing)
        {
            return;
        }
        var n = wing.Number;
        switch (n)
        {
            case 0: case 1: case 2: case 41:
                p[5] |= 0x04; break;
            case 3: case 4: case 5: case 6: case 30: case 42: case 49:
                p[5] |= 0x08; break;
            case 36: case 37: case 38: case 39: case 40: case 43: case 50:
            case 130: case 131: case 132: case 133: case 134: case 135:
                p[5] |= 0x0C; break;
        }
        switch (n)
        {
            case 0: case 3: case 36: p[9] |= 0x01; break;
            case 1: case 4: case 37: p[9] |= 0x02; break;
            case 2: case 5: case 38: p[9] |= 0x03; break;
            case 41: case 6: case 39: p[9] |= 0x04; break;
            case 30: case 40: p[9] |= 0x05; break;
            case 42: case 43: p[9] |= 0x06; break;
            case 49: case 50: p[9] |= 0x07; break;
            case 130: p[17] |= 0x20; break;
            case 131: p[17] |= 0x40; break;
            case 132: p[17] |= 0x60; break;
            case 133: p[17] |= 0x80; break;
            case 134: p[17] |= 0xA0; break;
            case 135: p[17] |= 0xC0; break;
        }
    }

    private static void AddPet(byte[] p, EquipRef? maybe)
    {
        if (maybe is not { } pet)
        {
            p[5] |= 0b0000_0011;
            return;
        }
        var n = pet.Number;
        switch (n)
        {
            case 0: case 1: case 2:
                p[5] |= (byte)n;
                break;
            case 3:
                p[5] |= 0x03;
                p[10] |= 0x01;
                break;
            case 4:
                p[5] |= 0x03;
                p[12] |= 0x01;
                break;
            case 37:
                p[5] |= 0x03;
                p[10] &= 0xFE;
                p[12] &= 0xFE;
                p[12] |= 0x04;
                p[16] = 0x00;
                if (pet.BlackFenrir) p[16] |= 0x01;
                if (pet.BlueFenrir) p[16] |= 0x02;
                if (pet.GoldFenrir) p[17] |= 0x01;
                break;
            default:
                p[5] |= 0x03;
                break;
        }
        switch (n)
        {
            case 80: p[16] |= 0xE0; break;
            case 106: p[16] |= 0xA0; break;
            case 123: p[16] |= 0x60; break;
            case 66: p[16] |= 0x80; break;
            case 65: p[16] |= 0x40; break;
            case 64: p[16] |= 0x20; break;
        }
    }
}
