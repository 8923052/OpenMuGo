package version

import "testing"

func TestKey(t *testing.T) {
	// MuMain 1.04d：ASCII "20404"
	key := Key([]byte("20404"))
	if key == 0 {
		t.Fatal("版本键不应为 0")
	}
	// 手工核对：dword LE('2','0','4','0')=0x30343032，*256 + '4'(0x34)
	want := uint64(0x30343032)*256 + 0x34
	if key != want {
		t.Fatalf("版本键=%d, want %d", key, want)
	}
	if Key([]byte{1, 2, 3}) != 0 {
		t.Fatal("3 字节旧版本键应为 0")
	}
}

func TestCompareTo(t *testing.T) {
	muMain := ClientVersion{Season: 106, Episode: 3, Language: LanguageEnglish}

	if got := muMain.CompareTo(ClientVersion{Season: 106, Episode: 3, Language: LanguageEnglish}); got != 0 {
		t.Fatalf("同版本应为 0，得到 %d", got)
	}
	// 0.75 通配语言：(27139-75)*10+1
	if got := muMain.CompareTo(ClientVersion{Season: 0, Episode: 75, Language: LanguageInvariant}); got != 270641 {
		t.Fatalf("对 0.75 的比较=%d, want 270641", got)
	}
	// GMO 6.3 通配语言：(27139-1539)*10+1
	if got := muMain.CompareTo(ClientVersion{Season: 6, Episode: 3, Language: LanguageInvariant}); got != 256001 {
		t.Fatalf("对 6.3 的比较=%d, want 256001", got)
	}
	// 语言不兼容：对方为非通配且与本方不同 → 极小值
	if got := muMain.CompareTo(ClientVersion{Season: 6, Episode: 3, Language: LanguageJapanese}); got != unsuitable {
		t.Fatalf("语言不兼容应为 unsuitable，得到 %d", got)
	}
	invariant := ClientVersion{Season: 6, Episode: 3, Language: LanguageInvariant}
	if got := invariant.CompareTo(ClientVersion{Season: 6, Episode: 3, Language: LanguageEnglish}); got != unsuitable {
		t.Fatalf("通配对本方具体语言也应不兼容，得到 %d", got)
	}
}

func TestConstraintSuitable(t *testing.T) {
	muMain := ClientVersion{Season: 106, Episode: 3, Language: LanguageEnglish}

	cases := []struct {
		name string
		c    Constraint
		want bool
	}{
		{"无约束", Constraint{}, true},
		{"同版本通配下限（+1 需通过）", AtLeast(ClientVersion{Season: 106, Episode: 3, Language: LanguageInvariant}), true},
		{"更低下限", AtLeast(ClientVersion{Season: 5, Episode: 0, Language: LanguageInvariant}), true},
		{"更高下限", AtLeast(ClientVersion{Season: 106, Episode: 4, Language: LanguageInvariant}), false},
		{"上限高于客户端", Below(ClientVersion{Season: 107, Episode: 0, Language: LanguageInvariant}), true},
		{"上限低于客户端（<=1 不得放过）", Below(ClientVersion{Season: 0, Episode: 89, Language: LanguageInvariant}), false},
		{"语言不兼容不得选中", AtLeast(ClientVersion{Season: 0, Episode: 75, Language: LanguageChinese}), false},
		{"区间命中", Between(ClientVersion{Season: 6, Episode: 3, Language: LanguageInvariant}, ClientVersion{Season: 106, Episode: 3, Language: LanguageInvariant}), true},
	}
	for _, tc := range cases {
		if got := tc.c.Suitable(muMain); got != tc.want {
			t.Errorf("%s: Suitable=%v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestOriginalDefinitions(t *testing.T) {
	defs := Original()
	if len(defs) != 4 {
		t.Fatalf("原版注册 4 个客户端版本，得到 %d", len(defs))
	}
	want := []struct {
		wire   string
		season uint8
		epis   uint8
		lang   ClientLanguage
	}{
		{"07500", 0, 75, LanguageInvariant},
		{"09504", 0, 95, LanguageEnglish},
		{"10404", 6, 3, LanguageEnglish},
		{"20404", 106, 3, LanguageEnglish},
	}
	for i, d := range defs {
		if string(d.Version[:]) != want[i].wire {
			t.Errorf("#%d 版本字节=%q, want %q", i, string(d.Version[:]), want[i].wire)
		}
		if d.Season != want[i].season || d.Episode != want[i].epis || d.Language != want[i].lang {
			t.Errorf("#%d 版本=%d.%d/%v, want %d.%d/%v", i, d.Season, d.Episode, d.Language,
				want[i].season, want[i].epis, want[i].lang)
		}
		if d.Key() != Key([]byte(want[i].wire)) {
			t.Errorf("#%d 键不一致", i)
		}
	}
}

func TestRegistry(t *testing.T) {
	r := NewRegistry([]GameClientDefinition{MuMain()})

	v, ok := r.Resolve(MuMain().Key())
	if !ok || v.Season != 106 || v.Episode != 3 {
		t.Fatalf("Resolve 未命中: %v %v", v, ok)
	}
	if _, ok := r.Resolve(Key([]byte("10404"))); ok {
		t.Fatal("未注册版本应不命中（本项目当前为白名单语义）")
	}
	d, ok := r.Default()
	if !ok || d != v {
		t.Fatalf("默认版本应为注册表第一条: %v %v", d, ok)
	}
	b, ok := r.VersionBytes(v)
	if !ok || string(b[:]) != "20404" {
		t.Fatalf("版本字节=%q ok=%v", string(b[:]), ok)
	}
	if len(r.Definitions()) != 1 {
		t.Fatal("定义数量错误")
	}
}

func TestRegistryEmpty(t *testing.T) {
	r := NewRegistry(nil)
	if _, ok := r.Default(); ok {
		t.Fatal("空注册表不应有默认版本")
	}
	if _, ok := r.Resolve(Key([]byte("20404"))); ok {
		t.Fatal("空注册表不应命中")
	}
}
