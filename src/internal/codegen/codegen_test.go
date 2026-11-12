package codegen

import "testing"

func TestPinyinInitials(t *testing.T) {
	cases := map[string]string{
		"车间":   "CJ",
		"厂检":   "CJ", // 与车间同首字母：冲突时追加数字后缀 → CJ1
		"产线":   "CX",
		"班组":   "BZ",
		"客户":   "KH",
		"供应商": "GYS",
		"仓库":   "CK",
		"物料":   "WL",
		"字典项": "ZDX",
		"产品":   "CP",
		"工序":   "GX",
		"一车间A区": "YCJAQ",
	}
	for name, want := range cases {
		if got := PinyinInitials(name); got != want {
			t.Errorf("PinyinInitials(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestPrefixCandidates(t *testing.T) {
	got := prefixCandidates("CJ")
	if got[0] != "CJ" || got[1] != "CJ1" || got[9] != "CJ9" || got[10] != "CJ10" {
		t.Fatalf("unexpected candidates head: %v", got[:11])
	}
	if len(got) != 1+9+90 {
		t.Fatalf("candidates length = %d, want 100", len(got))
	}
	if got := prefixCandidates(""); got[0] != "CODE" {
		t.Fatalf("empty desired should fall back to CODE, got %q", got[0])
	}
}
