// Tool goldenconfig：把 OpenMU 数据初始化（Persistence/Initialization）产出的完整
// GameConfiguration 导出为 Go 侧可消费的按域拆分 JSON（doc/10 T0-c / 防线 1 数据对齐）。
//
// 用法: goldenconfig <output-dir> [numberOfGameServers]
//   默认输出到当前目录；数据版本 = Season 6（与 OpenMUGo 的 MuMain 端点对应）。
//
// 导出即"最终形态"：CreateInitialDataAsync 完成后，配置已含全部更新插件的内容
// （原版约定：全新初始化已包含 Updates 的净效果，AddAllUpdateEntries 只补安装记录，
//   见 OpenMU DataInitializationBase / UpdatePlugInBase）。
//
// Go 侧对应载入器：OpenMUGo/internal/gamelogic/config（go:embed data/<version>/）。
//
// 用法示例（在 OpenMUGo 根目录）:
//   dotnet run --project tools/goldenconfig -- data/season6
namespace GoldenConfig;

using System.Diagnostics;
using System.Text.Json;
using Microsoft.Extensions.Logging;
using MUnique.OpenMU.AttributeSystem;
using MUnique.OpenMU.DataModel;
using MUnique.OpenMU.DataModel.Attributes;
using MUnique.OpenMU.DataModel.Configuration;
using MUnique.OpenMU.DataModel.Configuration.Items;
using MUnique.OpenMU.DataModel.Configuration.ItemCrafting;
using MUnique.OpenMU.DataModel.Configuration.Quests;
using MUnique.OpenMU.Interfaces;
using MUnique.OpenMU.Persistence;
using MUnique.OpenMU.Persistence.InMemory;
using MUnique.OpenMU.DataModel.Entities;
using MUnique.OpenMU.GameLogic;
using MUnique.OpenMU.GameServer.RemoteView;
using MUnique.OpenMU.Persistence.Initialization.Updates;
using MUnique.OpenMU.Persistence.Initialization.VersionSeasonSix;

internal static class Program
{
    // 与 OpenMU GameContext.DefaultExperienceFormula/DefaultMasterExperienceFormula 一致
    // （配置里公式为空时原版按此默认值求值；导出"生效公式"而非 null）。
    private const string DefaultExperienceFormula =
        "if(level == 0, 0, if(level < 256, 10 * (level + 8) * (level - 1) * (level - 1), (10 * (level + 8) * (level - 1) * (level - 1)) + (1000 * (level - 247) * (level - 256) * (level - 256))))";

    private const string DefaultMasterExperienceFormula =
        "(505 * level * level * level) + (35278500 * level) + (228045 * level * level)";

    private static readonly JsonSerializerOptions JsonOptions = new() { WriteIndented = false };

