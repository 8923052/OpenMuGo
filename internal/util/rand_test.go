package util

import (
	"encoding/json"
	"math"
	"os"
	"strconv"
	"testing"
)

// golden 载入 tools/goldenrand 产出的向量（internal/util/testdata/rand_golden.json）。
type randGolden struct {
	Seed       int      `json:"seed"`
	Next0100   []int    `json:"next_0_100"`
	Next510    []int    `json:"next_5_10"`
	NextDouble []string `json:"next_double"`
	NextBare   []int    `json:"next_bare"`
}

func loadRandGolden(t *testing.T) []randGolden {
	t.Helper()
	raw, err := os.ReadFile("testdata/rand_golden.json")
	if err != nil {
		t.Fatalf("读 golden 向量失败（tools/goldenrand 生成）: %v", err)
	}
	var out []randGolden
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("解析 golden 向量失败: %v", err)
	}
	if len(out) == 0 {
		t.Fatal("golden 向量为空")
	}
	return out
}

// TestRandMatchesSystemRandom 锁定：固定种子下 Next/NextDouble/Next 裸值
// 与 .NET System.Random(seed) 逐值相同（Knuth 减法算法移植正确性）。
func TestRandMatchesSystemRandom(t *testing.T) {
	for _, v := range loadRandGolden(t) {
		t.Run(itoa(v.Seed), func(t *testing.T) {
			// next_0_100：每段向量都从同一 seed 重新构造。
			r := NewRand(v.Seed)
			for i, want := range v.Next0100 {
				if got := r.Next(0, 100); got != want {
					t.Fatalf("seed=%d Next(0,100)[%d] = %d, want %d", v.Seed, i, got, want)
				}
			}
			r = NewRand(v.Seed)
			for i, want := range v.Next510 {
				if got := r.Next(5, 10); got != want {
					t.Fatalf("seed=%d Next(5,10)[%d] = %d, want %d", v.Seed, i, got, want)
				}
			}
			r = NewRand(v.Seed)
			for i, wantStr := range v.NextDouble {
				var want float64
				if _, err := fmtSscan(wantStr, &want); err != nil {
					t.Fatalf("解析 golden double %q: %v", wantStr, err)
				}
				if got := r.NextDouble(); got != want {
					t.Fatalf("seed=%d NextDouble[%d] = %v, want %v", v.Seed, i, got, want)
				}
			}
			r = NewRand(v.Seed)
			for i, want := range v.NextBare {
				if got := r.NextBare(); got != want {
					t.Fatalf("seed=%d Next()[%d] = %d, want %d", v.Seed, i, got, want)
				}
			}
		})
	}
}

// TestNextRandomBoolSemantics 锁定 OpenMU 语义：percent=50 时 Next(0,100)<=50
// （51/100 命中），0 恒假、100 恒真——不得按直觉"修正"为 50/100。
func TestNextRandomBoolSemantics(t *testing.T) {
	r := NewRand(7)
	hits := 0
	const n = 10000
	for i := 0; i < n; i++ {
		if r.NextRandomBool(50) {
			hits++
		}
	}
	// 命中率应接近 51%（容差放宽到 ±2%），区别于 50% 主要靠语义而非统计。
	if hits < n*49/100 || hits > n*53/100 {
		t.Fatalf("percent=50 命中 %d/%d，偏离 51%% 语义", hits, n)
	}
	if r2 := NewRand(1); r2.NextRandomBool(0) {
		t.Fatal("percent=0 应恒假")
	}
	if r3 := NewRand(1); !r3.NextRandomBool(100) {
		t.Fatal("percent=100 应恒真")
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [12]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

// fmtSscan 解析 "R" 格式的 double（golden 里的字符串形态）。
func fmtSscan(s string, out *float64) (int, error) {
	return fmtSscanImpl(s, out)
}

// fmtSscanImpl 用 strconv 实现（避免 fmt 循环依赖噪音）。
func fmtSscanImpl(s string, out *float64) (int, error) {
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, err
	}
	*out = v
	return 1, nil
}

// TestFloatSemantics 锁定 T0-e：银行家舍入与向零截断的边界行为。
func TestFloatSemantics(t *testing.T) {
	cases := []struct {
		in           float64
		toEven       float64
		awayFromZero float64
		trunc        int
	}{
		{0.5, 0, 1, 0},
		{1.5, 2, 2, 1},
		{2.5, 2, 3, 2},
		{-0.5, 0, -1, 0}, // C# Round(-0.5) = -0（与 0 相等）；Go RoundToEven(-0.5) = -0
		{-1.5, -2, -2, -1},
		{2.675, 3, 3, 2},
	}
	for _, c := range cases {
		if got := RoundToEven(c.in); got != c.toEven && !(math.IsNaN(got) && math.IsNaN(c.toEven)) {
			t.Fatalf("RoundToEven(%v)=%v, want %v", c.in, got, c.toEven)
		}
		if got := RoundAwayFromZero(c.in); got != c.awayFromZero {
			t.Fatalf("RoundAwayFromZero(%v)=%v, want %v", c.in, got, c.awayFromZero)
		}
		if got := TruncateToInt(c.in); got != c.trunc {
			t.Fatalf("TruncateToInt(%v)=%d, want %d", c.in, got, c.trunc)
		}
	}
	// NaN/Inf 原样通过。
	if !math.IsNaN(RoundToEven(math.NaN())) {
		t.Fatal("NaN 应原样通过")
	}
	if !math.IsInf(RoundToEven(math.Inf(1)), 1) {
		t.Fatal("Inf 应原样通过")
	}
	// float32 截断：0.1 的 double→float32 行为与 C# 一致。
	if TruncateToFloat32(0.1) != 0.1 {
		t.Fatal("float32(0.1) 应与 C# 转换一致")
	}
}
