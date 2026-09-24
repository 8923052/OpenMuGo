# spec — 协议规格（唯一真相源）

每个子目录是一个版本的 OpenMU 协议 XML，**原样取自对应版本的 NuGet 包
`MUnique.OpenMU.Network.Packets` 的 contentFiles**（MuMain 客户端构建时引用的
就是同一个包），不要手工修改。

当前版本：`0.9.10`（与 MuMain `ClientLibrary/MUnique.Client.Library.csproj` 中
`<OpenMuPacketsVersion>` 对齐）。

## 为什么不直接引用 OpenMU 源码里的 XML

OpenMU 仓库工作区的 XML 可能领先于已发布的 NuGet 包；MuMain 实际按 NuGet
contentFiles 的 XML 生成代码，因此以 NuGet 包为准才能保证与客户端字节一致。

## 升级到新版本

```powershell
# 1. 从 nuget.org 取新版本 nupkg（zip），解压 contentFiles/any/net10.0 到 spec/<版本>/
# 2. 一键切换 + 重新生成 + 重刷 golden + 测试：
./tools/regen.ps1 -Version <版本> -Golden
# 3. 审查 git diff：包/字段变更、golden 向量差异、生成器对新增类型的警告
```

若新版 XML 含生成器尚不支持的写法，`muprotogen` 会直接报错或列出警告
（如 `UseCustomIndexer`），先补生成器再合入。