    private static async Task<int> Main(string[] args)
    {
        if (args.Length < 1)
        {
            Console.Error.WriteLine("usage: goldenconfig <output-dir> [numberOfGameServers]");
            return 1;
        }

        var outputDir = args[0];
        var numberOfServers = args.Length > 1 ? byte.Parse(args[1]) : (byte)1;
        Directory.CreateDirectory(outputDir);

        var loggerFactory = Microsoft.Extensions.Logging.Abstractions.NullLoggerFactory.Instance;

        Console.WriteLine("== goldenconfig: 初始化 Season6 数据（内存库，可能需要数分钟）==");
        var stopwatch = Stopwatch.StartNew();
        var provider = new InMemoryPersistenceContextProvider();
        var initialization = new DataInitialization(provider, loggerFactory);
        await initialization.CreateInitialDataAsync(numberOfServers, createTestAccounts: false).ConfigureAwait(false);
        Console.WriteLine($"初始化完成，耗时 {stopwatch.Elapsed.TotalSeconds:F1}s");

        var configContext = provider.CreateNewConfigurationContext();
        var gameConfiguration = (await configContext.GetAsync<GameConfiguration>().ConfigureAwait(false)).Single();
        Console.WriteLine($"GameConfiguration 载入: {gameConfiguration.Maps.Count} 地图 / " +
                          $"{gameConfiguration.Monsters.Count} 怪物 / {gameConfiguration.Items.Count} 物品 / " +
                          $"{gameConfiguration.Skills.Count} 技能");

        var totalSpawns = gameConfiguration.Maps.Sum(m => m.MonsterSpawns.Count);
        // TRIM-02：先收集配方与宿主（meta 需要计数，写文件在 55_drops 之后）。
        var craftingHosts = new Dictionary<ItemCrafting, List<MonsterDefinition>>();
        foreach (var monster in gameConfiguration.Monsters)
        {
            foreach (var crafting in monster.ItemCraftings)
            {
                if (!craftingHosts.TryGetValue(crafting, out var hosts))
                {
                    hosts = new List<MonsterDefinition>();
                    craftingHosts[crafting] = hosts;
                }
                hosts.Add(monster);
            }
        }
        // TRIM-03：任务定义（原版挂在 MonsterDefinition.Quests，一个 NPC 一个给予者）。
        var questHosts = new Dictionary<QuestDefinition, MonsterDefinition>();
        foreach (var monster in gameConfiguration.Monsters)
        {
            foreach (var quest in monster.Quests)
            {
                questHosts.TryAdd(quest, monster);
            }
        }
        // TRIM-09：大师树（结构与槽位索引都来自原版代码，见 96_master_skills.json 导出处的注释）。
        // MasterSkillRoot 只有 Guid + Name（初始化器里的 byte 键是局部的、不落库），而根的
        // 结构性用途只有 AddMasterPointAction.CheckRank 的"同根"分组（客户端槽位号本身已含
        // 根信息：左 1 / 中 37 / 右 73）。故按导出顺序给每根一个序号 index，id/name 仅便追溯。
        var masterRootIndex = new Dictionary<MasterSkillRoot, int>();
        var masterRoots = gameConfiguration.MasterSkillRoots
            .Select((r, i) =>
            {
                masterRootIndex[r] = i;
                return new { index = i, id = r.Id.ToString(), name = Ls(r.Name) };
            })
            .ToArray();
        var masterSkillDefs = gameConfiguration.Skills
            .Where(s => s.MasterDefinition is not null)
            .OrderBy(s => (int)s.Number)
            .ToArray();
        var masterClasses = gameConfiguration.CharacterClasses.Count(c => c.IsMasterClass);
        // TRIM-08：聊天命令表（见 CollectChatCommands 的注释）。
        var chatCommands = CollectChatCommands(gameConfiguration);
        var meta = new
        {
            schema_version = 1,
            data_initialization_id = DataInitialization.Id,
            data_initialization_caption = initialization.Caption,
            exported_at_utc = DateTime.UtcNow.ToString("o"),
            note = "由 OpenMUGo tools/goldenconfig 从 OpenMU Persistence/Initialization 内存初始化导出；口径见 doc/15 与 doc/10 T0-c。",
            counts = new
            {
                character_classes = gameConfiguration.CharacterClasses.Count,
                skills = gameConfiguration.Skills.Count,
                items = gameConfiguration.Items.Count,
                monsters = gameConfiguration.Monsters.Count,
                maps = gameConfiguration.Maps.Count,
                monster_spawns = totalSpawns,
                warps = gameConfiguration.WarpList.Count,
                craftings = craftingHosts.Count,
                quests = questHosts.Count,
                master_skills = masterSkillDefs.Length,
                master_skill_roots = masterRoots.Length,
                master_classes = masterClasses,
                chat_commands = chatCommands.Length,
            },
        };

        Write(outputDir, "00_meta.json", meta);
        // 05_game_config.json：GameConfiguration 全局标量（玩法常量），之前未导出、Go 侧硬编码。
        // （MaximumItemOptionLevelDrop / ExcellentItemDropLevelDelta 已在 42_item_options.json，不重复。）
        Write(outputDir, "05_game_config.json", new
        {
            maximum_level = (int)gameConfiguration.MaximumLevel,
            maximum_master_level = (int)gameConfiguration.MaximumMasterLevel,
            experience_rate = (double)gameConfiguration.ExperienceRate,
            master_experience_rate = (double)gameConfiguration.MasterExperienceRate,
            prevent_experience_overflow = gameConfiguration.PreventExperienceOverflow,
            minimum_monster_level_for_master_experience = (int)gameConfiguration.MinimumMonsterLevelForMasterExperience,
            info_range = (int)gameConfiguration.InfoRange,
            area_skill_hits_player = gameConfiguration.AreaSkillHitsPlayer,
            maximum_inventory_money = gameConfiguration.MaximumInventoryMoney,
            maximum_vault_money = gameConfiguration.MaximumVaultMoney,
            clamp_money_on_pickup = gameConfiguration.ClampMoneyOnPickup,
            recovery_interval = gameConfiguration.RecoveryInterval,
            maximum_letters = gameConfiguration.MaximumLetters,
            letter_send_price = gameConfiguration.LetterSendPrice,
            maximum_characters_per_account = (int)gameConfiguration.MaximumCharactersPerAccount,
            character_name_regex = gameConfiguration.CharacterNameRegex,
            maximum_password_length = gameConfiguration.MaximumPasswordLength,
            maximum_party_size = (int)gameConfiguration.MaximumPartySize,
            should_drop_money = gameConfiguration.ShouldDropMoney,
            item_drop_duration_ms = (long)gameConfiguration.ItemDropDuration.TotalMilliseconds,
            damage_per_one_item_durability = (double)gameConfiguration.DamagePerOneItemDurability,
            damage_per_one_pet_durability = (double)gameConfiguration.DamagePerOnePetDurability,
            hits_per_one_item_durability = (double)gameConfiguration.HitsPerOneItemDurability,
        });
        Write(outputDir, "10_character_classes.json", gameConfiguration.CharacterClasses.Select(c => new
        {
            number = (int)c.Number,
            name = Ls(c.Name),
            can_get_created = c.CanGetCreated,
            creation_allowed_flag = (int)c.CreationAllowedFlag,
            level_requirement_by_creation = (int)c.LevelRequirementByCreation,
            level_warp_requirement_reduction_percent = c.LevelWarpRequirementReductionPercent,
            fruit_calculation = (int)c.FruitCalculation,
            is_master_class = c.IsMasterClass,
            next_class = c.NextGenerationClass is null ? (int?)null : (int)c.NextGenerationClass.Number,
            home_map = c.HomeMap is null ? (int?)null : (int)c.HomeMap.Number,
            base_attributes = c.BaseAttributeValues.Select(b => new
            {
                designation = b.Definition?.Designation,
                value = b.Value,
                // 必须带聚合形态：护盾恢复倍率等含 Multiplicate 乘法项（100·AddRaw
                // 与 1/75000·Multiplicate）；漏导会被当成 AddRaw 相加、数值放大数千倍。
                aggregate_type = b.AggregateType.ToString(),
            }),
            stat_attributes = c.StatAttributes.Select(s => new
            {
                designation = s.Attribute?.Designation,
                base_value = s.BaseValue,
                increasable_by_player = s.IncreasableByPlayer,
            }),
            attribute_combinations = c.AttributeCombinations.Select(Rel),
            // TRIM-06：连击定义（只有 Blade Knight 显式配置；其余职业由游戏逻辑沿
            // NextGenerationClass 回溯继承，见 Player.cs:1610-1626 —— 故这里**只导本人持有的**，
            // 继承关系留给 Go 侧按同一算法解析，不把继承结果固化进数据）。
            combo_definition = c.ComboDefinition is null ? null : ComboExport(c.ComboDefinition),
        }));
        Write(outputDir, "20_experience.json", new
        {
            experience_formula = gameConfiguration.ExperienceFormula ?? DefaultExperienceFormula,
            maximum_level = (int)gameConfiguration.MaximumLevel,
            master_experience_formula = gameConfiguration.MasterExperienceFormula ?? DefaultMasterExperienceFormula,
            maximum_master_level = (int)gameConfiguration.MaximumMasterLevel,
            // 按原版 GameContext.CreateExpTable 同一公式逐级求值，Go 侧直接消费表（公式留存备查）。
            table = CreateExpTable(gameConfiguration.ExperienceFormula ?? DefaultExperienceFormula, gameConfiguration.MaximumLevel),
            master_table = CreateExpTable(gameConfiguration.MasterExperienceFormula ?? DefaultMasterExperienceFormula, gameConfiguration.MaximumMasterLevel),
        });
        Write(outputDir, "30_skills.json", gameConfiguration.Skills.Select(s => new
        {
            number = (int)s.Number,
            name = Ls(s.Name),
            attack_damage = s.AttackDamage,
            range = (int)s.Range,
            implicit_target_range = (int)s.ImplicitTargetRange,
            hits_per_attack = (int)s.NumberOfHitsPerAttack,
            moves_to_target = s.MovesToTarget,
            moves_target = s.MovesTarget,
            qualified_classes = s.QualifiedCharacters.Select(c => (int)c.Number),
            // T2-11：类型/目标/伤害类型 + 消耗（ConsumeRequirements 即"施放时扣除"）+
            // 挂接的 MagicEffect 号（buff/异常状态挂接用）。
            skill_type = (int)s.SkillType,
            target = (int)s.Target,
            target_restriction = (int)s.TargetRestriction,
            damage_type = (int)s.DamageType,
            consume = s.ConsumeRequirements.Select(r => new
            {
                attribute = r.Attribute is null ? null : r.Attribute.Designation,
                value = r.MinimumValue,
            }),
            magic_effect_number = s.MagicEffectDef is null ? (int?)null : (int)s.MagicEffectDef.Number,
            // 优先级 1 补全（影响已实现系统的数值/判定）：
            // 施放前置要求（原版 Skill.Requirements：等级等 AttributeRequirement）。
            requirements = s.Requirements.Select(r => new
            {
                attribute = r.Attribute is null ? null : r.Attribute.Designation,
                value = r.MinimumValue,
            }),
            // 技能伤害按属性缩放的派生关系（原版 Skill.AttributeRelationships，如骑术/Lance）。
            attribute_relationships = s.AttributeRelationships.Select(Rel),
            // 元素克制：命中目标可能附加对应元素效果；255 表示对该元素免疫。
            elemental_modifier_target = s.ElementalModifierTarget is null ? null : s.ElementalModifierTarget.Designation,
            skip_elemental_modifier = s.SkipElementalModifier,
        }));
        Write(outputDir, "40_items.json", gameConfiguration.Items.Select(i => new
        {
            group = (int)i.Group,
            number = (int)i.Number,
            name = Ls(i.Name),
            slot = i.ItemSlot is null ? (int?)null : i.ItemSlot.ItemSlots.FirstOrDefault(),
            // T2-5：槽位判定必须用**完整** ItemSlots 集合（MoveItemAction.CanMoveAsync 的
            // `ItemSlot.ItemSlots.Contains(toSlot)`）。`slot` 只留 FirstOrDefault，是**有损**的
            // ——"Left or Right Hand" 的 {0,1} 会被压成 0，用它校验会把右手武器误拒。
            slots = i.ItemSlot is null ? (int[]?)null : i.ItemSlot.ItemSlots.OrderBy(s => s).ToArray(),
            slot_description = Ls(i.ItemSlot?.Description),
            width = (int)i.Width,
            height = (int)i.Height,
            drop_level = (int)i.DropLevel,
            maximum_drop_level = i.MaximumDropLevel is null ? (int?)null : (int)i.MaximumDropLevel.Value,
            maximum_item_level = (int)i.MaximumItemLevel,
            durability = (int)i.Durability,
            value = i.Value,
            maximum_sockets = i.MaximumSockets,
            drops_from_monsters = i.DropsFromMonsters,
            is_ammunition = i.IsAmmunition,
            is_bound_to_character = i.IsBoundToCharacter,
            is_quest_item = i.IsQuestItem,
            storage_limit_per_character = i.StorageLimitPerCharacter,
            has_skill = i.Skill is not null,
            // 技能书/卷轴（group15）对应的技能号：右键使用时据此学习
            // （LearnablesConsumeHandlerPlugIn）。武器自带技能也走此映射。
            skill_number = i.Skill is null ? (int?)null : (int)i.Skill.Number,
            // T2-5：CompliesRequirements 的输入（PlayerItemExtensions.CompliesRequirements
            // 逐条比对 player.Attributes[attr] 与 item.GetRequirement(...) 的值）。
            requirements = i.Requirements
                .Select(r => new { attribute = r.Attribute == null ? null : r.Attribute.Designation, value = r.MinimumValue })
                .ToArray(),
            qualified_classes = i.QualifiedCharacters.Select(c => (int)c.Number).OrderBy(n => n).ToArray(),
            // 装备 PowerUp（ItemPowerUpFactory.GetBasePowerUpWrappers 的数据源）：
            // 穿上后加成目标属性，基础值 + 每级加成表（关联 45_item_level_bonus）。
            // 缺此导出则装备只改外观、伤害/防御加成不进属性系统。
            base_power_up_attributes = i.BasePowerUpAttributes.Select(p => new
            {
                target = p.TargetAttribute is null ? null : p.TargetAttribute.Designation,
                base_value = p.BaseValue,
                aggregate_type = p.AggregateType.ToString(),
                bonus_table = p.BonusPerLevelTable is null ? null : Ls(p.BonusPerLevelTable.Name),
            }).ToArray(),
            // ConsumeEffect：使用该物品时产生的魔法效果编号（酒=201 攻速等；
            // 对照 ApplyMagicEffectConsumeHandlerPlugIn）。null = 普通恢复/无效果。
            consume_effect = i.ConsumeEffect is null ? (int?)null : (int)i.ConsumeEffect.Number,
        }));
        // T2-12：物品选项数据面（选项类型 + 定义 + 等级表 + 物品引用 + 组合奖励 + 远古套装组）。
        // 一次性导出**全部**选项定义，避免后续每接一种选项（幸运/普通/卓越/远古/和谐/镶嵌/
        // 守护/翅膀/宠物）都要重跑导出器。消费端形状见 internal/gamelogic/config 的 ItemOption*。
        var optionDefinitions = new List<ItemOptionDefinition>();
        var seenOptionDefs = new HashSet<ItemOptionDefinition>();
        foreach (var def in gameConfiguration.ItemOptions)
        {
            if (def is not null && seenOptionDefs.Add(def))
            {
                optionDefinitions.Add(def);
            }
        }

        // 远古套装组的 Options 未必都登记在全局表里：一并纳入，保证 set_groups 的引用可解析。
        foreach (var setGroup in gameConfiguration.ItemSetGroups)
        {
            if (setGroup.Options is { } setGroupOptions && seenOptionDefs.Add(setGroupOptions))
            {
                optionDefinitions.Add(setGroupOptions);
            }
        }

        Write(outputDir, "42_item_options.json", new
        {
            // 掉落随机选项用的两个配置值（对照 DefaultDropGenerator 的
            // _maxItemOptionLevelDrop / _excellentItemDropLevelDelta）。
            maximum_item_option_level_drop = (int)gameConfiguration.MaximumItemOptionLevelDrop,
            excellent_item_drop_level_delta = (int)gameConfiguration.ExcellentItemDropLevelDelta,
            option_types = gameConfiguration.ItemOptionTypes.Select(t => new
            {
                id = t.Id.ToString(),
                // kind 是稳定的语义标识（Go 侧按它区分幸运/普通/卓越/…，不做名称匹配）。
                kind = OptionTypeKind(t),
                name = Ls(t.Name),
                description = Ls(t.Description),
                is_visible = t.IsVisible,
            }),
            definitions = optionDefinitions.Select(ItemOptionDefinitionExport),
            // 物品 → 候选选项定义（原版 ItemDefinition.PossibleItemOptions）；只导非空项。
            items = gameConfiguration.Items
                .Where(i => i.PossibleItemOptions.Count > 0)
                .Select(i => new
                {
                    group = (int)i.Group,
                    number = (int)i.Number,
                    option_definitions = i.PossibleItemOptions.Select(OptionDefId).ToArray(),
                }),
            // 组合奖励（镶嵌套装包、Fenrir 移速等）：按"若干选项类型 + 最小数量"给加成。
            combination_bonuses = gameConfiguration.ItemOptionCombinationBonuses.Select(b => new
            {
                number = b.Number,
                description = Ls(b.Description),
                applies_multiple_times = b.AppliesMultipleTimes,
                requirements = b.Requirements.Select(r => new
                {
                    option_type = r.OptionType is null ? null : r.OptionType.Id.ToString(),
                    sub_option_type = r.SubOptionType,
                    minimum_count = r.MinimumCount,
                }).ToArray(),
                bonus = PowerUpDefinitionExport(b.Bonus),
            }),
            // 套装组（远古套装 + 防御率套装 + 等级套装）：Options 为套装加成定义，
            // items 里的 bonus_option 是"该件物品在套装中额外携带的远古属性"。
            set_groups = gameConfiguration.ItemSetGroups.Select(g => new
            {
                name = Ls(g.Name),
                always_applies = g.AlwaysApplies,
                count_distinct = g.CountDistinct,
                minimum_item_count = g.MinimumItemCount,
                set_level = g.SetLevel,
                option_definition = g.Options is null ? null : OptionDefId(g.Options),
                items = g.Items.Select(it => new
                {
                    group = it.ItemDefinition is null ? (int?)null : (int)it.ItemDefinition.Group,
                    number = it.ItemDefinition is null ? (int?)null : (int)it.ItemDefinition.Number,
                    ancient_set_discriminator = it.AncientSetDiscriminator,
                    bonus_option = it.BonusOption is null ? null : ItemOptionExport(it.BonusOption),
                }).ToArray(),
            }),
        });
        Write(outputDir, "45_item_level_bonus.json", gameConfiguration.ItemLevelBonusTables.Select(t => new
        {
            name = Ls(t.Name),
            bonus_per_level = t.BonusPerLevel.Select(b => new { level = b.Level, additional_value = b.AdditionalValue }),
        }));
        // T1-6：掉落组（DefaultDropGenerator 的输入）：怪物级 + 地图级（Season6 的主掉落挂在地图上）。
        Write(outputDir, "55_drops.json", new
        {
            monster_drops = gameConfiguration.Monsters
                .Where(m => m.DropItemGroups.Count > 0)
                .Select(m => new
                {
                    monster = (int)m.Number,
                    groups = m.DropItemGroups.Select(DropGroupExport),
                }),
            map_drops = gameConfiguration.Maps
                .Where(mp => mp.DropItemGroups.Count > 0)
                .Select(mp => new
                {
                    map = (int)mp.Number,
                    discriminator = mp.Discriminator,
                    groups = mp.DropItemGroups.Select(DropGroupExport),
                }),
        });
        // TRIM-02：合成配方（原版挂在 MonsterDefinition.ItemCraftings：ChaosMixes.cs 35 条 +
        // SocketSystem.cs 4 条）。handler = ItemCraftingHandlerClassName（空即 SimpleItemCraftingHandler）。
        // additions_* 允许为负（原版字段是 int，求值后才转 byte），必须原样导出。
        Write(outputDir, "90_crafting.json", new
        {
            craftings = craftingHosts
                .OrderBy(pair => (int)pair.Key.Number)
                .Select(pair => CraftingExport(pair.Key, pair.Value))
                .ToArray(),
        });
        Console.WriteLine($"  配方 {craftingHosts.Count} 条（S6 期望 39）");

        // S-3 后续（宝石升档合成/降档拆分）：GameConfiguration.JewelMixes 是 C1 BC 的
        // ItemType(=mix Number) → 单宝石/打包宝石 映射，源在 VersionSeasonSix/
        // GameConfigurationInitializer.CreateJewelMixes（10 条）。
        Write(outputDir, "98_jewel_mixes.json", gameConfiguration.JewelMixes
            .OrderBy(m => (int)m.Number)
            .Select(m => new
            {
                number = (int)m.Number,
                single = new { group = (int)m.SingleJewel!.Group, number = (int)m.SingleJewel.Number },
                mixed = new { group = (int)m.MixedJewel!.Group, number = (int)m.MixedJewel.Number },
            })
            .ToArray());
        Console.WriteLine($"  宝石合成 {gameConfiguration.JewelMixes.Count()} 条（S6 期望 10）");

        // TRIM-03：任务定义。原版 Quests.cs 用 CreateQuest(483) + 16 个 legacy 方法建 499 条，
        // 一律挂到给予者 NPC 的 MonsterDefinition.Quests（GameConfiguration 上没有 Quests 集合）。
        // 奖励的 item/attribute/skill 引用一并导出为**定义面标识**（Go 侧按 id/designation/group+number 解析）。
        Write(outputDir, "95_quests.json", new
        {
            quests = questHosts
                .OrderBy(pair => (int)pair.Key.Group)
                .ThenBy(pair => (int)pair.Key.Number)
                .Select(pair => QuestExport(pair.Key, pair.Value))
                .ToArray(),
        });
        Console.WriteLine($"  任务 {questHosts.Count} 条（S6 期望 499）");

        // TRIM-09：大师技能树。结构在 SkillsInitializer.InitializeMasterSkillData（挂在
        // Skill.MasterDefinition 上），客户端槽位号在 GameServer/RemoteView/MasterSkillExtensions
        // 的硬编码表里（原版注释：Webzen 不让客户端自己按技能号定位）。
        // 数值本是 MathParser 公式（GameLogic/MasterSkillExtensions.GetValue 带进程级缓存），
        // 这里**用原版方法在导出期逐级求值**，Go 运行期查表——口径与偏差登记见 OpenMUGo doc/16。
        Write(outputDir, "96_master_skills.json", new
        {
            roots = masterRoots,
            skills = masterSkillDefs.Select(s => MasterSkillExport(s, masterRootIndex)).ToArray(),
            // 只有大师职业有树（原版 GetMasterSkillIndex 按 CharacterClass.Number 查表）。
            tree_indices = gameConfiguration.CharacterClasses
                .Where(c => c.IsMasterClass)
                .OrderBy(c => (int)c.Number)
                .Select(c => new
                {
                    @class = (int)c.Number,
                    slots = masterSkillDefs
                        .Select(s => new { skill = (int)s.Number, index = (int)s.GetMasterSkillIndex(c) })
                        .Where(x => x.index > 0)
                        .OrderBy(x => x.skill)
                        .ToArray(),
                })
                .ToArray(),
        });
        Console.WriteLine($"  大师技能 {masterSkillDefs.Length} 条 / 槽位表 {gameConfiguration.CharacterClasses.Count(c => c.IsMasterClass)} 个大师职业");

        // TRIM-08：聊天命令表（名字/描述/参数在原版是代码特性 + 资源串，不在 GameConfiguration 里）。
        Write(outputDir, "97_chat_commands.json", chatCommands);
        Console.WriteLine($"  聊天命令 {chatCommands.Length} 条（带 [ChatCommandHelp] 的插件类型）");

        // T2-9：NPC 商店（MerchantNpc 的 MerchantStore，原版 NpcInitialization.Create*Store）。
        // 物品只导出**实体面**字段（slot/level/durability/技能/幸运/选项等级），
        // 定义面（value/drop_level/width…）由 40_items.json 按 (group, number) 关联。
        Write(outputDir, "85_merchant_stores.json", gameConfiguration.Monsters
            .Where(m => m.MerchantStore is { Items.Count: > 0 })
            .OrderBy(m => m.Number)
            .Select(m => new
            {
                npc = (int)m.Number,
                name = Ls(m.Designation),
                items = m.MerchantStore.Items.OrderBy(i => i.ItemSlot).Select(i => new
                {
                    slot = (int)i.ItemSlot,
                    group = i.Definition is null ? (int?)null : (int)i.Definition.Group,
                    number = i.Definition is null ? (int?)null : (int)i.Definition.Number,
                    level = (int)i.Level,
                    durability = (int)i.Durability,
                    has_skill = i.HasSkill,
                    luck = i.ItemOptions.Any(o => o.ItemOption?.OptionType == ItemOptionTypes.Luck),
                    option_level = (int?)i.ItemOptions.FirstOrDefault(o => o.ItemOption?.OptionType == ItemOptionTypes.Option)?.Level ?? 0,
                }),
            }));
        Write(outputDir, "50_monsters.json", gameConfiguration.Monsters.Select(m => new
        {
            number = (int)m.Number,
            name = Ls(m.Designation),
            object_kind = m.ObjectKind.ToString(),
            npc_window = (int)m.NpcWindow,
            attribute = (int)m.Attribute,
            move_range = (int)m.MoveRange,
            attack_range = (int)m.AttackRange,
            view_range = (int)m.ViewRange,
            move_delay_ms = (long)m.MoveDelay.TotalMilliseconds,
            attack_delay_ms = (long)m.AttackDelay.TotalMilliseconds,
            respawn_delay_ms = (long)m.RespawnDelay.TotalMilliseconds,
            number_of_maximum_item_drops = m.NumberOfMaximumItemDrops,
            intelligence = m.IntelligenceTypeName,
            attack_skill = m.AttackSkill is null ? (int?)null : (int)m.AttackSkill.Number,
            attributes = m.Attributes.Select(a => new
            {
                designation = a.AttributeDefinition?.Designation,
                value = a.Value,
            }),
        }));
        Write(outputDir, "60_maps.json", gameConfiguration.Maps.Select(mp => new
        {
            number = (int)mp.Number,
            discriminator = mp.Discriminator,
            name = Ls(mp.Name),
            exp_multiplier = (double)mp.ExpMultiplier,
            // 进图条件（原版 MapRequirements，如特殊状态/等级）与地图内全局光环加成。
            requirements = mp.MapRequirements.Select(r => new
            {
                attribute = r.Attribute is null ? null : r.Attribute.Designation,
                value = r.MinimumValue,
            }),
            character_power_ups = mp.CharacterPowerUpDefinitions.Select(PowerUpDefinitionExport).ToArray(),
            safezone_map = mp.SafezoneMap is null ? (int?)null : (int)mp.SafezoneMap.Number,
            // TerrainData = 原始 .att 文件内容（base64）；解析器在 Go 侧 T1-d 移植。
            terrain = mp.TerrainData is null ? null : Convert.ToBase64String(mp.TerrainData),
            spawns = mp.MonsterSpawns.Select(sp => new
            {
                monster = sp.MonsterDefinition is null ? (int?)null : (int)sp.MonsterDefinition.Number,
                x1 = (int)sp.X1,
                y1 = (int)sp.Y1,
                x2 = (int)sp.X2,
                y2 = (int)sp.Y2,
                quantity = (int)sp.Quantity,
                direction = (int)sp.Direction,
                trigger = sp.SpawnTrigger.ToString(),
                wave = (int)sp.WaveNumber,
            }),
            enter_gates = mp.EnterGates.Select(g => new
            {
                number = (int)g.Number,
                x1 = (int)g.X1,
                y1 = (int)g.Y1,
                x2 = (int)g.X2,
                y2 = (int)g.Y2,
                level_requirement = (int)g.LevelRequirement,
                target = g.TargetGate is null ? null : new
                {
                    map = g.TargetGate.Map is null ? (int?)null : (int)g.TargetGate.Map.Number,
                    x1 = (int)g.TargetGate.X1,
                    y1 = (int)g.TargetGate.Y1,
                    x2 = (int)g.TargetGate.X2,
                    y2 = (int)g.TargetGate.Y2,
                    direction = (int)g.TargetGate.Direction,
                    is_spawn_gate = g.TargetGate.IsSpawnGate,
                },
            }),
            exit_gates = mp.ExitGates.Select(g => new
            {
                x1 = (int)g.X1,
                y1 = (int)g.Y1,
                x2 = (int)g.X2,
                y2 = (int)g.Y2,
                direction = (int)g.Direction,
                is_spawn_gate = g.IsSpawnGate,
            }),
        }));
        Write(outputDir, "70_warps.json", gameConfiguration.WarpList.Select(w => new
        {
            index = w.Index,
            name = Ls(w.Name),
            costs = w.Costs,
            level_requirement = w.LevelRequirement,
            gate = w.Gate is null ? null : new
            {
                map = w.Gate.Map is null ? (int?)null : (int)w.Gate.Map.Number,
                x1 = (int)w.Gate.X1,
                y1 = (int)w.Gate.Y1,
                x2 = (int)w.Gate.X2,
                y2 = (int)w.Gate.Y2,
                direction = (int)w.Gate.Direction,
            },
        }));
        Write(outputDir, "35_magic_effects.json", gameConfiguration.MagicEffects.Select(MagicEffectExport));
        Write(outputDir, "80_attributes.json", new
        {
            // 全局属性定义全集（Go 侧按 designation 为键；原版 Id 为 Guid，一并导出备查）。
            attributes = gameConfiguration.Attributes.Select(a => new
            {
                id = a.Id.ToString(),
                designation = a.Designation,
                description = a.Description,
                maximum_value = a.MaximumValue,
            }),
            // 全局派生关系（目标/输入/操作数均以 designation 引用）。
            global_combinations = gameConfiguration.GlobalAttributeCombinations.Select(Rel),
        });

        // 区域技能形状设置 Skill.AreaSkillSettings 由"配置更新插件"追加，全新初始化
        // (CreateInitialDataAsync) 本身不含——原版靠 DataUpdateService 在运行时按管理员操作
        // 应用。为"一次性覆盖全部技能"，这里显式应用这 4 个 AreaSkillSettings 相关更新插件
        // （含 AddAreaSkillSettings 内部把基础技能设置复制给大师/强化技能的 ReplacedSkill 循环），
        // 再把 area_skill_settings 单独导出到 32_area_skill_settings.json。其余文件已按"全新初始化"
        // 写出，其标量不受这些插件影响。顺序：先建基础设置，再补 EffectRange/大师继承。
        IConfigurationUpdatePlugIn[] areaUpdates =
        {
            new AddAreaSkillSettingsUpdatePlugIn(),
            new FixSummonerCurseSkillsPlugIn(),
            new FixAreaSkillsUpdatePlugIn(),
            new FinishDarkLordMasterTreePlugIn(),
        };
        foreach (var update in areaUpdates)
        {
            await update.ApplyUpdateAsync(configContext, gameConfiguration).ConfigureAwait(false);
            Console.WriteLine($"  已应用区域技能更新插件: {update.Name}");
        }

        Write(outputDir, "32_area_skill_settings.json", gameConfiguration.Skills
            .Where(s => s.AreaSkillSettings is not null)
            .OrderBy(s => s.Number)
            .Select(s => new { number = (int)s.Number, area = AreaSkillExport(s.AreaSkillSettings) }));

        Console.WriteLine($"导出完成 → {Path.GetFullPath(outputDir)}");
        return 0;
    }

