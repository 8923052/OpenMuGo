// Tool goldencombat：用 OpenMU 原实现（AttributeSystem + Season6 初始化）产出
// 职业属性向量——按职业 × 等级dump全量战斗相关属性值（doc/15 §11 T2-0 / 防线 2）。
// Go 侧 internal/gamelogic/player 的属性装配与该向量逐值比对。
// 用法: goldencombat <output-file>
namespace GoldenCombat;

using System.Text.Json;
using Microsoft.Extensions.Logging;
using MUnique.OpenMU.AttributeSystem;
using MUnique.OpenMU.DataModel.Configuration;
using MUnique.OpenMU.Persistence.InMemory;
using MUnique.OpenMU.Persistence.Initialization.VersionSeasonSix;

internal static class Program
{
    // 与 doc/15 §11 T2-0 对应：覆盖 MVP 全部职业 × 两个等级点。
    private static readonly (string Name, int Number, int Level)[] Scenario =
    {
        ("Dark Wizard", 0, 1), ("Dark Wizard", 0, 100),
        ("Dark Knight", 4, 1), ("Dark Knight", 4, 100),
        ("Fairy Elf", 8, 1), ("Fairy Elf", 8, 100),
        ("Magic Gladiator", 12, 1), ("Magic Gladiator", 12, 100),
        ("Dark Lord", 16, 1), ("Dark Lord", 16, 100),
    };

    // 战斗相关 designation（与导出件 80_attributes.json 命名一致）。
    private static readonly string[] Designations =
    {
        "Total Strength", "Total Agility", "Total Vitality", "Total Energy", "Total Leadership",
        "Maximum Health", "Maximum Mana", "Maximum Shield", "Maximum Ability",
        "Attack Speed", "Magic Speed", "Base Defense", "Defense (PvM)",
        "Attack Rate (PvM)", "Defense Rate (PvM)",
        // TRIM-06：PvP 选型、诅咒/Fenrir 基伤、连击与 Soul Barrier、大师开关板。
        // 职业裸装下这些多为 0，比对的是"两侧都读到同一条属性、且值一致"。
        "Attack Rate (PvP)", "Defense Rate (PvP)", "Defense (PvP)",
        "Minimum Curse Base Damage", "Maximum Curse Base Damage",
        "Curse Attack Damage Increase Multiplier", "Fenrir Base Damage",
        "Weakness Physical Damage Decrement", "Berserker Proficiency Multiplier (MST)",
        "Final Damage Increase (PvP)", "Combo Bonus", "Is Skill Combo Available",
        "Soul Barrier Damage Receive Decrement", "Soul Barrier Mana Toll Per Received Hit",
        "Master Skill Physical Bonus Damage (MST)", "Bow Strengthener Bonus Damage (MST)",
    };

    private static async Task<int> Main(string[] args)
    {
        if (args.Length < 1)
        {
            Console.Error.WriteLine("usage: goldencombat <output-file>");
            return 1;
        }

        var loggerFactory = Microsoft.Extensions.Logging.Abstractions.NullLoggerFactory.Instance;
        var provider = new InMemoryPersistenceContextProvider();
        var initialization = new DataInitialization(provider, loggerFactory);
        await initialization.CreateInitialDataAsync(1, createTestAccounts: false).ConfigureAwait(false);

        var configContext = provider.CreateNewConfigurationContext();
        var gameConfiguration = (await configContext.GetAsync<GameConfiguration>().ConfigureAwait(false)).Single();
        var classes = gameConfiguration.CharacterClasses.ToDictionary(c => c.Number);

        var outEntries = new List<object>();
        foreach (var (name, number, level) in Scenario)
        {
            if (!classes.TryGetValue((byte)number, out var cls))
            {
                Console.Error.WriteLine($"职业 {number} ({name}) 不存在，跳过");
                continue;
            }

            // 原版 ItemAwareAttributeSystem 装配：stat=类初始值（Level 覆盖为场景值），
            // base=类基值，relationships=类组合（Season6 全局组合为空）。
            var statAttributes = cls.StatAttributes
                .Select(s => (IAttribute)new StatAttribute(s.Attribute, s.Attribute!.Designation == "Level" ? level : s.BaseValue))
                .ToList();
            var baseAttributes = cls.BaseAttributeValues
                .Select(b => (IAttribute)new ConstValueAttribute(b.Value, b.Definition!))
                .ToList();
            var system = new AttributeSystem(statAttributes, baseAttributes, cls.AttributeCombinations);

            // 按 designation 读值（原版 GetValueOfAttribute 语义：含定义级钳制）。
            var values = new Dictionary<string, double>();
            foreach (var designation in Designations)
            {
                var def = FindDefinition(gameConfiguration, cls, designation);
                values[designation] = def is null ? 0 : system[def];
            }

            outEntries.Add(new { class_name = name, class_number = (int)number, level, values });
        }

        var payload = new
        {
            scenario = "class combat stats (attribute system replay)",
            note = "值来自 OpenMU AttributeSystem 原实现；Go 侧 internal/gamelogic/player 逐值比对。",
            entries = outEntries,
        };
        var options = new JsonSerializerOptions { WriteIndented = true };
        await File.WriteAllTextAsync(args[0], JsonSerializer.Serialize(payload, options)).ConfigureAwait(false);
        Console.WriteLine($"combat vectors → {Path.GetFullPath(args[0])}");
        return 0;
    }

    private static AttributeDefinition? FindDefinition(GameConfiguration config, CharacterClass cls, string designation)
    {
        // 类 StatAttributes/BaseAttributeValues 里的定义优先（同 Id），再查全局 Attributes。
        foreach (var s in cls.StatAttributes)
        {
            if (s.Attribute?.Designation == designation)
            {
                return s.Attribute;
            }
        }

        foreach (var b in cls.BaseAttributeValues)
        {
            if (b.Definition?.Designation == designation)
            {
                return b.Definition;
            }
        }

        return config.Attributes.FirstOrDefault(a => a.Designation == designation);
    }
}
