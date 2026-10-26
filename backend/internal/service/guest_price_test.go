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

func TestMinUsdtCountForGuest(t *testing.T) {
	cases := []struct {
		unit, rate float64
		want       int
	}{
		{6, 7.2, 2},       // 谷歌邮箱 ¥6：1 件只有 0.83 USDT → 至少 2 件
		{0.064, 7.2, 113}, // ¥0.064 的邮箱：需要 113 件才够 1 USDT
		{8, 7.2, 1},       // 单价已超过 1 USDT
		{7.2, 7.2, 1},     // 恰好 1 USDT
		{7.21, 7.2, 1},
		{0, 7.2, 1}, // 单价异常时不拦
		{6, 0, 1},   // 汇率缺失时不拦
	}
	for _, c := range cases {
		if got := MinUsdtCountForGuest(c.unit, c.rate); got != c.want {
			t.Errorf("MinUsdtCountForGuest(%v, %v) = %d, want %d", c.unit, c.rate, got, c.want)
		}
	}
}