    private static string Ls(LocalizedString? s) => s?.ToString() ?? string.Empty;

    // AreaSkillExport 把 Skill.AreaSkillSettings 投影为导出形态（字段名与 Go
    // config.AreaSkillSettings 的 json tag 对齐；TimeSpan 以毫秒存）。
    private static object AreaSkillExport(AreaSkillSettings? s) => new
    {
        use_frustum_filter = s!.UseFrustumFilter,
        frustum_start_width = (double)s.FrustumStartWidth,
        frustum_end_width = (double)s.FrustumEndWidth,
        frustum_distance = (double)s.FrustumDistance,
        use_target_area_filter = s.UseTargetAreaFilter,
        target_area_diameter = (double)s.TargetAreaDiameter,
        effect_range = s.EffectRange,
        min_hits_per_target = s.MinimumNumberOfHitsPerTarget,
        max_hits_per_target = s.MaximumNumberOfHitsPerTarget,
        min_hits_per_attack = s.MinimumNumberOfHitsPerAttack,
        max_hits_per_attack = s.MaximumNumberOfHitsPerAttack,
        hit_chance_per_distance_multiplier = (double)s.HitChancePerDistanceMultiplier,
        use_deferred_hits = s.UseDeferredHits,
        delay_per_one_distance_ms = (long)s.DelayPerOneDistance.TotalMilliseconds,
        delay_between_hits_ms = (long)s.DelayBetweenHits.TotalMilliseconds,
        projectile_count = s.ProjectileCount,
    };

