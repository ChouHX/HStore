package store

import (
	"testing"

	"hstore/backend/internal/db"
	"hstore/backend/internal/model"
)

// 兑换码取件的订单（含自营卡密单）下单链路不写 contact，
// 用户只能靠兑换码回查。这里确保公开查询认兑换码，能定位到订单。
func TestQueryPublicOrdersByRedeemCode(t *testing.T) {
	// 复用 TestMain 打开的测试库（db.Open 是单例）。
	if _, e := db.Open(); e != nil {
		t.Fatal(e)
	}

	id, e := CreateSelfProduct(SelfProductInput{GoodsID: 940201, Name: "兑换码取件商品", Price: 0.5, Enabled: true})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _, _ = DeleteSelfProduct(id) })

	goodsID := id
	CreateCodes(CreateCodesInput{FaceValue: 1, GoodsID: &goodsID, GoodsCount: 1, Count: 1})
	codes := ListCodes(CodeFilter{GoodsID: id}, 10, 0)
	if len(codes) != 1 {
		t.Fatalf("应生成 1 条兑换码，实际 %d", len(codes))
	}
	code := codes[0]

	// 模拟兑换码取件下单：订单没有 contact，卡密来自自营库存。
	const orderNo = "HSTESTCODE0001"
	UpsertOrder(UpsertOrderInput{
		OrderNumber: orderNo, GoodsID: id, GoodsName: "兑换码取件商品",
		Count: 1, Status: 3, Result: "兑换码取件", Platform: "self", Source: "user",
	})
	UpsertCards(orderNo, []model.Card{{CardID: "SELF-940201-1", CardNo: "user@outlook.com----pw"}})
	FinishCode(code.ID, orderNo)

	// 1) 用兑换码查，能查到这笔订单。
	items := QueryPublicOrders(code.Code, 10)
	found := false
	for _, it := range items {
		if it.OrderNumber == orderNo {
			found = true
			if it.CardCount != 1 {
				t.Fatalf("卡密条数应为 1，实际 %d", it.CardCount)
			}
			if it.Contact != "" {
				t.Fatalf("兑换码取件单不应有联系方式，实际 %q", it.Contact)
			}
		}
	}
	if !found {
		t.Fatalf("用兑换码 %s 没查到订单 %s，返回 %d 条", code.Code, orderNo, len(items))
	}

	// 2) 兑换码反查订单号与订单一致，公开详情据此做第二重校验。
	row := GetCodeByCode(code.Code)
	if row == nil || row.OrderNumber != orderNo {
		t.Fatalf("兑换码反查订单号不一致: %+v", row)
	}

	// 3) 不相干的兑换码查不到任何订单，避免误放行。
	if got := QueryPublicOrders("ZZZZ-ZZZZ-ZZZZ", 10); len(got) != 0 {
		t.Fatalf("不存在的兑换码不应查出订单，实际 %d 条", len(got))
	}
}
