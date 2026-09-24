// ItemRef：物品 12 字节线上编码的纯数据输入（S6E3 ItemSerializer）。
// 字段语义与 C# ItemSerializer.SerializeItem 一一对应；本参考实现逐行移植其字节公式。
namespace GoldenItems;

internal sealed class ItemRef
{
    public int Number;           // item.Definition.Number（含 >=256）
    public int Group;            // item.Definition.Group
    public byte Level;
    public byte Durability;
    public bool HasSkill;
    public bool Luck;
    public int OptionLevel;      // 普通选项 level（0..7，含 bit2 进 exc 字节）
    public int WingOptionNumber; // >0 时为翅膀备选选项编号
    public byte ExcellentBits;   // 卓越/翅膀选项位掩码（直接给出 0..5 位）
    public bool Is512Item;       // (Number & 0x100)==0x100
    public byte FenrirBits;      // Black/Blue/Gold 0x01/0x02/0x04
    public byte AncientDiscriminator;
    public byte AncientBonusLevel;
    public bool GuardianOption;
    public byte HarmonyNumber;
    public byte HarmonyLevel;
    public bool HasSocketBonus;
    public byte SocketBonusNumber;
    // Socket 槽：-1=无孔(0xFF)，-2=空孔(0xFE)，0..255=已镶嵌球的最终编码字节。
    // 默认全为无孔（普通物品 SocketCount=0，SetSocketBytes 仍写 5 个 0xFF）。
    public int[] Sockets = new int[5] { -1, -1, -1, -1, -1 };
    public bool HasSockets; // 该物品有孔（决定第6字节走 SocketBonus 还是 Harmony）
}