    // MagicEffectExport 把 MagicEffectDefinition 投影为导出形态（buff 生产者数据源）。
    private static object MagicEffectExport(MagicEffectDefinition e) => new
    {
        number = (int)e.Number,
        name = Ls(e.Name),
        sub_type = (int)e.SubType,
        inform_observers = e.InformObservers,
        stop_by_death = e.StopByDeath,
        send_duration = e.SendDuration,
        duration = EffectValue(e.Duration),
        chance = e.Chance is null ? null : EffectValue(e.Chance),
        // 持续时长按目标等级缩放（原版 DurationDependsOnTargetLevel + 两个除数）。
        duration_depends_on_target_level = e.DurationDependsOnTargetLevel,
        monster_target_level_divisor = (double)e.MonsterTargetLevelDivisor,
        player_target_level_divisor = (double)e.PlayerTargetLevelDivisor,
        power_up_definitions = e.PowerUpDefinitions.Select(p => new
        {
            target_id = p.TargetAttribute is null ? null : p.TargetAttribute.Id.ToString(),
            target = p.TargetAttribute?.Designation,
            boost = EffectValue(p.Boost),
        }).ToArray(),
        // TRIM-06：PvP 三元组。原版在 PvP 时按"非空则整体替换"选取
        // （MagicEffectPowerUpExtensions.cs:51 与 :343-355 的 duration/chance/powerUps 三处），
        // 未设置时回落到上面的 PvE 值 —— 所以这里**照原样导出可空**，回落逻辑留给 Go。
        chance_pvp = e.ChancePvp is null ? null : EffectValue(e.ChancePvp),
        duration_pvp = e.DurationPvp is null ? null : EffectValue(e.DurationPvp),
        power_up_definitions_pvp = e.PowerUpDefinitionsPvp.Select(p => new
        {
            target_id = p.TargetAttribute is null ? null : p.TargetAttribute.Id.ToString(),
            target = p.TargetAttribute?.Designation,
            boost = EffectValue(p.Boost),
        }).ToArray(),
    };

