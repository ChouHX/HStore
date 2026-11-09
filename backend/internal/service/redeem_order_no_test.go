package service

import (
	"testing"

	"hstore/backend/internal/db"
	"hstore/backend/internal/store"
)

// 兑换码核销单的订单号必须直接用兑换码，而不是另生成 HS 开头的内部单号。
// 用户手里的凭证就是兑换码，查询/取件也是输兑换码，两个单号只会让人混淆。
func TestRedeemOrderNoIsRedeemCode(t *testing.T) {
	const goodsID = 900301
	seedSelfProduct(t, goodsID, 5, 0)

	gid := goodsID
	store.CreateCodes(store.CreateCodesInput{GoodsID: &gid, GoodsCount: 1, Count: 1})
	codes := store.ListCodes(store.CodeFilter{GoodsID: gid}, 10, 0)
	if len(codes) != 1 {
		t.Fatalf("应生成 1 条兑换码，实际 %d", len(codes))
	}
	code := codes[0]

	user, e := store.CreateGuestUser()
	if e != nil {
		t.Fatalf("创建用户失败: %v", e)
	}

	res := Redeem(user.UID, code.Code)
	if !res.OK {
		t.Fatalf("兑换应成功，实际: %s", res.Message)
	}
	if res.Order == nil {
		t.Fatal("兑换成功应返回订单")
	}
	if res.Order.OrderNumber != code.Code {
		t.Fatalf("订单号应为兑换码 %s，实际 %s", code.Code, res.Order.OrderNumber)
	}
	if res.OrderNumber != code.Code {
		t.Fatalf("返回的 orderNumber 应为兑换码，实际 %s", res.OrderNumber)
	}

	// redeem_codes.order_number 必须与订单号一致，否则兑换码反查不到订单。
	row := store.GetCodeByCode(code.Code)
	if row == nil || row.OrderNumber != code.Code {
		t.Fatalf("兑换码记录的订单号应为 %s，实际 %+v", code.Code, row)
	}
}

// 兑换码格式不合法时回退到系统单号，避免把脏字符串写成主键。
func TestRedeemOrderNoFallbackForInvalidCode(t *testing.T) {
	if got := redeemOrderNo("NOT-A-VALID-CODE"); got == "NOT-A-VALID-CODE" {
		t.Fatalf("非法码值不应直接当订单号，实际 %s", got)
	} else if len(got) == 0 || got[0] != 'H' {
		t.Fatalf("应回退到系统单号（HS 开头），实际 %s", got)
	}

	// 合法码值会先归一化（去掉多余分隔符 / 小写转大写）。
	if got := redeemOrderNo("abcd-efgh-ijkl-mnop"); got != "ABCD-EFGH-IJKL-MNOP" {
		t.Fatalf("合法码值应归一化后使用，实际 %s", got)
	}
	// 不带分隔符也能归一化成标准格式。
	if got := redeemOrderNo("ABCDEFGHIJKLMNOP"); got != "ABCD-EFGH-IJKL-MNOP" {
		t.Fatalf("无分隔符码值应被补齐分隔符，实际 %s", got)
	}
}

// 兑换码当订单号后，用户用兑换码仍能查到订单（公开查询）。
func TestRedeemOrderQueryableByCode(t *testing.T) {
	const goodsID = 900302
	seedSelfProduct(t, goodsID, 5, 0)

	gid := goodsID
	store.CreateCodes(store.CreateCodesInput{GoodsID: &gid, GoodsCount: 1, Count: 1})
	codes := store.ListCodes(store.CodeFilter{GoodsID: gid}, 10, 0)
	if len(codes) != 1 {
		t.Fatalf("应生成 1 条兑换码，实际 %d", len(codes))
	}
	code := codes[0]

	user, e := store.CreateGuestUser()
	if e != nil {
		t.Fatalf("创建用户失败: %v", e)
	}
	res := Redeem(user.UID, code.Code)
	if !res.OK {
		t.Fatalf("兑换应成功: %s", res.Message)
	}

	// 公开查询：直接输兑换码，应命中这笔订单。
	items := store.QueryPublicOrders(code.Code, 10)
	found := false
	for _, it := range items {
		if it.OrderNumber == code.Code {
			found = true
		}
	}
	if !found {
		t.Fatalf("用兑换码 %s 查不到订单（返回 %d 条）", code.Code, len(items))
	}

	// 按订单号取详情也能取到。
	order := store.GetOrder(code.Code)
	if order == nil {
		t.Fatalf("按兑换码 %s 取订单失败", code.Code)
	}
	if len(order.Cards) == 0 {
		t.Fatal("订单应带卡密")
	}
	_ = db.Must()
}
