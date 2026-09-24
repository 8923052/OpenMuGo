# OpenMUGo — Agent 关键信息

Go 重写的 MU Online（S6E3，客户端版本 `20404`）服务端，目标是与 OpenMU(C#) 协议、MuMain(C++ 客户端) 字节级对齐。纯内存态、无持久化，以「官方 XML 生成协议 + C# 参考实现 golden 向量」保证正确性。

> 目录名是 `OpenMUGo`，Go module 名仍是 `mugo`（`go.mod`），import 前缀一律 `mugo/internal/...`。

## 一句话现状

**T1 实体与世界收官 + T2 主干打通（2026-09-19，总进度 ≈65%，见台账 §8）**：地形碰撞/真实属性装配/物品编解码与 Storage/AoI 分桶/怪物生成与 AI/掉落生成/传送门全通（出站物品用 S6 **5~15B 扩展布局**、金币用 **C1 0x2F**）；T2-0 防线基建（goldencombat + trace）、T2-1 视野补全、T2-2 战斗 Hit、T2-3 死亡+掉落、T2-4 拾取/丢弃、T2-5 背包搬运(0x24)+装备穿脱+外观广播（C1 25）、T2-6 药水(0x26)+MagicEffect 数据结构、T2-7 死亡复活+经验结算+升级（C3 16 / C1 F3 05 / C1 48）全部落地并经真机联调修正。MVP 主循环「打怪 → 掉落 → 拾取 → 背包装备 → 药水 → 升级」已全程打通。下一步 **T2-8 属性点分配（F3 06）**。

真机（MuMain / Client/ 下的 Main.exe）联调修正过十余处：入站 C1/C2 必须逆 Xor32（否则登录后卡死）、20404 必须下发 44B 的 `CharacterListExtended`、初始进图不等 F3 12、对象 ID 三段布局与出生位旗标、自 ID 哨兵 0x200 + 视野差分、行走 nibble +1 转枚举、0 步方向包只改朝向、怪物攻击必发 0x18、攻速不可写死、物品 5~15B 动态长度（详见铁律 13）。前两处详见 [doc/08](doc/08-rewrite-lessons-pitfalls.md) 的 E2 与 B10。

**进度只看一处**：[doc/15-foundation-progress-analysis.md](doc/15-foundation-progress-analysis.md) 是**唯一进度台账**（铁律 12 的执行入口）；其他文档里的进度表述都是历史快照。

## 目录速查

完整目录表见 [doc/12-directory-audit.md](doc/12-directory-audit.md) §1（原版项目 ↔ Go 落位）。只记最容易搞错的几条：

- `internal/attribute`（属性系统，原版 AttributeSystem）与 `internal/gamelogic/player`（玩家属性装配）**是两个东西**，别混。
- 出站序列化一律在 `internal/view/remote`，**不许留在 `gamelogic`**；领域模型在 `gamelogic/entity`。
- `handler/<域>` ← `MessageHandler/<域>`，`view/remote/<域>` ← `RemoteView/<域>`，`gamelogic/<域>` ← `GameLogic/<域>`；**原版名字不一致就照抄不一致**（如 `handler/quests` 复数 vs `view/remote/quest` 单数）。
- 原版包根的扁平文件（`GameLogic` 约 50 个、`MessageHandler` 约 150 个）按 `doc/12` §4 的归并规则落位，**每条归并都要登记在该表**。

## 常用命令

```bash
# Git Bash（D:\Soft\Git\bin\bash.exe；系统 PATH 里的 bash 是坏的 WSL，勿用）
./build.sh                  # 默认编译，产物在当前目录（不内置测试账号）
./build.sh -s openmu        # 显式内置 test0..test9
./build.sh -t               # go vet + go test 后编译
./build.sh linux amd64      # 交叉编译
./build.sh -o build         # 产物输出到 build 子目录
go test ./... -count=1      # 全量测试
./mugo.exe                  # 运行；运行时 -seed=openmu 也可临时开账号
```

## 铁律（改动前必读）

1. **默认不带测试数据**：`defaultSeed=none`，测试账号只能 `-s openmu` 编译期或 `-seed=openmu` 运行期显式开启。
2. **协议字段不要手猜**：查 `spec/0.9.10/*.xml` 与 MuMain/OpenMU 源码；编码正确性靠 golden 向量，不靠"看起来对"。
3. **生成物勿手改**：`internal/proto/**/*_gen.go` 改 XML 后用 `tools/regen.ps1` 重新生成。
4. **SimpleModulus 计数器每连接独立**，绝不能跨连接共享 codec。
5. **"C1 明文"只免 SimpleModulus，Xor32 照做**：F3 组、D4 行走等 `IsEncryptionExpected=false` 的包，客户端出站仍**无条件** Xor32，服务端入站必须走 `codec.OpenPlain` 逆回来（C3/C4 走 `codec.Open` = SM+Xor32）。漏了这步 → 客户端登录后第一个 C1 包变 `F3 0D` 被忽略、**卡死**（见 doc/08 E2）。C1 的 code 在偏移 2，**C2 的 code 在偏移 3**。
6. **可拔插边界**：种子/属性简化等"非主线"逻辑隔离在独立包，主线不反向依赖。
7. 无真机验证约定，正确性靠：源码逐行移植 + C# golden + Go 回环测试。**回环测试的"客户端"必须和 MuMain/OpenMU ClientLibrary 的管线完全一致（含 Xor32 无条件），否则测试全绿而真机挂**。
8. **同一个包按版本有不同形态**：20404（OpenMU 记为 Season 106 Episode 3 = MuMain）与 10404（Season 6/3 = GMO）不是同一个版本号，插件选择不同 → 角色列表 44B/27B 扩展 vs 34B/18B 紧凑。改包前先按客户端源码确认版本与长度。
9. 严禁加超过3行的注释，不需要，有需要描述的应该增加md文档，放到doc目录里
10. **架构分层与文件对应原版**（重写任何模块前必读；细则见 `doc/12` §4 与 `doc/11`）：
    - **三层分开**：入站 `handler`（薄壳：解包 → 调 action）/ 逻辑 `gamelogic/action` / 出站 `view`，对应原版 `MessageHandler` / `PlayerActions` / `IViewPlugIn`。`handler` 里**禁止**拼出站包。
    - **文件对齐**：一个 C# 类 → 一个 Go 文件，文件头一行注明对应 OpenMU 路径。域子包名照抄原版，不一致就照抄不一致。
    - **flat 归并**：原版包根的扁平文件按 `doc/12` §4 规则归并（先按原版域目录 → 按"动作主角" → 工具散到使用方 → 接口跟唯一实现），**每条归并必须登记在该表**。
    - **多版本不可裁剪**：4 个版本（`07500`/`09504`/`10404`/`20404`）靠"实现带版本区间 + 运行时选最优匹配"共存。版本差异**只允许**在 `internal/view/remote`、`internal/server/gameserver`、`internal/transport/crypto` 三处；`gamelogic` 与 `persistence` **禁止** import `internal/version`、禁止任何版本判断。变体后缀沿用 `_075`/`_095`/`_097`/`_extended`，每个实现暴露一个 `version.Constraint`。`CompareTo` 的语言不兼容语义与 `Suitable` 的 `<= 1` 判定**原样保留**。
    - **序列化不得留在 gamelogic**：领域模型在 `gamelogic/entity`，编解码在 `view/remote`。
11. **服务粒度同样要对应原版**：原版是「单进程 + 多服务」（`Program.cs` DI 注册 + 4 个 `IHostedService`），且 **GS 是「一个实例多端点、每端点绑一个客户端版本」**。~~Go 侧现在只有两条写死的监听器~~（**2026-09-24 更正**：容器两段启动 + `-versions` 多端点已落地，见 `cmd/mugo/main.go:176-215`、`internal/server/container/container.go:32-35`，本条按历史表述保留）。服务级缺失清单与实施顺序见 [doc/13-service-level-gaps.md](doc/13-service-level-gaps.md)，**改服务装配前先读它**；具体施工方案（阶段划分、每阶段验收、先手修复清单）见 [doc/14-simple-services-first.md](doc/14-simple-services-first.md)。
12. 每次实现后都要更新文档进度——**只更新 [doc/15](doc/15-foundation-progress-analysis.md) 唯一台账**（其他文档的进度内容是历史快照，不改）；施工过程细节写对应施工文档（如 doc/14）。
13. **对象 ID 布局与进图时序（改视野/进图/掉落/生成器前必读；对照原版 `GameMap.AddAsync`、`ViewExtensions`、`NewNpcsInScopePlugIn`、`NonPlayerCharacter.Initialize`、`SelectCharacterAsync`）**。2026-09-19 真机事故源：NPC ID 从 0x8001 起分配 + 进图挂在 F3 12 上，导致客户端看不见 NPC、坐标/朝向错乱。
    - **ID 三段布局**：掉落物 `0..0x1FF`（`_dropIdGenerator = IdGenerator(0, 0x1FF)`）、对象池 **`0x201..0x7FFF`**（玩家与怪物/NPC **同一**生成器，原版每地图一个 `IdGenerator(0x201, 0x7FFF)`；Go 侧全局单池，Spawner 的 ID 经 `world.ReserveIDs` 预占）、**`0x8000` 以上不是 ID——是出站视野包的"出生位"旗标位**。客户端按 `id & 0x7FFF` 索引对象，ID 段写错 → 对象互相覆盖。
    - **"自己"的 ID 恒为哨兵 `0x200`**（原版 `ViewExtensions.GetId(identifiable, playerOfView)`：对象==查看者本人时返回 ConstantPlayerId）。自己收到的 C2 12 出生包 = `0x8000|0x200`、自己的 D4 行走确认 ObjectID = `0x200`；广播给其他观察者时才用移动者的真实动态 ID。**ID 池从 0x201 起就是给 0x200 让位的**。写成动态 ID → 真机出现"另一个自己跟着走"（复制人）。
    - **视野差分覆盖所有对象类型**：玩家移动跨桶时对视野内**玩家、怪物/NPC、掉落物**逐个触发进入（NewPlayersInScope/NewNpcsInScope，isSpawned=true → 置 0x8000）/离开（ObjectsOutOfScope）事件（原版 `UpdateObservingBucketsAsync` + `ObserverToWorldViewAdapter`）。只做进图快照不做行走差分 → 真机"走到有怪的地方看不到新 NPC"。
    - **视野差分只发增量**：进入视野包只含"新进入"的对象——把视野内全部对象重发会让客户端把每个 NPC 重新出生一遍（真机事故：NPC 反复"从地下钻出来"）。
    - **视野判定是桶粒度不是逐对象切比雪夫**（`world.InScope`/`coveredBuckets`，对照 `BucketMap.GetBucketsInRange`）：订阅覆盖 [pos±12] 的全部桶，桶内所有对象可见——可见距离随桶边界浮动 12~27，对象在屏幕边缘外就出现。逐对象过滤 → "走近才刷 NPC"。
    - **行走三语义**（原版 `PlayerMovement.MoveAsync`，逐条对照）：①客户端起点与服务端位置欧氏距离 >5 → 回拉（C1 15 ObjectMoved instant，自 ID=0x200）；②路径在**第一个阻挡步截断**（不整包拒绝），玩家走到最后可走步；③首步即阻挡 → 回拉。**地形数据/解析与原版一致（walk=v0||v1，safezone=v1）——若行走大面积受阻，先查角色是否站在阻挡格（如种子坐标在喷泉上）**。
    - **0 步方向包只改朝向，绝不回拉**（原版两处锚点：`CharacterWalkBaseHandlerPlugIn.WalkAsync` 的 `Header.Length > 6` 判据，否则只 `player.Rotation = TargetRotation.ParseAsDirection()`；`PlayerMovement.WalkToAsync` 开头 `if (steps.IsEmpty) return;`）：客户端 `Action()` 的 MOVEMENT_ATTACK 分支**每一下普通挥击**都先发 `C1 06 D4 <x> <y> <朝向<<4|0>`（`LetHeroStop()` → `SendCharacterMove(..., PathNum = 1)`，只带朝向、无方向字节，帧长恒 6）。判据 `action.IsNonMovingWalk(stepCount, frameLen)` = `stepCount == 0 || frameLen <= 6` → 只更新朝向、**零出站帧**。当成行走回拉（C1 15）会经客户端 `ReceiveMovePosition` → `SetPlayerStop` 把刚起手的挥击切成站立 → 真机"**攻击动作一开始就停住、一顿一顿，但伤害照出**"（伤害来自紧随的 `SendHitRequest`，与动画无关；doc/08 B11、doc/15 T2-2 修正 8）。另需 `len(frame) < 6` 直接返回——`AsWalkRequest` 不做长度校验，短帧读 `d[5]` 越界。
    - **死亡/重生**（原版 `OnDeathAsync` + `RespawnAtAsync`）：死亡 → IsAlive=false + ObjectGotKilled 广播（尸体不再被 AI 索敌）→ 3s 后满血回出生门 → MapChanged（C3 1C 0F）→ 客户端 F3 12 重入。进图时 CurrentHealth=0 → 取满血。**守卫（ObjectKind=Guard）不索敌玩家**（原版 GuardIntelligence 只打怪）。
    - **周期恢复**（原版 `GameContext.RecoverTimer` 3s + `Player.RegenerateAsync`，对照 `Stats.Regeneration`）：各属性按 `(max×倍率+绝对值)×elapsed/Interval` 累加并钳到 max——**HP 7s、MP 3s、AG 3s、护盾 1s**（四条 `IntervalRegenerationAttributes`，遍历顺序 法力→生命→AG→护盾）；倍率/绝对值走属性系统（"Health Recovery Multiplier"/"…Recovery Absolute Increase" 等 designation，护盾绝对值叫 "Shield Recovery Absolute" 无 Increase）。**安全区内 AG 绝对值额外 +3**（`AbilityRecoveryAbsolute += 3×IsInSafezone`）→ 城里回 AG 明显快于城外。**护盾只在"恢复启用"（安全区 或 `ShieldRecoveryEverywhere ≥ 1`）且静置 ≥10s 后才回**（`ShieldRecoveryHiatusPlugIn`：挨打/离开安全区即把 hiatus 归零重新计时）。Go 侧 `CharStats` 是整数而原版属性是 float，用 `action.RegenRemainder` 结转不足 1 点的小数余量——每 3s 丢一次小数会让长期速率偏慢约 20%。
    - **战斗出站用 ObjectHitExtended（16B）**：S6 客户端按 16B 解析 0x11——发基础 10B 形态会被错位解析（无伤害数字 + 客户端 SD/AG/MP 乱变，真机事故）。扩展字段：Kind/RF 位/双倍/三重 + ObjectId(**小端**) + HealthStatus/ShieldStatus（血盾条 = current/max*250，0xFF=无该资源，`CalcStatStatus`）+ HealthDamage/ShieldDamage（小端 u32）。targetId 按 GetId(playerOfView)（自己=0x200）。**必须先扣血再取状态**（对照 `ShowHitAsync` 在 `ApplyDamage` 之后取值）；且**不要**额外补发 0x26 FF——客户端按伤害自行扣血条，服务端多发会覆盖客户端状态。
    - **怪物攻击必须显式发 0x18 ObjectAnimation**（`View.ShowObjectAnimation`，animation = `AT_ATTACK1` = **120**）：对照 `Monster.AttackAsync`——先结算伤害，再 `ForEachWorldObserverAsync(ShowMonsterAttackAnimationAsync)`。S6 客户端的怪物近身挥击**只由 0x18 触发**，缺失时客户端不播放挥击动作、玩家只见"隔空掉血"→ 实测表现为"近身怪看起来是远程"。要点：观众集合取**怪物**的观察者（不是受击者的）、方向 = 怪物→目标（`npcMoveNibble` 的 8 卦限反查，与行走 nibble 同一套编码 = Direction 枚举 packet byte）、targetId 按接收者视角解析（自己=0x200）。
    - **攻速/魔速不能写死**（原版 `FixAttackSpeedCalculationUpdate`）：客户端 `SetAttackSpeed` 把该值直接算成**动画倍速**（`PlaySpeed = 基准 + AttackSpeed*0.004`，ZzzCharacter.cpp），上限 200。恒发 200 = 所有职业/武器按最高速播放 → 实测"攻击动作一顿一顿、不连贯"。按职业敏捷推导：DK/DW 敏/20 魔速、DL 敏/10 双速、精灵 敏/50 双速、RF 敏/9 双速、MG/召唤 敏/20 魔速（武器/手套加成待装备系统落地再补）。
    - **地面物的两个出站码必须与客户端派发表一致**（`WSclient.cpp` 顶层 switch）：**物品 → C2 0x20**（`ReceiveCreateItemViewportExtended`，内嵌 5~15B 动态物品数据，stride 逐件按实际长度定）；**金币 → C1 0x2F**（`ReceiveCreateMoney`，MoneyDroppedExtended）。发 0x20 的金币会被客户端当**物品**解析 → 地面永远看不到金币。**字节序不对称**：物品 `DroppedItem.Id` 客户端 `MAKEWORD(IdH,IdL)` 读 = 大端；金币 `PCREATE_MONEY.Id` 原生 WORD 直读 = 小端。
    - **物品线上编码是 5~15B 动态长度**（`view/remote.EncodeItemExtended`，`PITEM_EXTENDED_BASE` 恒 5B：GroupAndNumber/Level/Durability/OptionFlags；`#pragma pack(push,1)`，枚举底层 `: BYTE`）：长度算法对齐客户端 `CalcItemLength`（Option→Excellent→Ancient→Harmony→Sockets）——**Luck/Skill/Guardian 是纯标志位、不占字节**；`HasExcellent` 只看 `ExcellentBits|FenrirBits`（Wing 已在卓越位里，误加 `WingOptionNumber>0` 会让翅膀物品多一字节、后续整条错位）；`SocketBonus` 是 4-bit。写死 12B → 地面物/背包物整体错位。
    - **背包搬运(0x24) 的子码区分与失败帧必须带物品段**（T2-5，对照 `ItemMovedPlugIn`/`ItemMoveFailedPlugIn`）：成功 = `C3 24 <TargetStorageType@3><TargetSlot@4><ItemData@5…>`；失败 = `C3 24 **FF**`（0xFF 落在 TargetStorageType 位）。**失败帧仍要带物品数据段**——`item == nil` 时按原版 `NeededSpace = ItemExtendedMaxSize(15)` 补零，否则客户端 `CalcItemLength` 越界。测试里统计"成功帧"必须按 `frame[3] != 0xFF` 过滤，`countFrames(0x24)` 会把失败帧一并计入（踩过）。
    - **装备外观变化包 C1 0x25 全长固定 14B**（`PCHANGE_CHARACTER_EXTENDED`）：客户端的 `Key` 是 **WORD** → 3B `PBMSG_HEADER` 后编译器插 **1B 对齐填充** → `Key@4`（与 OpenMU `ChangedPlayerId@4` 吻合）；字段偏移 ItemSlot@6 / ItemGroup@7 / ItemNumber@8(**LE**) / ItemLevel@10 / ExcellentFlags@11 / AncientDiscriminator@12 / IsAncientSetComplete@13。**按 13B 紧凑发 = 后续字段整体错位**。
    - **`ItemGroup == 0xFF` 表示该槽"卸下"**（客户端 `WSclient.cpp ReceiveChangePlayer` 据此清空模型；否则按 group/number 装备）——卸下时**不能**沿用物品自身 group（发出去的会变成"换个 group 装备"而不是清空）。
    - **外观包只发他人、不发自己**（原版 `InventoryStorage.UpdateItemsOnChangeAsync` → `ForEachWorldObserverAsync(..., sendToSelf: false)`）。但**必须同步自己的 27B 进图外观**（`remote.PatchAppearanceSlot`）：OpenMU 每次由 `EquippedItems` 现算，Go 侧是增量维护——"走出视野再回来"的观察者会从 `ScopeEntry.Appearance` 读到**旧装备**。宠物槽补丁要在 `setUnshinyItem` 之后**重挂** Fenrir 档位（0x02/0x04/0x06）。
    - **搬运决策三段照抄 `CanMoveAsync`**：① 装备区 `ItemSlot.ItemSlots.Contains(toSlot)`；② `CompliesRequirements`（需求值 `(multiplier×CalculateDropLevel×reqVal/100)+20`，力量再 `+OptionLevel×4`；能量 multiplier=4；召唤师书 `group==5 && HasSkill` 走 `((reqVal×(dl+itemLevel)×3)/100)+20`；`CalculateDropLevel` 远古 +30 / 卓越 +25 **互斥**、再 +3×itemLevel）+ 职业在 `QualifiedCharacters`；③ `ConflictsWithEquippedHands`（右手非弹药武器时左手不能放 2 宽件；左手有 2 宽件时右手不能放单手武器/盾）。目标格空时的矩形占位检查必须**忽略源件自身**（原版 `i == fromSlot && sameStorage → continue`）。**原地同槽搬运直接判不可行**——原版会落入堆叠分支，`CanCompletelyStackOn(self, self)` 成立 → `FullStackAsync` 数量翻倍再移除源件（数量复制漏洞）。
    - **配置加载顺序：`buildIndex()` 必须在域校验之前**（`config.Load`）：`validateDrops` 用 `itemByKey` 判"悬空物品引用"，索引未建时 `hasItem` 对**所有**引用返回 false → 每个掉落组的 `PossibleItems` 被清空、`item_type=="None"` 的组被整体剔除（S6 主掉落 51 个按怪级分档的 None 组全丢）。这是"怪物击杀不掉物品"的主根因。
    - **`GetPossibleList` 的等级条件是"保留"语义**（`it.DropLevel > monsterLevel - DropLevelMaxGap(12)`，只排除远低于怪级的廉价物品）：写成"跳过"后怪级 < 12 时条件恒成立 → `possibleList` 恒空 → `RandomItem` 组一件都掉不出来。另 `PartitionDropGroups` 前必须做 `IsGroupRelevant`（`MinMonsterLevel ≤ 怪级 ≤ MaxMonsterLevel`）过滤——它决定加权游标的分母，漏掉会抬高低级怪掉高级物的权重并挤掉金币概率。
    - **掉落/金币包 FreshDrop 位必须置位**：进视野即"新鲜掉落"（原版 ShowDroppedItemsAsync/ShowMoneyAsync 的 fresh=true 语义，id bit7）。
    - **桶维护必须先 remove 再改坐标**：`map.Walk` 里若先更新 p.X/p.Y 再 `grid.remove(p)`，remove 定位到新桶（no-op），旧桶条目残留成幽灵对象（跨桶移动必现，真机事故）。
    - **对象 ID 在进图时分配**（Go：`enterWorld` → `world.AllocID()`，离场/换图 `FreeID` 回收）。F1 00 `GameServerEntered` 的 PlayerId 是**哨兵值 `0x200`**（原版 `ShowLoginWindowPlugIn`），与对象 ID 无关，不要拿来当自己的视野 ID。
    - **视野包出生位**：`AddNpcsToScope` 与 `AddCharactersToScope` 里出生对象的 Id 必须 `|= 0x8000`（原版 isSpawned=true 语义）。
    - **NPC 出生点/朝向**（`NonPlayerCharacter.Initialize`）：矩形内随机 → `IsValidSpawnPoint`（Monster/Trap 禁入安全区；Monster/Guard 必须可走）→ 失败重试（单点区 1 次、区域 100 次，全败原版直接抛异常）；朝向 `GetSpawnDirection`：配置 Undefined(0) → 随机 1..8，**封包值 = Direction-1**（`ToPacketByte`）。
    - **初始进图不等 F3 12**：原版 `SelectCharacterAsync` 尾部**直接**调 `ClientReadyAfterMapChangeAsync`（客户端加载地图期间缓存后续封包）；F3 12 只在换图/重生（EnteringMap 状态）后驱动重入，已在图上时按 `PlayerMapTransitions` 守卫忽略。真实客户端初始进图**不会发** F3 12。
    - **行走方向 nibble 须 +1 转枚举**（原版 `DecodePayload` → `ParseAsDirection`，`CalculateTargetPoint`）：客户端 nibble 是 **0..7**（0=West），增量表 `action.DirectionDeltas` 按**枚举 1..8** 索引——直接拿 nibble 当索引=每步错一个方向，表现为大面积"行走路径受阻"+ 服务器坐标与客户端漂移（真机事故）。速记映射：nibble 0=W(-1,-1)、1=SW(0,-1)、2=S(+1,-1)、3=SE(+1,0)、4=E(+1,+1)、5=NE(0,+1)、6=N(-1,+1)、7=NW(-1,0)。旋转字段全程存封包值（0..7）。
    - 帧头速记（常踩）：C1 头 len8+code@偏移2；C2 头 len16+code@**偏移3**。
    - **0x26 是"同码双形态"，字节序相反（T2-6，最易踩）**：客户端按**长度**区分两种形态，长度与字节序**同时**不同——
      - 子码 **0xFF**：9B `CurrentHealthAndShield`（u16 **大端** Health@4、Shield@7）↔ 24B `CurrentStatsExtended`（u32 **小端** Health@4/Shield@8/Mana@12/Ability@16，u16LE 攻速@20/魔速@22）。
      - 子码 **0xFE**：9B `MaximumHealthAndShield`（u16 **大端**）↔ 20B `MaximumStatsExtended`（u32 **小端** @4/8/12/16）。
      - S6（20404）一律发**扩展形态**（24B/20B 小端）；9B 大端是老客户端形态。按大端写 24B 的血量 → 客户端血条瞬间变天文数字。
      - 子码 **0xFD** `ItemConsumptionFailedExtended`（12B）：药水不可用**必须**发扩展形态（带当下血/盾），客户端据此置 `EnableUse=0` 并用包内数值纠正本地预测。
      - 入站 `ConsumeItemRequest` 是 **C3** 0x26、6B、slot@3（走 `codec.Open` = SM+Xor32）；出站 0x26 全是 **C1**（走 `OpenPlain`，仍无条件 Xor32）——同一个 code 入站 C3、出站 C1，别串。
    - **药水恢复是"分段 + 冷却"，不能一次写满**（T2-6，对照 `RecoverConsumeHandler`）：`recoverPercentage = TotalRecoverPercentage + itemLevel×RecoverPercentageIncreaseByPotionLevel`；`additional = max(0, AdditionalRecoverMinusCharacterLevel − 等级)`；`total = max×百分比/100 + additional`（**盾药水没有等级补偿项**）。默认分三段入账 20%@200ms / 60%@600ms / 20%@200ms；延迟缩减 = `itemLevel/16`，**仅当分段表为空或等级 ≥16 时一次性生效**。全局冷却 0.5s（`CooldownTime`），冷却内的第二次请求直接失败帧。用后耐久 −1、耗完移除该格物品。（详见 doc/15 T2-6）
    - **经验包必须发 16B 扩展形态**（T2-7）：C3 0x16 `ExperienceGainedExtended` = Type@3、AddedExperience u32LE@4、DamageOfLastHit u32LE@8、KilledObjectId u16LE@12、KillerObjectId u16LE@14。MuMain `WSclient.cpp` 的 0x16 分派是 `if (Size >= sizeof(PRECEIVE_EXP_EXTENDED)) ReceiveDieExpLarge else ReceiveDieExp`——发 9B 紧凑形态会被当 16B 错位解析（经验数字乱跳、击杀表现不触发）。**`KillerObjectId` 自己 = 哨兵 `0x200`**，被杀怪填真实对象 ID；`DamageOfLastHit` 只对队友非零。
    - **升级包是 C1 F3 05，不是 C3**（T2-7）：`CharacterLevelUpdateExtended` **32B**，C1 头 + 子码 0x05，字段全小端：Level@4、LevelUpPoints@6、MaxHealth@8、MaxMana@12、MaxShield@16、MaxAbility@20（u32）、FruitPoints@24、MaxFruitPoints@26、NegFruitPoints@28、MaxNegFruitPoints@30（u16）。**原版把同一个 `GetMaximumFruitPoints()` 填进 MaxFruitPoints 与 MaxNegativeFruitPoints**（`UpdateCharacterStatsPlugIn.SendCharacterInformationAsync` 两处入参同源），照抄。升级光效 `C1 0x48 ShowEffect`（6B，PlayerId **u16 大端**@3、Effect@5，LevelUp=16）走 `ForEachWorldObserverAsync(..., sendToSelf: true)` → **自己收 0x200，观察者收真实 ID**。
    - **击杀结算的次序与返回值**（T2-7，对照 `AttackableNpcBase.OnDeathAsync`）：**先结算经验、后生成掉落**；掉落金钱 = `计算出的经验 + BaseMoneyDrop(7)`——即使已达等级上限、实际入账 0，金钱仍按**计算值**给（`AddAfterKillAsync` 返回的是 `CalculateAfterKillAsync` 的结果）。升级循环每跨一级都要：等级 +1 → 加升级点 → `SetReclaimableAttributesToMaximum`（血/法力/BP/盾 Current ← Maximum）→ **重建属性系统**（属性按等级派生）→ 发 C1 F3 05 → 发 0x48 光效；且**每一级各发一个** C3 16（原版 `AddExperienceCoreAsync` 循环内逐段入账）。已达上限时发 `Type=MaxLevelReached` 的 C3 16 且不再入账。
    - **当前值的唯一出口是 C1 26 FF（24B 扩展），且进图/重生必须主动补发**（2026-09-19 真机事故：城里不回红蓝、复活后 HP/AG/SD 一直空）。MuMain `WSclient.cpp` 的 `case 0x26` **只有** `ReceiveStatsExtended` 一条分支（按 `PRECEIVE_STATS_EXTENDED` 24B 解析 Life/Shield/Mana/BP/攻速/魔速，全 DWORD **小端**），**不存在 9B `CurrentHealthAndShield` 的解析路径**——发 9B 会被当 24B 读越界，血/蓝/AG/攻速全变垃圾。因此 `action.PlayerView` 不再保留 `ShowHealthUpdate`，周期恢复/药水/击杀恢复一律走 `ShowCurrentStatsExtended`。OpenMU 是靠**属性变更事件**触发下发（`UpdateStatsExtendedPlugIn`，`[MinimumClient(106,3)]` → S6 走扩展形态），Go 侧没有事件系统，**必须在 `enterWorld` 显式补发 C1 26 FE + C1 26 FF**（原版在属性初始化时触发）；漏发 = 重生后服务端已回满、客户端血条仍停在死亡时的 0。
    - **重生要回满四项，不是只回血**（原版 `SetReclaimableAttributesToMaximum`）：HP/蓝/AG/护盾 的 Current ← Maximum 全部顶满，并把护盾 hiatus 归零。只回血 → 真机"复活后血条满但 AG、SD 一直是空的"。
    - **击杀怪物后的即时恢复是"属性驱动"的**（原版 `AfterKilledMonsterAsync`，在 `AttackableNpcBase.OnDeathAsync` 里排在**经验之后、掉落之前**，且只有 `attacker == player` 时才触发）：`min(max, cur + (uint)(倍率×max + 绝对值))`，顺序 法力→生命→AG→护盾。倍率/绝对值由卓越 option/镶嵌/大师技能提供，**默认全 0 → 默认击杀不回红蓝（与原版一致）**。实测"打死怪不回红蓝"的根因是这条路径根本没接，不是参数缺失——先接链路，再谈数值。
    - **进图必须用真实属性系统重算 `c.Stats`，占位/种子值不得覆盖**（2026-09-20 真机事故：进图 SD/AG 恒 0 且不恢复，客户端 max 显示 1）。旧代码 `if c.Stats == nil { c.Stats = st }` 导致种子里 `NewCharStats` 的占位最大值（maxAG=0/maxSD=0）永久顶掉 `ResolveCharStats` 的真实结果——**`ResolveCharStats` 已保留持久化当前值/钱/果实点，进图一律 `c.Stats = st` 整体替换**，当前值 > 最大值时钳制。
    - **HP 只在休息时回，MP 休息有叠加周期**（原版 `CharacterClassInitialization.cs` L140-141：`HealthRecoveryMultiplier = 0.03×IsResting`、`ManaRecoveryMultiplier += 0.03×IsResting`；`RegenerateAsync` L912-925：休息时 HP 周期 7s→5s，MP 在常规 3s 之上**再叠加** elapsed/5s，等效合成周期 1.875s）。`IsResting` 由 **C1 18 动画包**驱动（`AnimationHandlerPlugIn`：[3]=Rotation、[4]=动画号，0x80/0x6C 坐、0x81/0x6D 倚、0x82/0x6E 悬 → 置 1，**无 else 分支**——站起动画不清除，清除只发生在移动 `UpdateIsInSafezoneAfterPlayerMoved`）。休息态变化必须重算 CombatValues 快照（IsResting 是属性系统的动态输入）。
    - **属性基值条目有聚合形态，导出件曾丢失**（2026-09-20 真机事故：`regenSD=133.3334`，正确 ≈0.001778，差 25 万倍）。原版 `CreateConstValueAttribute(1f/75000, ShieldRecoveryMultiplier, AggregateType.Multiplicate)` 是**乘法项**（与 AddRaw 的 100 相乘），导出件 `10_character_classes.json` 的 `base_attributes` 最初不带 `aggregate_type`，全部按 AddRaw 相加 → 又被 ramp 关系放大。`AttributeVal` 已支持 `aggregate_type` 字段，同名属性多条不同形态条目是常态，装配时必须逐条带形态。
    - **护盾恢复倍率随 hiatus 线性 ramp**：`RampFactor = 4/3 + (1/15)×ShieldRecoveryHiatus`（基值 4/3、斜率 1/15 都在原版 L153/189），倍率 = base×ramp(hiatus)；hiatus≥10s 才开始回，且"离开安全区/被打/回满"三者重置计时（Stats.cs L1189）。真实速率约 maxSD/375 点每秒——**回满需数分钟，"瞬间回满"本身是 bug 信号**。
    - **测试账号不持久化当前值**（对照 OpenMU `AccountInitializerBase.CreateCharacter`：只设 Level/LevelUpPoints）：`NewCharStats` 的 Current 四项恒 0，进图由 `ResolveCharStats` 回落职业 StatAttribute 基值（DK=110/20/1/1）。任何"用公式伪造当前值/最大值再覆盖真实系统结果"的写法都是数据污染。
14. 重写的核心业务逻辑必须和原版一致，如果有特别情况也需要先提示确认再执行
15. 复杂问题不好分析，应该先写完善的日志，实测之后可以给你分析
16. 交互结构体由生成器生成，在OpenMUGo\internal\proto，严禁手写字节，特殊情况需要先提示确认
17. 
## 详细文档索引（doc/）

| 文档 | 内容 |
|------|------|
| [doc/01-architecture.md](doc/01-architecture.md) | 架构分层、目录职责、会话状态机 |
| [doc/02-build-run-test.md](doc/02-build-run-test.md) | 构建/运行/测试、Git Bash 与 PowerShell 环境注意点 |
| [doc/03-protocol-crypto.md](doc/03-protocol-crypto.md) | C1-C4 帧、SimpleModulus/Xor32/Xor3、版本口径 |
| [doc/04-packet-flow-enter-world.md](doc/04-packet-flow-enter-world.md) | 连接→登录→选角→进图→移动 完整封包时序与字节偏移 |
| [doc/05-encoding-golden.md](doc/05-encoding-golden.md) | 物品编码（出站 5~15B 扩展 / 12B golden 参考）、外观 18B/27B 与 golden 体系 |
| [doc/06-test-accounts-seed.md](doc/06-test-accounts-seed.md) | OpenMU 测试账号、种子可拔插设计 |
| [doc/07-codegen.md](doc/07-codegen.md) | 协议生成器、XML、regen 流程 |
| [doc/08-rewrite-lessons-pitfalls.md](doc/08-rewrite-lessons-pitfalls.md) | **重写经验与踩坑大全（重点）** |
| [doc/09-real-client-login-hang-diagnosis.md](doc/09-real-client-login-hang-diagnosis.md) | **真机事故复盘：登录后卡死（Xor32/C1 + 角色列表版本形态）** |
| [doc/10-rewrite-feasibility-roadmap.md](doc/10-rewrite-feasibility-roadmap.md) | **重写可行性分析：大模型、依赖、优先级、路线图与一致性五道防线（§3 现状为 2026-09-18 历史快照）** |
| [doc/11-multi-version-adaptation.md](doc/11-multi-version-adaptation.md) | **多版本适配：版本矩阵、选择机制、变体清单与 Go 落位（必读）** |
| [doc/12-directory-audit.md](doc/12-directory-audit.md) | **目录结构审计与原版↔Go 映射表（含 flat 文件归并规则；铁律 10 的执行依据）** |
| [doc/13-service-level-gaps.md](doc/13-service-level-gaps.md) | **服务级缺失分析：原版 20 项 DI 注册 / 4 个 IHostedService vs Go 现状；多端点模型与实施顺序（铁律 11 的执行依据）** |
| [doc/14-simple-services-first.md](doc/14-simple-services-first.md) | **先行服务施工方案（施工档案）：P1 契约层 → P2 端点模型 → P3 观察者 → P4 容器 → P5 社交服务。P1–P5 全部完成** |
| [doc/15-foundation-progress-analysis.md](doc/15-foundation-progress-analysis.md) | **⭐ 唯一进度台账（铁律 12 执行入口）：分层进度矩阵、S-1…S-20 终局盘点、五道防线、48 变体缺口、完成度量化（≈44%，T0 收官）+ 下一步关键路径。其他文档的进度描述与本文冲突时以本文为准** |
