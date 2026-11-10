package platform

import (
	"strings"
	"unicode"

	"github.com/mozillazg/go-pinyin"
)

// TenantShortCodeBase 从租户名称生成 5 位标识：
// 中文取拼音首字母，字母和数字原样保留，不足 5 位补 0。
func TenantShortCodeBase(name, code string) string {
	var b strings.Builder
	args := pinyin.NewArgs()
	args.Style = pinyin.FirstLetter
	for _, r := range name {
		switch {
		case r < 128 && ((r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')):
			b.WriteRune(unicode.ToUpper(r))
		case unicode.Is(unicode.Han, r):
			if py := pinyin.Pinyin(string(r), args); len(py) > 0 && len(py[0]) > 0 && py[0][0] != "" {
				b.WriteString(strings.ToUpper(string([]rune(py[0][0])[0])))
			}
		}
		if b.Len() >= 5 {
			break
		}
	}
	if b.Len() == 0 {
		for _, r := range strings.ToUpper(code) {
			if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
				b.WriteRune(r)
			}
			if b.Len() >= 5 {
				break
			}
		}
	}
	result := b.String()
	if result == "" {
		return "T0000"
	}
	if len(result) > 5 {
		result = result[:5]
	}
	for len(result) < 5 {
		result += "0"
	}
	return result
}

// TenantCodeBase 取租户名称前两个字的拼音首字母作为租户编码；纯拉丁名称取前两个字符。
func TenantCodeBase(name, code string) string {
	args := pinyin.NewArgs()
	args.Style = pinyin.FirstLetter
	var b strings.Builder
	for _, r := range name {
		switch {
		case r < 128 && ((r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')):
			b.WriteRune(unicode.ToUpper(r))
		case unicode.Is(unicode.Han, r):
			if py := pinyin.Pinyin(string(r), args); len(py) > 0 && len(py[0]) > 0 && py[0][0] != "" {
				b.WriteString(strings.ToUpper(string([]rune(py[0][0])[0])))
			}
		}
		if b.Len() >= 2 {
			break
		}
	}
	if b.Len() == 0 {
		for _, r := range strings.ToUpper(code) {
			if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
				b.WriteRune(r)
			}
			if b.Len() >= 2 {
				break
			}
		}
	}
	result := b.String()
	if result == "" {
		return "T0"
	}
	for len(result) < 2 {
		result += "0"
	}
	return result[:2]
}

// ShortCodeCandidates 返回从 base 开始的一组候选标识，用于冲突时替换末位。
func ShortCodeCandidates(base string) []string {
	if len(base) != 5 {
		base = TenantShortCodeBase(base, base)
	}
	candidates := []string{base}
	const alphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	for _, c := range alphabet {
		candidates = append(candidates, base[:4]+string(c))
	}
	for _, a := range alphabet {
		for _, b := range alphabet {
			candidates = append(candidates, base[:3]+string(a)+string(b))
		}
	}
	return candidates
}

// UniqueShortCode 标识冲突时替换末位字符（数字或字母），保持 5 位。
func UniqueShortCode(base string, used map[string]bool) string {
	if len(base) != 5 {
		base = TenantShortCodeBase(base, base)
	}
	if !used[base] {
		return base
	}
	const alphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	for _, c := range alphabet {
		candidate := base[:4] + string(c)
		if !used[candidate] {
			return candidate
		}
	}
	return base
}
