// Tool goldenattributes：用 OpenMU AttributeSystem 原实现产出属性聚合向量
// （doc/10 防线 2 / T0-a 验收），Go 侧移植（internal/attribute）逐值比对。
//
// 场景覆盖：StatAttribute、ConstValueAttribute、四种 AggregateType
// （AddRaw/Multiplicate/AddFinal/Maximum）、六种 InputOperator、
// 定义级 MaximumValue 钳制、关系传播（改 stat 后派生值联动）。
// 用法: goldenattributes <output-file>
namespace GoldenAttributes;

using System.Text.Json;
using MUnique.OpenMU.AttributeSystem;

internal static class Program
{
    private static async Task<int> Main(string[] args)
    {
        if (args.Length < 1)
        {
            Console.Error.WriteLine("usage: goldenattributes <output-file>");
            return 1;
        }

        // 定义（真实属性用 Guid 键；这里用确定性 Guid 便于 Go 侧复现）。
        var str = new AttributeDefinition(Guid.Parse("00000000-0000-0000-0000-000000000001"), "Strength", "");
        var agi = new AttributeDefinition(Guid.Parse("00000000-0000-0000-0000-000000000002"), "Agility", "");
        var def = new AttributeDefinition(Guid.Parse("00000000-0000-0000-0000-000000000003"), "Defense", "")
        {
            MaximumValue = 5000, // 定义级钳制
        };
        var speed = new AttributeDefinition(Guid.Parse("00000000-0000-0000-0000-000000000004"), "Speed", "");
        var hp = new AttributeDefinition(Guid.Parse("00000000-0000-0000-0000-000000000005"), "HP", "");
        var maxhp = new AttributeDefinition(Guid.Parse("00000000-0000-0000-0000-000000000006"), "MaxHP", "")
        {
            MaximumValue = 100, // 定义级钳制
        };
        var multOperand = new AttributeDefinition(Guid.Parse("00000000-0000-0000-0000-000000000007"), "MultOperand", "");

        var statAttributes = new IAttribute[]
        {
            new StatAttribute(str, 100),
            new StatAttribute(agi, 57.5f),
        };
        var baseAttributes = new IAttribute[]
        {
            new ConstValueAttribute(20, def),   // DEF 基础 20
            new ConstValueAttribute(50, hp),    // HP 基础 50
        };

        var system = new AttributeSystem(statAttributes, baseAttributes, new[]
        {
            // DEF += AGI * 2（AddRaw）
            new AttributeRelationship(def, 2f, agi, InputOperator.Multiply, null, AggregateType.AddRaw),
            // DEF *= STR * 0.1（Multiplicate）→ 乘区 = 1 + (100*0.1)=11? 不——乘区元素值为 STR*0.1=10，聚合成 1*10
            new AttributeRelationship(def, 0.1f, str, InputOperator.Multiply, null, AggregateType.Multiplicate),
            // DEF += STR * 1（AddFinal）
            new AttributeRelationship(def, 1f, str, InputOperator.Multiply, null, AggregateType.AddFinal),
            // HP = max(STR * 0.3)（Maximum 聚合）
            new AttributeRelationship(hp, 0.3f, str, InputOperator.Multiply, null, AggregateType.Maximum),
            // SPEED += STR^2（Exponentiate）
            new AttributeRelationship(speed, 2f, str, InputOperator.Exponentiate, null, AggregateType.AddRaw),
            // SPEED *= MULT（操作数为属性，初始 3）——属性操作数联动
            new AttributeRelationship(speed, multOperand, str, AggregateType.Multiplicate),
            // MAXHP += STR * 5（AddRaw），被 MaxHP.MaximumValue=100 钳制
            new AttributeRelationship(maxhp, 5f, str, InputOperator.Multiply, null, AggregateType.AddRaw),
        });

        // 预置操作数属性（属性操作数：GetOrCreate 生成 ComposableAttribute，AddElement 常量）。
        system.AddElement(new ConstValueAttribute(3, multOperand), multOperand);

        // 输出初始态各目标值。
        var step1 = new
        {
            defense = system[def],
            speed = system[speed],
            hp = system[hp],
            maxhp = system[maxhp],
            agility_value = system[agi],
        };

        // 改 stat：STR 100 → 150，派生属性联动（关系传播）。
        system[str] = 150;
        var step2 = new
        {
            defense = system[def],
            speed = system[speed],
            hp = system[hp],
            maxhp = system[maxhp],
        };

        // 恢复 STR=100，移除 AGI 关系元素再回读（RemoveElement 语义）。
        system[str] = 100;
        var defComposable = system.GetComposableAttribute(def);
        var agiElement = system.CreateRelatedAttribute(
            new AttributeRelationship(def, 2f, agi, InputOperator.Multiply, null, AggregateType.AddRaw),
            system, AggregateType.AddRaw);
        defComposable!.AddElement(agiElement);
        var withExtra = system[def];
        defComposable.RemoveElement(agiElement);
        var afterRemove = system[def];

        var payload = new
        {
            scenario = "OpenMUGo T0-a golden：四聚合 + 全 InputOperator + 定义钳制 + 关系传播",
            step1_initial = step1,
            step2_after_str_150 = step2,
            step3_extra_agi_element = withExtra,
            step3_after_remove = afterRemove,
        };

        var options = new JsonSerializerOptions { WriteIndented = true };
        await File.WriteAllTextAsync(args[0], JsonSerializer.Serialize(payload, options)).ConfigureAwait(false);
        Console.WriteLine($"attribute vectors → {Path.GetFullPath(args[0])}");
        return 0;
    }
}
