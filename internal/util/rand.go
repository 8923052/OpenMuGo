// Package util 承载跨模块的数值工具：System.Random 复刻（T0-d）与
// C# 浮点语义对齐（T0-e）。对应原版 GameLogic/Rand.cs 与 Math.* 语义（doc/10 T0-d/e）。
//
// 为什么必须复刻 System.Random：掉落/成功率/属性随机要"与原版可复现一致"，
// golden 化的前提是随机源逐值相同。.NET 6+ 的 new Random(seed) 仍使用与
// .NET Framework 相同的 Knuth 减法算法（Random.Net5CompatSeedImpl），
// 本文件是其逐行移植；向量由 tools/goldenrand 产出（internal/util/testdata）。
//
// 关键实现细节：**全部算术必须是 int32**。该算法的中间值依赖 32 位有符号
// 回绕保持有界（例如 k 循环里"正数减大负数"会回绕成负数再被 += MBIG 拉回）；
// Go 的 int 在 64 位平台上不回绕，用 int 会产出越界值（与 C# int 的差异）。
package util

import "math"

// mbig/mseed 与 .NET Random 源码常量一致。
const (
	mbig  int32 = math.MaxInt32 // 2147483647
	mseed int32 = 161803398
)

// Rand 是 System.Random(seed) 的 Knuth 减法算法移植（非并发安全；
// 并发场景由调用方加锁或各自持有实例——与原版 [ThreadStatic] 语义对齐）。
type Rand struct {
	seedArray [56]int32
	inext     int
	inextp    int
}

// NewRand 以指定种子构造（对应 new System.Random(seed)）。
func NewRand(seed int) *Rand {
	r := &Rand{}
	r.Seed(seed)
	return r
}

// Seed 重置种子（对应 .NET 10 CompatPrng.Initialize；int32 回绕语义与 C# 一致）。
//
// 注意：.NET 10 的种子填充是**修改版** Knuth 减法算法（dotnet/runtime#23198），
// 与 .NET Framework 经典实现不同——数组存的是 mk（非 mj），且链条为
// mk = mj - mk → mj = seedArray[ii]。移植以 .NET 10 源码为准，golden 锁定。
func (r *Rand) Seed(seed int) {
	var subtraction int32
	if seed == math.MinInt32 {
		subtraction = math.MaxInt32
	} else if seed < 0 {
		subtraction = int32(-seed)
	} else {
		subtraction = int32(seed)
	}

	seedArray := &r.seedArray
	mj := int32(161803398) - subtraction // 魔数基于黄金分割率
	seedArray[55] = mj
	mk := int32(1)

	ii := 0
	for i := 1; i < 55; i++ {
		ii += 21
		if ii >= 55 {
			ii -= 55
		}
		seedArray[ii] = mk
		mk = mj - mk
		if mk < 0 {
			mk += mbig
		}
		mj = seedArray[ii]
	}

	for k := 1; k < 5; k++ {
		for i := 1; i < 56; i++ {
			n := i + 30
			if n >= 55 {
				n -= 55
			}
			seedArray[i] -= seedArray[1+n]
			if seedArray[i] < 0 {
				seedArray[i] += mbig
			}
		}
	}
	r.inext = 0
	r.inextp = 21
}

// internalSample 对应 Random.InternalSample。
func (r *Rand) internalSample() int32 {
	locINext := r.inext + 1
	if locINext >= 56 {
		locINext = 1
	}
	locINextp := r.inextp + 1
	if locINextp >= 56 {
		locINextp = 1
	}
	retVal := r.seedArray[locINext] - r.seedArray[locINextp]
	if retVal == mbig {
		retVal--
	}
	if retVal < 0 {
		retVal += mbig
	}
	r.seedArray[locINext] = retVal
	r.inext = locINext
	r.inextp = locINextp
	return retVal
}

// sample 对应 Random.Sample()。
func (r *Rand) sample() float64 {
	return float64(r.internalSample()) * (1.0 / float64(mbig))
}

// NextBare 对应无参 Next()：[0, int.MaxValue)。
func (r *Rand) NextBare() int { return int(r.internalSample()) }

// Next 对应 Next(minValue, maxValue)：含 min、不含 max（小区间路径，
// (int)(Sample()*range)+min；游戏内区间都远小于 int 上限，不实现大区间分支）。
func (r *Rand) Next(minValue, maxValue int) int {
	rangeLen := maxValue - minValue
	if rangeLen <= 0 {
		// 原版对 min>=max 有不同分支/异常；游戏内不出现，返回 min 以便排查。
		return minValue
	}
	return int(r.sample()*float64(rangeLen)) + minValue
}

// NextDouble 对应 NextDouble()：[0, 1)。
func (r *Rand) NextDouble() float64 { return r.sample() }

// NextRandomChance 对应 Rand.NextRandomBool(double chance)：chance∈[0,1]，
// lot=NextDouble()，lot<=chance 判真（chance==0 短路 false）。用于区域技能
// 按距离衰减的命中概率（AreaSkillSettings.HitChancePerDistanceMultiplier）。
func (r *Rand) NextRandomChance(chance float64) bool {
	if chance == 0 {
		return false
	}
	return r.NextDouble() <= chance
}

// NextRandomBool 对应 Rand.NextRandomBool(percent)：
// percent∈(0,100) 时 a=Next(0,100)，a <= percent 判真——
// 注意这是 OpenMU 的**实际语义**（percent=50 命中率 51/100），不得"修正"。
func (r *Rand) NextRandomBool(percent int) bool {
	if percent == 0 {
		return false
	}
	if percent == 100 {
		return true
	}
	return r.Next(0, 100) <= percent
}
