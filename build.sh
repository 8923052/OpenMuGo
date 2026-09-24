#!/usr/bin/env bash
# MuGoServer 编译脚本（Git Bash / Linux 通用）。
#
# 用法:
#   ./build.sh                          编译当前平台（默认不内置任何测试账号）
#   ./build.sh linux amd64              交叉编译指定 GOOS/GOARCH
#   ./build.sh -t                       编译前先执行 go vet + go test
#   ./build.sh -s openmu                编译产物内置 OpenMU 测试账号（test0..test9）
#   ./build.sh -o build                 产物输出到 build 子目录
#   ./build.sh -s openmu linux arm64    组合使用
#
# 选项:
#   -t, --test            编译前执行 go vet + go test
#   -s, --seed <openmu|none>
#                         烧入产物的默认测试数据（默认 none；也可用环境变量 SEED）
#                           none   = 空账号库（默认，接 DB/自定义种子/生产构建）
#                           openmu = 内置 OpenMU 同款 test0..test9 内存账号
#                         运行时仍可用 ./mugo -seed=... 覆盖。
#   -o, --out <dir>       产物输出目录（默认当前目录，即可用 -o build 输出到子目录）
#   -h, --help            显示本帮助
#
# 位置参数:
#   GOOS GOARCH           交叉编译目标（缺省为本机平台）
#
# 产物默认输出到当前目录（脚本所在的 OpenMUGo 根目录），可用 -o 指定其他目录。
set -euo pipefail

# 固定工作目录为脚本所在的模块根目录，避免从其他目录调用时路径漂移。
cd "$(dirname "$0")"

RUN_TEST=0
SEED="${SEED:-none}"
OUT_DIR="${OUT_DIR:-.}"

usage() {
    sed -n '2,25p' "$0" | sed 's/^# \{0,1\}//'
}

die() {
    echo "错误: $*" >&2
    exit 1
}

# 选项解析：命名选项可出现在 GOOS/GOARCH 位置参数前后任意位置。
POSITIONAL=()
while [ $# -gt 0 ]; do
    case "$1" in
        -t|--test)
            RUN_TEST=1
            shift
            ;;
        -s|--seed)
            [ $# -ge 2 ] || die "$1 需要一个值: openmu|none"
            SEED="$2"
            shift 2
            ;;
        --seed=*)
            SEED="${1#*=}"
            shift
            ;;
        -o|--out)
            [ $# -ge 2 ] || die "$1 需要一个目录参数"
            OUT_DIR="$2"
            shift 2
            ;;
        --out=*)
            OUT_DIR="${1#*=}"
            shift
            ;;
        -h|--help)
            usage
            exit 0
            ;;
        --)
            shift
            POSITIONAL+=("$@")
            break
            ;;
        -*)
            die "未知选项: $1（用 -h 查看用法）"
            ;;
        *)
            POSITIONAL+=("$1")
            shift
            ;;
    esac
done

case "$SEED" in
    openmu|none) ;;
    *) die "非法 --seed='$SEED'，仅支持 openmu|none" ;;
esac

# 无位置参数时保留 "$@" 为空（不能展开含空串的数组，否则会被当成一个空 GOOS）。
if [ "${#POSITIONAL[@]}" -gt 0 ]; then
    set -- "${POSITIONAL[@]}"
else
    set --
fi
GOOS="${1:-$(go env GOOS)}"
GOARCH="${2:-$(go env GOARCH)}"

BIN_NAME="mugo"
[ "$GOOS" = "windows" ] && BIN_NAME="mugo.exe"
OUT="${OUT_DIR%/}/${BIN_NAME}"

echo "==> 环境检查"
command -v go >/dev/null 2>&1 || { echo "错误: 未找到 go，请先安装 Go 1.23+"; exit 1; }
go version
echo "    目标平台 : ${GOOS}/${GOARCH}"
echo "    测试数据 : ${SEED}（编译期烧入默认值，运行时 -seed 可覆盖）"

if [ "$RUN_TEST" = "1" ]; then
    echo "==> go vet ./..."
    go vet ./...
    echo "==> go test ./..."
    go test ./...
fi

echo "==> 编译 -> ${OUT}"
mkdir -p "$OUT_DIR"
# -X main.defaultSeed 注入种子默认值；-s -w 去符号表缩小体积。
LDFLAGS="-s -w -X main.defaultSeed=${SEED}"
CGO_ENABLED=0 GOOS="$GOOS" GOARCH="$GOARCH" \
    go build -trimpath -ldflags "$LDFLAGS" -o "$OUT" ./cmd/mugo

echo "==> 完成: ${OUT}"
ls -lh "$OUT"
