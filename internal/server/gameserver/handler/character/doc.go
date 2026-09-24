// Package character 对应 OpenMU `src/GameServer/MessageHandler/Character/`（13 个 .cs）
// 以及 `MessageHandler/` 根下与角色直接相关、未分域的 handler（含 075 变体）。
//
// 入站：角色列表、创建、删除、焦点、选角、加点、大师加点、按键配置、
// 角色移动（CharacterMoveHandlerPlugIn + 075）、行走（CharacterWalkHandlerPlugIn + 075、Base）。
//
// 变体命名沿用原版后缀：`_075`（如 characterwalk_075.go ← CharacterWalkHandlerPlugIn075.cs）。
//
// 逻辑在 gamelogic/action（PlayerActions/Character/ + PlayerMovement），
// 出站角色列表在 view/remote（ShowCharacterListPlugIn 系列）。
package character
