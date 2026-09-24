// Package api 是跨服务契约层，对应 OpenMU 的 MUnique.OpenMU.Interfaces（src/Interfaces，24 个主 .cs）。
//
// 分层约束（doc/12 §2.4、doc/14 §3 P1）：
//   - 本包**只放接口与传输用 DTO**，不含任何业务逻辑；
//   - 本包**不得 import** internal/server/*、internal/gamelogic/*、internal/persistence、
//     internal/version、internal/proto —— 只有"服务层依赖契约层"这一个方向；
//   - gamelogic / persistence 也不得 import 本包（版本无关铁律的延伸）。
//
// 一个 C# 类 → 一个 Go 文件，文件头注明对应 OpenMU 路径；原版未落位的契约不预先占位（见 doc/14 §3 P1 的取舍表）。
package api