    // ComboExport 把 SkillComboDefinition 投影为导出形态（TRIM-06）。
    // 一条 step = (技能, 顺序, 是否终结步)；同一顺序可有多条（"这套里的任意一招"）。
    private static object ComboExport(SkillComboDefinition d) => new
    {
        name = Ls(d.Name),
        maximum_completion_ms = (long)d.MaximumCompletionTime.TotalMilliseconds,
        steps = d.Steps
            .OrderBy(s => s.Order)
            .ThenBy(s => s.Skill is null ? 0 : (int)s.Skill.Number)
            .Select(s => new
            {
                skill = s.Skill is null ? (int?)null : (int)s.Skill.Number,
                skill_name = s.Skill is null ? string.Empty : Ls(s.Skill.Name),
                order = s.Order,
                is_final_step = s.IsFinalStep,
            })
            .ToArray(),
    };

    // CollectChatCommands 导出聊天命令表（TRIM-08）。
    // 原版这份元数据不在 GameConfiguration 里，而是**代码特性 + 本地化资源**：
    // [ChatCommandHelp]（命令串、参数类型、最小角色状态）、[Argument]/[ValidValues]（参数名、
    // shortName、必填、枚举值）、[Display](PlugInResources) 的名字与描述，运行期由反射与
    // PlugInManager 组合。Go 没有反射式插件容器 → 在这里调原版**公开的**
    // ChatCommandTypeExtensions.TryCreateChatCommandInfo 求值后落盘，Go 运行期查表
    // （与 96_master_skills.json 同一做法）。没有 [ChatCommandHelp] 的插件类型原版也不进
    // 可用列表（GetAvailableCommands 的 `Help is { }` 过滤），此处同样跳过。
    // 语言取 InvariantCulture（= PlugInResources 的中性/默认英文），保证跨机器可复现。
    // **激活状态也在表里**：原版 GetAvailableCommands / GetStrategy 都只看 PlugInManager 里
    // "已激活"的插件（PlugInManager.cs:275-278），而初始化按
    // `IsActive = !typeof(IDisabledByDefault).IsAssignableFrom(type)` 落库
    // （DataInitializationBase.cs:135-138）——65 条里有 22 条（/addstr 一族、/get*/set* 一族、
    // /clearinv /npc /openware）默认**停用**：既不进 F5 列表，也不能执行。故这里直接读
    // GameConfiguration.PlugInConfigurations 的 isActive，而不是按类型猜。
    private static object[] CollectChatCommands(GameConfiguration gameConfiguration)
    {
        var marker = typeof(MUnique.OpenMU.GameLogic.PlugIns.ChatCommands.IChatCommandPlugIn);
        var activeByType = gameConfiguration.PlugInConfigurations
            .GroupBy(c => c.TypeId)
            .ToDictionary(g => g.Key, g => g.First().IsActive);
        var rows = new List<(string Command, object Payload)>();
        var seen = new HashSet<string>(StringComparer.Ordinal);
        foreach (var type in marker.Assembly.GetTypes()
                     .Where(t => t is { IsClass: true, IsAbstract: false } && marker.IsAssignableFrom(t))
                     .OrderBy(t => t.FullName, StringComparer.Ordinal))
        {
            var info = MUnique.OpenMU.GameLogic.PlugIns.ChatCommands.ChatCommandTypeExtensions
                .TryCreateChatCommandInfo(type, System.Globalization.CultureInfo.InvariantCulture);
            if (info is null)
            {
                continue;
            }
            if (!seen.Add(info.Command))
            {
                throw new InvalidOperationException($"聊天命令 {info.Command} 由多个类型声明（{type.FullName}）");
            }
            if (!activeByType.TryGetValue(type.GUID, out var isActive))
            {
                throw new InvalidOperationException(
                    $"聊天命令 {info.Command} 的类型 {type.FullName} 没有插件配置记录（TypeId={type.GUID}）");
            }
            rows.Add((info.Command, new
            {
                command = info.Command,
                // 原版 ChatCommandInfo.MinimumCharacterStatus；S2C F5 01 的第 7 字节。
                minimum_character_status = (int)info.MinimumCharacterStatus,
                enabled_by_default = isActive,
                name = info.Name ?? string.Empty,
                description = info.Description ?? string.Empty,
                usage = info.Usage ?? string.Empty,
                // type_name 保留原样（Boolean / 各整数类型 / String…），
                // 到 wire 的 ChatCommandParameterType 映射属视图层（对照 ChatCommandListViewPlugIn.GetParameterType）。
                parameters = info.Parameters.Select(p => new
                {
                    name = p.Name,
                    short_name = p.ShortName ?? string.Empty,
                    type_name = p.TypeName,
                    is_required = p.IsRequired,
                    valid_values = string.Join('|', p.ValidValues),
                }).ToArray(),
            }));
        }
        return rows.OrderBy(r => r.Command, StringComparer.Ordinal).Select(r => r.Payload).ToArray();
    }

