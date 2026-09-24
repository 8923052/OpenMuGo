package util

// float.go —— T0-e：C# 浮点/舍入语义对齐工具。
//
// C# 与 Go 的算术同属 IEEE 754，数值运算天然一致；差异集中在"舍入入口"：
//   - Math.Round(x) 默认 MidpointRounding.ToEven（银行家舍入）→ Go math.RoundToEven；
//     Go 的 math.Round 是远离零（AwayFromZero），**不可混用**；
//   - C# 整型转换 (int)x 是向零截断 → Go int(x) 同；
//   - double→float 的转换都是"舍入到最近偶数尾数"，Go float32(d) 同；
//   - Math.Pow 与 Go math.Pow 在个别边界 ulp 上可能有实现差异——战斗公式 golden
//     化时若涉及幂运算，以 C# 侧向量为基准并做 ulp 容差（见 attribute 包注释）。
//
// 本包把这些入口显式命名，防止后续代码随手用错 math.Round（doc/10 D8）。

import "math"

// RoundToEven 对应 C# Math.Round(x)（默认银行家舍入）。
func RoundToEven(x float64) float64 { return math.RoundToEven(x) }

// RoundAwayFromZero 对应 C# Math.Round(x, MidpointRounding.AwayFromZero)。
func RoundAwayFromZero(x float64) float64 { return math.Round(x) }

// TruncateToFloat32 对应 C# 的 double → float 隐式转换（舍入到 float32）。
func TruncateToFloat32(x float64) float32 { return float32(x) }

// TruncateToInt 对应 C# 的 (int)x（向零截断，非四舍五入）。
func TruncateToInt(x float64) int { return int(x) }
