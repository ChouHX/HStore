package store

import (
	"testing"

	"hstore/backend/internal/db"
)

// 兑换码列表要能按商品筛选，界面上「商品管理」多出来的商品下拉依赖这个能力。
func TestListCodesFilterByGoodsID(t *testing.T) {
	EnsureSelfCategory()
	goodsA, e := CreateSelfProduct(SelfProductInput{GoodsID: 940101, Name: "筛选商品A", Price: 0.5, Enabled: true})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _, _ = DeleteSelfProduct(goodsA) })
	goodsB, e := CreateSelfProduct(SelfProductInput{GoodsID: 940102, Name: "筛选商品B", Price: 0.6, Enabled: true})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _, _ = DeleteSelfProduct(goodsB) })

	idA, idB := goodsA, goodsB
	CreateCodes(CreateCodesInput{FaceValue: 1, GoodsID: &idA, GoodsCount: 1, Count: 3})
	CreateCodes(CreateCodesInput{FaceValue: 1, GoodsID: &idB, GoodsCount: 1, Count: 2})

	if got := CountCodes(CodeFilter{GoodsID: goodsA}); got != 3 {
		t.Fatalf("按商品A筛选应为 3 条，实际 %d", got)
	}
	if got := CountCodes(CodeFilter{GoodsID: goodsB}); got != 2 {
		t.Fatalf("按商品B筛选应为 2 条，实际 %d", got)
	}

	items := ListCodes(CodeFilter{GoodsID: goodsA}, 50, 0)
	if len(items) != 3 {
		t.Fatalf("商品A应有 3 条，实际 %d", len(items))
	}
	for _, c := range items {
		if c.GoodsID == nil || *c.GoodsID != goodsA {
			t.Fatalf("筛选结果混入了其他商品的兑换码: %+v", c.Code)
		}
	}

	// 不传筛选时两者都在。
	if got := CountCodes(CodeFilter{}); got < 5 {
		t.Fatalf("不带筛选应至少 5 条，实际 %d", got)
	}
}

// 商品筛选要能和状态筛选叠加。
func TestListCodesFilterByGoodsIDAndStatus(t *testing.T) {
	EnsureSelfCategory()
	goods, e := CreateSelfProduct(SelfProductInput{GoodsID: 940103, Name: "筛选叠加商品", Price: 0.5, Enabled: true})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _, _ = DeleteSelfProduct(goods) })

	gid := goods
	CreateCodes(CreateCodesInput{FaceValue: 1, GoodsID: &gid, GoodsCount: 1, Count: 4})

	if _, e := db.Must().Exec(
		`UPDATE redeem_codes SET status='voided'
		 WHERE id = (SELECT id FROM redeem_codes WHERE goods_id=? LIMIT 1)`, gid); e != nil {
		t.Fatal(e)
	}

	if got := CountCodes(CodeFilter{GoodsID: gid, Status: "unused"}); got != 3 {
		t.Fatalf("商品+未使用筛选应为 3 条，实际 %d", got)
	}
	if got := CountCodes(CodeFilter{GoodsID: gid, Status: "voided"}); got != 1 {
		t.Fatalf("商品+已作废筛选应为 1 条，实际 %d", got)
	}
}