    // EffectValue 把 PowerUpDefinitionValue（常量 + 关系值）投影为导出形态，
    // 对照 AttributeSystemExtensions.CreateElement：结果值 = ConstantValue（聚合
    // 形态）+ Σ RelatedValues（输入属性 ⊗ 操作数）。
    private static object EffectValue(PowerUpDefinitionValue? v)
    {
        var constant = v?.ConstantValue;
        return new
        {
            constant = constant?.Value ?? 0f,
            aggregate_type = constant?.AggregateType.ToString() ?? AggregateType.AddRaw.ToString(),
            related = v?.RelatedValues.Select(Rel).ToArray() ?? Array.Empty<object>(),
            maximum_value = v?.MaximumValue,
        };
    }

    // OptionTypeKind 给选项类型一个稳定的语义标识（Go 侧按它区分幸运/普通/卓越/远古/
    // 和谐/镶嵌/守护/翅膀/宠物，而不是按 Name 字符串匹配）。原版 ItemOptionTypes 的静态
    // 实例 Id 是硬编码常量，且 CreateItemOptionTypes 会把它复制进配置，故按 Id 比较即可。
    private static string OptionTypeKind(ItemOptionType t)
    {
        if (t.Id == ItemOptionTypes.Luck.Id) return "Luck";
        if (t.Id == ItemOptionTypes.Option.Id) return "Option";
        if (t.Id == ItemOptionTypes.Excellent.Id) return "Excellent";
        if (t.Id == ItemOptionTypes.Wing.Id) return "Wing";
        if (t.Id == ItemOptionTypes.AncientOption.Id) return "AncientOption";
        if (t.Id == ItemOptionTypes.AncientBonus.Id) return "AncientBonus";
        if (t.Id == ItemOptionTypes.HarmonyOption.Id) return "HarmonyOption";
        if (t.Id == ItemOptionTypes.SocketOption.Id) return "SocketOption";
        if (t.Id == ItemOptionTypes.SocketBonusOption.Id) return "SocketBonusOption";
        if (t.Id == ItemOptionTypes.GuardianOption.Id) return "GuardianOption";
        if (t.Id == ItemOptionTypes.BlueFenrir.Id) return "BlueFenrir";
        if (t.Id == ItemOptionTypes.BlackFenrir.Id) return "BlackFenrir";
        if (t.Id == ItemOptionTypes.GoldFenrir.Id) return "GoldFenrir";
        if (t.Id == ItemOptionTypes.DarkHorse.Id) return "DarkHorse";
        return string.Empty;
    }

