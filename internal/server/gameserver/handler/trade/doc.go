// Package trade 对应 OpenMU `src/GameServer/MessageHandler/Trade/`（5 个 .cs）。
//
// 入站：交易接受、交易按钮、交易取消、交易金额、交易请求。
//
// 逻辑在 gamelogic/action（PlayerActions/Trade/ 的 BaseTradeAction/TradeAcceptAction/
// TradeButtonAction/TradeCancelAction/TradeMoneyAction/TradeRequestAction），
// 出站在 view/remote/trade。
package trade
