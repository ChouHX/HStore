package service

import "testing"

func TestCeilToCents(t *testing.T) {
	cases := []struct {
		amount float64
		want   int64
	}{
		{0.064, 7},  // 有余数 → 向上取整
		{0.06, 6},   // 恰好 6 分，不能因为 0.06*100=6.000000000000001 变成 7
		{0.07, 7},   // 已到分，不动
		{0.001, 1},  // 不足 1 分 → 1 分
		{0.010, 1},  // 恰好 1 分
		{0.192, 20}, // 0.064 × 3
		{1.12, 112}, // 常见售价
		{2.24, 224},
		{6.68, 668},
		{0, 0},
	}
	for _, c := range cases {
		if got := ceilToCents(c.amount); got != c.want {
			t.Errorf("ceilToCents(%v) = %d, want %d", c.amount, got, c.want)
		}
	}
}
