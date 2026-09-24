namespace GoldenItems;

// 外观输入。Equipment 按 InventoryConstants 槽位索引：
// 0 左手 1 右手 2 头 3 铠 4 裤 5 手 6 鞋 7 翅膀 8 宠物。
internal sealed class EquipRef
{
    public int Number;
    public int Group;
    public byte Level;
    public bool Excellent;
    public bool Ancient;
    public bool IsPet;   // 槽 8
    public bool IsWing;  // 槽 7
    // 宠物可见选项（Fenrir）
    public bool BlackFenrir;
    public bool BlueFenrir;
    public bool GoldFenrir;
}

internal sealed class AppearanceRef
{
    public int ClassNumber;
    public byte Pose;
    public bool FullAncientSetEquipped;
    public EquipRef?[] Equipment = new EquipRef[12]; // 仅 0..8 使用
}
