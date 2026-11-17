package store

import (
	"testing"

	"hstore/backend/internal/model"
)

// 已核销的兑换码删除后：兑换码、订单、卡密全部消失，
// 用户不能再通过兑换码或订单号查到这笔记录。
func TestDeleteUsedCodeCascades(t *testing.T) {
	gid := 930301
	EnsureSelfCategory()
	if _, e := CreateSelfProduct(SelfProductInput{
		GoodsID: gid, Name: "删除已用码测试", Price: 1, Enabled: true,
	}); e != nil {
		t.Fatalf("建商品失败: %v", e)
	}
	t.Cleanup(func() {
		_, _ = DeleteSelfProduct(gid)
		DeleteTierDiscounts(gid)
	})
	ImportSelfStock(gid, []string{"CARD-DEL-1", "CARD-DEL-2"}, "test", "del-"+itoa(gid))
	SetProductSelf(gid, true, 0)

	// 造一张兑换码并核销
	batchID, _ := CreateCodesBatch(CreateCodesInput{GoodsID: &gid, GoodsCount: 1, Count: 1})
	t.Cleanup(func() { DeleteBatch(batchID) })
	codes := ListCodes(CodeFilter{BatchID: batchID}, 10, 0)
	if len(codes) != 1 {
		t.Fatalf("应生成 1 张兑换码，实际 %d", len(codes))
	}
	code := codes[0]

	// 模拟核销：占用 → 完成，写入订单与卡密
	orderNo := code.Code // 兑换码核销单直接用码当订单号
	if !ClaimCode(code.ID, 1, 60000) {
		t.Fatal("占用兑换码失败")
	}
	FinishCode(code.ID, orderNo)
	UpsertOrder(UpsertOrderInput{
		OrderNumber: orderNo, GoodsID: gid, GoodsName: "删除已用码测试",
		Count: 1, Money: 1, Status: 3, Source: "user",
	})
	UpsertCards(orderNo, []model.Card{{CardID: "SELF-DEL-1", CardNo: "CARD-DEL-1"}})

	if GetOrder(orderNo) == nil {
		t.Fatal("订单应已写入")
	}

	// 执行删除
	if !DeleteCode(code.ID) {
		t.Fatal("已使用的兑换码应允许删除")
	}

	// 三处都不应再查得到
	if got := GetCodeByCode(code.Code); got != nil {
		t.Errorf("兑换码应已删除，仍能查到 %+v", got)
	}
	if got := CodeByOrder(orderNo); got != nil {
		t.Errorf("按订单号反查兑换码应为空，实际 %+v", got)
	}
	if got := GetOrder(orderNo); got != nil {
		t.Errorf("订单应已级联删除，仍能查到 %s", got.OrderNumber)
	}
	if items := QueryPublicOrders(code.Code, 10); len(items) != 0 {
		t.Errorf("公开查询不应再返回该笔记录，实际 %d 条", len(items))
	}
}

// 批量删除同样覆盖已使用的码。
func TestDeleteCodesIncludesUsed(t *testing.T) {
	gid := 930302
	EnsureSelfCategory()
	if _, e := CreateSelfProduct(SelfProductInput{
		GoodsID: gid, Name: "批量删除测试", Price: 1, Enabled: true,
	}); e != nil {
		t.Fatalf("建商品失败: %v", e)
	}
	t.Cleanup(func() { _, _ = DeleteSelfProduct(gid) })
	ImportSelfStock(gid, []string{"CARD-B-1", "CARD-B-2"}, "test", "bdel-"+itoa(gid))
	SetProductSelf(gid, true, 0)

	batchID, _ := CreateCodesBatch(CreateCodesInput{GoodsID: &gid, GoodsCount: 1, Count: 2})
	codes := ListCodes(CodeFilter{BatchID: batchID}, 10, 0)
	if len(codes) != 2 {
		t.Fatalf("应生成 2 张，实际 %d", len(codes))
	}
	// 核销其中一张
	used := codes[0]
	if !ClaimCode(used.ID, 1, 60000) {
		t.Fatal("占用失败")
	}
	FinishCode(used.ID, used.Code)

	ids := []int{codes[0].ID, codes[1].ID}
	if n := DeleteCodes(ids); n != 2 {
		t.Fatalf("应删除 2 张（含已使用），实际 %d", n)
	}
	if n := CountCodes(CodeFilter{BatchID: batchID}); n != 0 {
		t.Fatalf("删除后应为 0 张，实际 %d", n)
	}
}

// 核销中的码不允许删除，避免留下半截状态。
func TestDeleteReclaimingCodeRejected(t *testing.T) {
	gid := 930303
	EnsureSelfCategory()
	if _, e := CreateSelfProduct(SelfProductInput{
		GoodsID: gid, Name: "核销中删除测试", Price: 1, Enabled: true,
	}); e != nil {
		t.Fatalf("建商品失败: %v", e)
	}
	t.Cleanup(func() { _, _ = DeleteSelfProduct(gid) })
	ImportSelfStock(gid, []string{"CARD-R-1"}, "test", "rdel-"+itoa(gid))
	SetProductSelf(gid, true, 0)

	batchID, _ := CreateCodesBatch(CreateCodesInput{GoodsID: &gid, GoodsCount: 1, Count: 1})
	t.Cleanup(func() { DeleteBatch(batchID) })
	codes := ListCodes(CodeFilter{BatchID: batchID}, 10, 0)
	if len(codes) != 1 {
		t.Fatalf("应生成 1 张，实际 %d", len(codes))
	}
	if !ClaimCode(codes[0].ID, 1, 60000) {
		t.Fatal("占用失败")
	}
	if DeleteCode(codes[0].ID) {
		t.Fatal("核销中的兑换码不应被删除")
	}
}
