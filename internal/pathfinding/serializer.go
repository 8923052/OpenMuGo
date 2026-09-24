package pathfinding

import "errors"

// serializer.go —— 预表序列化，对照 PreCalculation/ 的 CompactPathsSerializer.cs /
// NormalPathsSerializer.cs / PathsSerializeExtensions.cs。用于把预计算结果落盘、
// 下次启动直接加载（原版 PreCalculator 全图预计算代价极高，序列化即可复用）。
//
// 与原版的差异登记：原版 Deserialize 的 elementSize 常量（Compact=6 / Normal=8）与
// 每条记录实际字节数（Compact=4 / Normal=6）不一致，靠流位置判断收尾；Go 侧改为
// "按记录定长读到缓冲区结束"，保证序列化↔反序列化精确往返。

// PathInfoFormat 是序列化格式（原版 PathsSerializeExtensions.PathInfoFormat）。
type PathInfoFormat byte

const (
	// FormatCompact 用差值压缩：起点 2 字节 + 终点差 1 字节 + 首步差 1 字节 = 4 字节/条。
	// 仅适用于 start↔end/nextStep 的坐标差在 -8..+7 内（maximumRange < 8）。
	FormatCompact PathInfoFormat = 0
	// FormatNormal 每点 2 字节：6 字节/条。
	FormatNormal PathInfoFormat = 1
)

// ErrCompactRange 表示差值超出压缩格式可表示范围（±8）。
var ErrCompactRange = errors.New("pathfinding: compact format 只能表示 -8..7 的坐标差")

// SerializePaths 把表项序列化为字节流（首字节为格式标记；原版 SerializeToStream）。
func SerializePaths(infos []PathInfo, format PathInfoFormat) []byte {
	out := make([]byte, 0, 1+len(infos)*6)
	out = append(out, byte(format))
	for _, info := range infos {
		switch format {
		case FormatCompact:
			de, _ := calcDiff(info.Combination.Start, info.Combination.End)
			dn, _ := calcDiff(info.Combination.Start, info.NextStep)
			out = append(out, info.Combination.Start.X, info.Combination.Start.Y, de, dn)
		default:
			out = append(out,
				info.Combination.Start.X, info.Combination.Start.Y,
				info.Combination.End.X, info.Combination.End.Y,
				info.NextStep.X, info.NextStep.Y)
		}
	}
	return out
}

// SerializePathsCompact 序列化并严格校验差值（超出 ±8 返回 ErrCompactRange）。
func SerializePathsCompact(infos []PathInfo) ([]byte, error) {
	out := make([]byte, 0, 1+len(infos)*4)
	out = append(out, byte(FormatCompact))
	for _, info := range infos {
		de, err := calcDiff(info.Combination.Start, info.Combination.End)
		if err != nil {
			return nil, err
		}
		dn, err := calcDiff(info.Combination.Start, info.NextStep)
		if err != nil {
			return nil, err
		}
		out = append(out, info.Combination.Start.X, info.Combination.Start.Y, de, dn)
	}
	return out, nil
}

// DeserializePaths 从字节流还原表项（读首字节判格式；原版 DeserializeFromStream）。
func DeserializePaths(data []byte) ([]PathInfo, error) {
	if len(data) < 1 {
		return nil, errors.New("pathfinding: 空的预表数据")
	}
	format := PathInfoFormat(data[0])
	body := data[1:]
	var infos []PathInfo
	switch format {
	case FormatCompact:
		if len(body)%4 != 0 {
			return nil, errors.New("pathfinding: compact 预表长度不是 4 的倍数")
		}
		for i := 0; i+4 <= len(body); i += 4 {
			start := Point{X: body[i], Y: body[i+1]}
			de, dn := body[i+2], body[i+3]
			// calcDiff 存了 diff+8，此处还原时减回 8（保证精确往返）。
			end := Point{X: byte(int(start.X) + int(de>>4&0x0F) - 8), Y: byte(int(start.Y) + int(de&0x0F) - 8)}
			next := Point{X: byte(int(start.X) + int(dn>>4&0x0F) - 8), Y: byte(int(start.Y) + int(dn&0x0F) - 8)}
			infos = append(infos, PathInfo{Combination: PointCombination{Start: start, End: end}, NextStep: next})
		}
	default:
		if len(body)%6 != 0 {
			return nil, errors.New("pathfinding: normal 预表长度不是 6 的倍数")
		}
		for i := 0; i+6 <= len(body); i += 6 {
			start := Point{X: body[i], Y: body[i+1]}
			end := Point{X: body[i+2], Y: body[i+3]}
			next := Point{X: body[i+4], Y: body[i+5]}
			infos = append(infos, PathInfo{Combination: PointCombination{Start: start, End: end}, NextStep: next})
		}
	}
	return infos, nil
}

// calcDiff 计算 start→end 的压缩差值字节（+8 偏移，各 4 位；原版 CompactPathsSerializer.CalcDiff）。
func calcDiff(start, end Point) (byte, error) {
	dx := int(end.X) - int(start.X) + 8
	dy := int(end.Y) - int(start.Y) + 8
	if dx < 0 || dx > 15 || dy < 0 || dy > 15 {
		return 0, ErrCompactRange
	}
	return byte((dx<<4)&0xF0 | (dy & 0x0F)), nil
}
