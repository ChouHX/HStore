package store

import (
	"strconv"
	"strings"
)

func stringsTrimRight(s, cut string) string { return strings.TrimRight(s, cut) }

// ToFloat 把 any 安全转成 float64。
func ToFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case string:
		if f, e := strconv.ParseFloat(strings.TrimSpace(n), 64); e == nil {
			return f, true
		}
	}
	return 0, false
}

// ToInt 把 any 安全转成 int。
func ToInt(v any) (int, bool) {
	f, ok := ToFloat(v)
	return int(f), ok
}

// ToString 把 any 安全转成 string。
func ToString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// TrimFloat 金额展示：保留 4 位后去掉多余的 0。
func TrimFloat(v float64) string {
	s := strconv.FormatFloat(v, 'f', 4, 64)
	s = strings.TrimRight(s, "0")
	s = strings.TrimRight(s, ".")
	if s == "" || s == "-0" {
		return "0"
	}
	return s
}
