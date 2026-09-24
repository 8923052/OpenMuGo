// goldencrypto 用官方 MUnique.OpenMU.Network 0.9.10 的管线产出加密交叉向量。
// 仅在协议版本升级时运行：dotnet run --project tools/goldencrypto -- <输出目录>
//
// 输出 golden.json：
//   s2c_sealed:  GS 发送链（SimpleModulus 73326 加密）对连续多帧的输出，
//                Go 的 SimpleModulus(DefaultServerEncryptKeys).Seal 必须逐字节相等。
//   c2s_sealed:  客户端发送链（Xor32 -> SimpleModulus 128079 加密）输出，
//                Go 的 S6E3ServerCodec.Open 必须还原为对应明文。
//   pass_through: C1 明文帧经过加密管线必须原样输出。
using System.Buffers;
using System.IO.Pipelines;
using System.Text;
using System.Text.Json;
using MUnique.OpenMU.Network;
using MUnique.OpenMU.Network.SimpleModulus;
using MUnique.OpenMU.Network.Xor;

if (args.Length < 1)
{
    Console.Error.WriteLine("用法: goldencrypto <输出目录>");
    return 1;
}
Directory.CreateDirectory(args[0]);

var opts = new JsonSerializerOptions { WriteIndented = true, Encoder = System.Text.Encodings.Web.JavaScriptEncoder.UnsafeRelaxedJsonEscaping };

// ---- 明文帧（C3 + SubCode 布局，长度字段真实）----
// Frame 构造 C3 + SubCode 帧 [C3][len][code][sub][payload...]。
byte[] Frame(byte type, byte code, byte subCode, params byte[] payload)
{
    var p = new byte[4 + payload.Length];
    p[0] = type;
    p[2] = code;
    p[3] = subCode;
    payload.CopyTo(p, 4);
    p[1] = (byte)p.Length;
    return p;
}

byte[] FrameC4(byte code, byte subCode, byte[] payload)
{
    var p = new byte[5 + payload.Length];
    p[0] = 0xC4;
    p[1] = (byte)(p.Length >> 8);
    p[2] = (byte)(p.Length & 0xFF);
    p[3] = code;
    p[4] = subCode;
    payload.CopyTo(p, 5);
    return p;
}

var s2cPlain = new[]
{
    Frame(0xC3, 0xF1, 0x00),                                                                   // 最小帧
    Frame(0xC3, 0xF1, 0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06),                                 // 首块塞满（counter+7）
    Frame(0xC3, 0xF1, 0x00, 0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88),                     // 跨到第二块
    Frame(0xC3, 0xF3, 0x00, Enumerable.Range(0, 30).Select(i => (byte)(i * 13 + 5)).ToArray()), // 多块
    FrameC4(0x12, 0x00, Enumerable.Range(0, 40).Select(i => (byte)(i ^ 0xA5)).ToArray()),       // C4 双字节长度
};

var c2sPlain = new[]
{
    Frame(0xC3, 0xF1, 0x00, 0x01, 0x02, 0x03, 0x04),                                           // 带 subcode 的 C3
    Frame(0xC3, 0xF1, 0x00, 0xDE, 0xAD, 0xBE, 0xEF, 0x00, 0x11, 0x22, 0x33),
    Frame(0xC3, 0x0E, 0x00, Enumerable.Range(0, 18).Select(i => (byte)(0x80 + i)).ToArray()),
};

// 经加密管线逐帧写出，返回密文帧集合（按密文帧长度字段切分）。
async Task<byte[][]> RunPipelineAsync(IPipelinedEncryptor encryptor, Pipe target, byte[][] frames)
{
    var results = new List<byte[]>();
    foreach (var frame in frames)
    {
        await encryptor.Writer.WriteAsync(frame);
        var rr = await target.Reader.ReadAsync();
        foreach (var seg in Collect(rr.Buffer))
        {
            results.Add(seg);
        }
        target.Reader.AdvanceTo(rr.Buffer.End);
    }
    return results.ToArray();
}

// ReadResult.Buffer 可能是多段；按帧长度边界再切分，确保每个元素是恰好一帧密文。
List<byte[]> Collect(ReadOnlySequence<byte> buffer)
{
    var list = new List<byte[]>();
    var rest = buffer;
    while (!rest.IsEmpty)
    {
        var first3 = rest.Slice(0, Math.Min(3, rest.Length)).ToArray();
        var size = PacketSizeLike(first3);
        if (rest.Length < size)
        {
            // 理论上同步管线不会出现半包；若出现直接保留剩余并等待（此处按错误处理）。
            throw new InvalidOperationException("管线返回了不完整的帧");
        }
        list.Add(rest.Slice(0, size).ToArray());
        rest = rest.Slice(size);
    }
    return list;
}

int PacketSizeLike(byte[] hdr)
{
    return hdr[0] == 0xC2 || hdr[0] == 0xC4
        ? (hdr[1] << 8) | hdr[2]
        : hdr[1];
}

// S2C：GS 发送链 = SM(73326 enc)，无 Xor32（PipelinedEncryptor 默认构造）。
var s2cPipe = new Pipe();
var s2cEnc = new PipelinedSimpleModulusEncryptor(s2cPipe.Writer, PipelinedSimpleModulusEncryptor.DefaultServerKey);
var s2cSealed = await RunPipelineAsync(s2cEnc, s2cPipe, s2cPlain);

// C2S：客户端发送链 = Xor32 -> SM(128079 enc)（MuMain ConnectInner 同构）。
var c2sPipe = new Pipe();
var c2sSm = new PipelinedSimpleModulusEncryptor(c2sPipe.Writer, PipelinedSimpleModulusEncryptor.DefaultClientKey);
var c2sXor = new PipelinedXor32Encryptor(c2sSm.Writer);
var c2sSealed = await RunPipelineAsync(c2sXor, c2sPipe, c2sPlain);

// 透传：C1 帧经过两条管线都应原样。
var ptPipe = new Pipe();
var ptEnc = new PipelinedSimpleModulusEncryptor(ptPipe.Writer, PipelinedSimpleModulusEncryptor.DefaultServerKey);
var ptPlain = new byte[] { 0xC1, 0x04, 0x00, 0x01 };
await ptEnc.Writer.WriteAsync(ptPlain);
var ptRr = await ptPipe.Reader.ReadAsync();
var ptOut = ptRr.Buffer.ToArray().ToArray();
ptPipe.Reader.AdvanceTo(ptRr.Buffer.End);

var doc = new
{
    packets_version = "0.9.10",
    s2c_plain = s2cPlain.Select(Convert.ToHexString).ToArray(),
    s2c_sealed = s2cSealed.Select(Convert.ToHexString).ToArray(),
    c2s_plain = c2sPlain.Select(Convert.ToHexString).ToArray(),
    c2s_sealed = c2sSealed.Select(Convert.ToHexString).ToArray(),
    pass_through_plain = Convert.ToHexString(ptPlain),
    pass_through_out = Convert.ToHexString(ptOut),
};
File.WriteAllText(Path.Combine(args[0], "golden.json"), JsonSerializer.Serialize(doc, opts), new UTF8Encoding(false));
Console.WriteLine($"goldencrypto: s2c={s2cSealed.Length} c2s={c2sSealed.Length} 向量已写出");
return 0;
