package service

import (
	"testing"

	"hstore/backend/internal/db"
	"hstore/backend/internal/store"
)

// TestRedeemOrderBooksSaleMoney 兑换码取件也要记销售额。
//
// 兑换码是先在别的平台卖出去的，站内核销时不扣余额，但金额必须按
// 「数量 × 售价」记账，否则后台的销售额/毛利永远是 0（成本照记还会变负数）。
func TestRedeemOrderBooksSaleMoney(t *testing.T) {
	const goodsID = 900201
	seedSelfProduct(t, goodsID, 20, 0)
	// seedSelfProduct 里售价是 0.09，这里改成确定的 0.064 便于断言。
	if _, e := db.Must().Exec("UPDATE products SET final_price=0.064 WHERE goods_id=?", goodsID); e != nil {
		t.Fatal(e)
	}

	user, e := store.CreateGuestUser()
	if e != nil {
		t.Fatalf("创建用户失败: %v", e)
	}
	balanceBefore := store.GetUser(user.UID).Balance

	res := placeSelfOrder(PlaceOrderOptions{
		User: user, GoodsID: goodsID, Count: 10, Source: "user", Payment: "code",
	}, store.GetProduct(goodsID), 10, "user", "code")
	if !res.OK || res.Order == nil {
		t.Fatalf("兑换码取件应成功，实际: %+v", res)
	}

	const want = 0.64 // 10 × 0.064
	if res.Order.SaleMoney != want || res.Order.Money != want {
		t.Fatalf("兑换码订单金额应为 %v，实际 sale=%v money=%v",
			want, res.Order.SaleMoney, res.Order.Money)
	}
	// 兑换码取件不扣余额，也不计入用户消费（total_spent 只统计真实付款）。
	if after := store.GetUser(user.UID).Balance; after != balanceBefore {
		t.Fatalf("兑换码取件不应扣余额: %v -> %v", balanceBefore, after)
	}
	if spent := store.GetUser(user.UID).TotalSpent; spent != 0 {
		t.Fatalf("兑换码取件不应累计消费: %v", spent)
	}
}
