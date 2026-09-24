// Package playershop 对应 OpenMU `src/GameServer/MessageHandler/PlayerShop/`（6 个 .cs）。
//
// 入站：购买请求、关闭商店、开店、物品列表请求、设置物品价格、商店组包。
//
// 逻辑在 gamelogic/action（PlayerActions/PlayerStore/），出站在 view/remote/playershop
// （ShowShopItemListPlugIn 含 Extended 变体、PlayerShopOpened/ClosedPlugIn）。
package playershop
