// Tool goldenrand：为 Go 侧 System.Random 复刻（internal/util/rand.go）产出固定种子序列向量。
// System.Random(seed) 在 .NET 6+ 仍用与 .NET Framework 相同的 Knuth 减法算法
// （Random.Net5CompatSeedImpl），这是"可复现随机"（掉落/成功率 golden 化）的前提。
// 用法: goldenrand <output-file>
namespace GoldenRand;

using System.Text.Json;

internal static class Program
{
    private static async Task<int> Main(string[] args)
    {
        if (args.Length < 1)
        {
            Console.Error.WriteLine("usage: goldenrand <output-file>");
            return 1;
        }

        var vectors = new List<object>();
        foreach (var seed in new[] { 1, 42, 12345, int.MaxValue, int.MinValue })
        {
            var r = new Random(seed);
            vectors.Add(new
            {
                seed,
                next_0_100 = Enumerable.Range(0, 20).Select(_ => r.Next(0, 100)).ToArray(),
            });
            r = new Random(seed);
            vectors.Add(new
            {
                seed,
                next_5_10 = Enumerable.Range(0, 10).Select(_ => r.Next(5, 10)).ToArray(),
            });
            r = new Random(seed);
            vectors.Add(new
            {
                seed,
                next_double = Enumerable.Range(0, 10).Select(_ => r.NextDouble()).Select(d => d.ToString("R")).ToArray(),
            });
            r = new Random(seed);
            vectors.Add(new
            {
                seed,
                next_bare = Enumerable.Range(0, 10).Select(_ => r.Next()).ToArray(),
            });
        }

        var options = new JsonSerializerOptions { WriteIndented = true };
        await File.WriteAllTextAsync(args[0], JsonSerializer.Serialize(vectors, options)).ConfigureAwait(false);
        Console.WriteLine($"rand vectors → {Path.GetFullPath(args[0])}");
        return 0;
    }
}