    // OptionDefId 取选项定义的标识。ItemOptionDefinition 在 DataModel 层没有暴露 Id
    // （IIdentifiable 只由持久化层的子类实现），运行时对象是 BasicModel 子类，故按接口取；
    // 取不到时返回空串（Go 侧会因 id 重复/为空而校验失败，不会静默错位）。
    private static string OptionDefId(ItemOptionDefinition d)
        => d is MUnique.OpenMU.Persistence.IIdentifiable identifiable ? identifiable.Id.ToString() : string.Empty;

    // ItemOptionDefinitionExport 把选项定义投影为导出形态（候选选项 + 各自等级表）。
    private static object ItemOptionDefinitionExport(ItemOptionDefinition d) => new
    {
        id = OptionDefId(d),
        name = Ls(d.Name),
        adds_randomly = d.AddsRandomly,
        add_chance = d.AddChance,
        maximum_options_per_item = d.MaximumOptionsPerItem,
        possible_options = d.PossibleOptions.Select(ItemOptionExport).ToArray(),
    };

    // ItemOptionExport 把可增长选项投影为导出形态。
    // level_type 决定等级取自**选项等级**（普通/和谐/镶嵌，OptionLevel）还是**物品等级**
    // （翅膀，ItemLevel），消费端查 LevelDependentOptions 时不能混用。
    private static object ItemOptionExport(IncreasableItemOption o) => new
    {
        number = o.Number,
        option_type = o.OptionType is null ? null : o.OptionType.Id.ToString(),
        sub_option_type = o.SubOptionType,
        level_type = o.LevelType.ToString(),
        weight = (int)o.Weight,
        power_up = PowerUpDefinitionExport(o.PowerUpDefinition),
        level_dependent_options = o.LevelDependentOptions
            .OrderBy(l => l.Level)
            .Select(l => new
            {
                level = l.Level,
                required_item_level = l.RequiredItemLevel,
                power_up = PowerUpDefinitionExport(l.PowerUpDefinition),
            })
            .ToArray(),
    };

    // PowerUpDefinitionExport 把选项加成投影为 {target_id,target,boost}，与魔法效果同一形状
    // （boost 必须带 aggregate_type：Multiplicate 与 AddRaw 混淆会把数值放大数千倍）。
    private static object? PowerUpDefinitionExport(PowerUpDefinition? p)
    {
        if (p is null)
        {
            return null;
        }

        return new
        {
            target_id = p.TargetAttribute is null ? null : p.TargetAttribute.Id.ToString(),
            target = p.TargetAttribute?.Designation,
            boost = EffectValue(p.Boost),
        };
    }

    // Rel 把 AttributeRelationship 投影为导出形态：属性引用同时带 designation（人读）与
    // id（精确解析——存在少量重复 designation，如 "Temp ..." 变体，不能按名解析）。
    private static object Rel(AttributeRelationship r) => new
    {
        target = r.TargetAttribute is null ? null : new { id = r.TargetAttribute.Id.ToString(), designation = r.TargetAttribute.Designation },
        input = r.InputAttribute is null ? null : new { id = r.InputAttribute.Id.ToString(), designation = r.InputAttribute.Designation },
        operand = r.OperandAttribute is null ? null : new { id = r.OperandAttribute.Id.ToString(), designation = r.OperandAttribute.Designation },
        input_operand = r.InputOperand,
        input_operator = r.InputOperator.ToString(),
        aggregate_type = r.AggregateType.ToString(),
    };

    // MasterSkillExport 把一条大师技能（Skill.MasterDefinition）投影为导出形态。
    // value/display 数组走原版 GameLogic/MasterSkillExtensions 的 CalculateValue/CalculateDisplayValue
    // （MathParser + 进程级缓存），因此与运行期取值**同源**；Go 侧按等级查表。
    private static object MasterSkillExport(Skill skill, Dictionary<MasterSkillRoot, int> rootIndex)
    {
        var m = skill.MasterDefinition!;
        var maximumLevel = (int)m.MaximumLevel;
        var entry = new SkillEntry { Skill = skill };
        var values = new float[maximumLevel];
        var displays = new float[maximumLevel];
        for (var level = 1; level <= maximumLevel; level++)
        {
            entry.Level = level;
            values[level - 1] = entry.CalculateValue();
            displays[level - 1] = entry.CalculateDisplayValue();
        }

        return new
        {
            number = (int)skill.Number,
            name = Ls(skill.Name),
            root = m.Root is null ? (int?)null : rootIndex[m.Root],
            rank = (int)m.Rank,
            maximum_level = maximumLevel,
            // 原版 MinimumLevel 复用为"第一次加该技能要付的点数"（AddMasterPointAction.cs:77,105）。
            initial_points = (int)m.MinimumLevel,
            required_master_skills = m.RequiredMasterSkills.Select(r => (int)r.Number).ToArray(),
            replaced_skill = m.ReplacedSkill is null ? (int?)null : (int)m.ReplacedSkill.Number,
            target_attribute = m.TargetAttribute?.Designation,
            aggregation = m.Aggregation.ToString(),
            extends_duration = m.ExtendsDuration,
            // PassiveBoost 的大师技不进 C1 F3 11 技能列表（SkillList.cs:224），只出现在大师树里。
            passive_boost = skill.SkillType == SkillType.PassiveBoost,
            value_formula = m.ValueFormula,
            display_value_formula = m.DisplayValueFormula,
            value_at_level = values,
            display_at_level = displays,
        };
    }

    // DropGroupExport 把掉落组投影为导出形态（T1-6：DefaultDropGenerator 的输入）。
    private static object DropGroupExport(DropItemGroup g) => new
    {
        chance = g.Chance,
        item_type = g.ItemType.ToString(),
        money_amount = g is ItemDropItemGroup idg ? idg.MoneyAmount : 0,
        min_level = g is ItemDropItemGroup idg2 ? (int)idg2.MinimumLevel : 0,
        max_level = g is ItemDropItemGroup idg3 ? (int)idg3.MaximumLevel : 0,
        item_level = g.ItemLevel is null ? (int?)null : (int)g.ItemLevel.Value,
        min_monster_level = g.MinimumMonsterLevel,
        max_monster_level = g.MaximumMonsterLevel,
        possible_items = g.PossibleItems.Select(pi => new { group = (int)pi.Group, number = (int)pi.Number }),
    };

    // CraftingExport 把一条 ItemCrafting 投影为导出形态（TRIM-02）。
    private static object CraftingExport(ItemCrafting c, IList<MonsterDefinition> hosts) => new
    {
        number = (int)c.Number,
        name = Ls(c.Name),
        handler = c.ItemCraftingHandlerClassName,
        hosts = hosts
            .OrderBy(m => (int)m.Number)
            .Select(m => new { monster = (int)m.Number, name = Ls(m.Designation), npc_window = m.NpcWindow.ToString() })
            .ToArray(),
        settings = c.SimpleCraftingSettings is null ? null : CraftingSettingsExport(c.SimpleCraftingSettings),
    };

    private static object CraftingSettingsExport(SimpleCraftingSettings s) => new
    {
        money = s.Money,
        money_per_success_percent = s.MoneyPerFinalSuccessPercentage,
        npc_price_divisor = s.NpcPriceDivisor,
        success_percent = (int)s.SuccessPercent,
        max_success_percent = (int)s.MaximumSuccessPercent,
        multiple_allowed = s.MultipleAllowed,
        result_select = s.ResultItemSelect.ToString(),
        add_luck = s.SuccessPercentageAdditionForLuck,
        add_excellent = s.SuccessPercentageAdditionForExcellentItem,
        add_ancient = s.SuccessPercentageAdditionForAncientItem,
        add_guardian = s.SuccessPercentageAdditionForGuardianItem,
        add_socket = s.SuccessPercentageAdditionForSocketItem,
        result_luck_chance = (int)s.ResultItemLuckOptionChance,
        result_skill_chance = (int)s.ResultItemSkillChance,
        result_exc_chance = (int)s.ResultItemExcellentOptionChance,
        result_max_exc = (int)s.ResultItemMaxExcOptionCount,
        required = s.RequiredItems.Select(CraftingRequiredExport).ToArray(),
        result_items = s.ResultItems.Select(CraftingResultExport).ToArray(),
    };

    private static object CraftingRequiredExport(ItemCraftingRequiredItem r) => new
    {
        reference = (int)r.Reference,
        min_amount = (int)r.MinimumAmount,
        max_amount = (int)r.MaximumAmount,
        min_level = (int)r.MinimumItemLevel,
        max_level = (int)r.MaximumItemLevel,
        add_percentage = (int)r.AddPercentage,
        npc_price_divisor = r.NpcPriceDivisor,
        success_result = r.SuccessResult.ToString(),
        fail_result = r.FailResult.ToString(),
        // possible_items 为空 = 原版"匹配任意物品"（Random Item）语义，Go 侧要能区分空与非空。
        possible_items = r.PossibleItems
            .Select(i => new { group = (int)i.Group, number = (int)i.Number, name = Ls(i.Name) })
            .ToArray(),
        required_options = r.RequiredItemOptions.Select(o => OptionTypeKind(o)).ToArray(),
    };

    private static object CraftingResultExport(ItemCraftingResultItem r) => new
    {
        reference = (int)r.Reference,
        add_level = (int)r.AddLevel,
        min_level = (int)r.RandomMinimumLevel,
        max_level = (int)r.RandomMaximumLevel,
        durability = r.Durability is null ? (int?)null : (int)r.Durability.Value,
        item = r.ItemDefinition is null
            ? null
            : new { group = (int)r.ItemDefinition.Group, number = (int)r.ItemDefinition.Number, name = Ls(r.ItemDefinition.Name) },
    };

    // QuestExport 导出一条任务定义。max_level=0 在原版表示"无上限"（PlayerQuestExtensions.cs:65），
    // 原样导出 0；qualified_class 只有单值（原版 QualifiedCharacter 是引用相等比较）。
    private static object QuestExport(QuestDefinition q, MonsterDefinition npc) => new
    {
        group = (int)q.Group,
        number = (int)q.Number,
        starting_number = (int)q.StartingNumber,
        refuse_number = (int)q.RefuseNumber,
        name = Ls(q.Name),
        npc_number = (int)npc.Number,
        npc_name = Ls(npc.Designation),
        min_level = q.MinimumCharacterLevel,
        max_level = q.MaximumCharacterLevel,
        repeatable = q.Repeatable,
        requires_client_action = q.RequiresClientAction,
        required_start_money = q.RequiredStartMoney,
        qualified_class = q.QualifiedCharacter is null ? (int?)null : (int)q.QualifiedCharacter.Number,
        required_kills = q.RequiredMonsterKills
            .OrderBy(k => k.Monster is null ? 0 : (int)k.Monster.Number)
            .Select(k => new
            {
                monster = k.Monster is null ? (int?)null : (int)k.Monster.Number,
                name = k.Monster is null ? string.Empty : Ls(k.Monster.Designation),
                count = k.MinimumNumber,
            }),
        // item_level 来自 DropItemGroup（QuestCompletionAction.cs:39 只认这一个约束；
        // WithItemRequirement 里模板物品的 level/luck/skill/option 全被原版丢弃 —— 见其 `// TODO?`）。
        required_items = q.RequiredItems
            .OrderBy(r => r.Item is null ? 0 : (int)r.Item.Group)
            .ThenBy(r => r.Item is null ? 0 : (int)r.Item.Number)
            .Select(r => new
            {
                group = r.Item is null ? (int?)null : (int)r.Item.Group,
                number = r.Item is null ? (int?)null : (int)r.Item.Number,
                name = r.Item is null ? string.Empty : Ls(r.Item.Name),
                count = r.MinimumNumber,
                item_level = r.DropItemGroup?.ItemLevel is byte level ? (int?)level : null,
            }),
        rewards = q.Rewards.Select(QuestRewardExport).ToArray(),
    };

    // QuestRewardExport 导出奖励。type 用 QuestRewardType 枚举名（稳定语义标识，同 crafting 的 handler 名）。
    private static object QuestRewardExport(QuestReward r) => new
    {
        type = r.RewardType.ToString(),
        value = r.Value,
        item = r.ItemReward is null
            ? null
            : new
            {
                group = r.ItemReward.Definition is null ? (int?)null : (int)r.ItemReward.Definition.Group,
                number = r.ItemReward.Definition is null ? (int?)null : (int)r.ItemReward.Definition.Number,
                name = r.ItemReward.Definition is null ? string.Empty : Ls(r.ItemReward.Definition.Name),
                level = (int)r.ItemReward.Level,
                durability = (int)r.ItemReward.Durability,
                has_skill = r.ItemReward.HasSkill,
                luck = r.ItemReward.ItemOptions.Any(o => o.ItemOption?.OptionType == ItemOptionTypes.Luck),
                option_level = (int?)r.ItemReward.ItemOptions.FirstOrDefault(o => o.ItemOption?.OptionType == ItemOptionTypes.Option)?.Level ?? 0,
            },
        attribute = r.AttributeReward is null
            ? null
            : new { id = r.AttributeReward.Id.ToString(), designation = r.AttributeReward.Designation },
        skill = r.SkillReward is null
            ? null
            : new { number = (int)r.SkillReward.Number, name = Ls(r.SkillReward.Name) },
    };

    private static long[] CreateExpTable(string experienceFormula, short maximumLevel)
    {
        var argument = new org.mariuszgromada.math.mxparser.Argument("level");
        var expression = new org.mariuszgromada.math.mxparser.Expression(experienceFormula);
        expression.addArguments(argument);
        return Enumerable.Range(0, maximumLevel + 2)
            .Select(level =>
            {
                argument.setArgumentValue(level);
                return (long)expression.calculate();
            })
            .ToArray();
    }

    private static void Write<T>(string outputDir, string fileName, T payload)
    {
        var path = Path.Combine(outputDir, fileName);
        using var stream = File.Create(path);
        JsonSerializer.Serialize(stream, payload, JsonOptions);
        Console.WriteLine($"  {fileName} ({new FileInfo(path).Length / 1024.0:F0} KB)");
    }
}
