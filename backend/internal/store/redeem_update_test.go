package store

import (
	"testing"

	"hstore/backend/internal/db"
)

// 批量修改：按筛选条件（这里是批次）把面值码整批改成商品码，并改有效期。
func TestUpdateCodesByFilterToGoods(t *testing.T) {
	EnsureSelfCategory()
	goods, e := CreateSelfProduct(SelfProductInput{GoodsID: 940501, Name: "批量改商品", Price: 0.5, Enabled: true})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _, _ = DeleteSelfProduct(goods) })

	batchID, _ := CreateCodesBatch(CreateCodesInput{FaceValue: 5, Count: 3})
	t.Cleanup(func() { DeleteBatch(batchID) })

	gid, count := goods, 2
	start, end := int64(1_700_000_000_000), int64(1_800_000_000_000)
	n, e := UpdateCodesByFilter(CodeFilter{BatchID: batchID}, UpdateCodesInput{
		GoodsID: &gid, GoodsCount: &count, StartAt: &start, EndAt: &end,
	})
	if e != nil {
		t.Fatalf("批量修改失败: %v", e)
	}
	if n != 3 {
		t.Fatalf("应修改 3 张，实际 %d", n)
	}

	items := ListCodes(CodeFilter{BatchID: batchID}, 10, 0)
	if len(items) != 3 {
		t.Fatalf("应取到 3 张，实际 %d", len(items))
	}
	for _, c := range items {
		if c.GoodsID == nil || *c.GoodsID != goods {
			t.Fatalf("商品关联未修改: %+v", c)
		}
		if c.GoodsCount != 2 {
			t.Fatalf("件数应为 2，实际 %d", c.GoodsCount)
		}
		if c.FaceValue != 0 {
			t.Fatalf("改成商品码后面值应归零，实际 %v", c.FaceValue)
		}
		if c.StartAt == nil || *c.StartAt != start || c.EndAt == nil || *c.EndAt != end {
			t.Fatalf("有效期未修改: %+v", c)
		}
	}

	// 再整批改回面值码，并清空有效期。
	face := 8.5
	if n, e := UpdateCodesByFilter(CodeFilter{BatchID: batchID}, UpdateCodesInput{
		FaceValue: &face, ClearDates: true,
	}); e != nil || n != 3 {
		t.Fatalf("改回面值码失败: n=%d e=%v", n, e)
	}
	for _, c := range ListCodes(CodeFilter{BatchID: batchID}, 10, 0) {
		if c.GoodsID != nil || c.GoodsCount != 0 {
			t.Fatalf("应取消商品关联: %+v", c)
		}
		if c.FaceValue != 8.5 {
			t.Fatalf("面值应为 8.5，实际 %v", c.FaceValue)
		}
		if c.StartAt != nil || c.EndAt != nil {
			t.Fatalf("有效期应被清空: %+v", c)
		}
	}
}

// 已使用 / 核销中的码不能被批量修改，避免破坏订单追溯链。
func TestUpdateCodesByFilterSkipsUsed(t *testing.T) {
	batchID, _ := CreateCodesBatch(CreateCodesInput{FaceValue: 3, Count: 3})
	t.Cleanup(func() { DeleteBatch(batchID) })

	items := ListCodes(CodeFilter{BatchID: batchID}, 10, 0)
	if len(items) != 3 {
		t.Fatalf("应取到 3 张，实际 %d", len(items))
	}
	usedID := items[0].ID
	if !UseBalanceCode(usedID, 1) {
		t.Fatal("核销应成功")
	}
	if _, e := db.Must().Exec("UPDATE redeem_codes SET status='redeeming' WHERE id = ?", items[1].ID); e != nil {
		t.Fatal(e)
	}

	face := 20.0
	n, e := UpdateCodesByFilter(CodeFilter{BatchID: batchID}, UpdateCodesInput{FaceValue: &face})
	if e != nil {
		t.Fatalf("批量修改失败: %v", e)
	}
	if n != 1 {
		t.Fatalf("只应修改 1 张未使用的码，实际 %d", n)
	}
	if c := GetCodeByID(usedID); c == nil || c.FaceValue != 3 {
		t.Fatalf("已使用的码不应被修改: %+v", c)
	}
	if c := GetCodeByID(items[1].ID); c == nil || c.FaceValue != 3 {
		t.Fatalf("核销中的码不应被修改: %+v", c)
	}
	if c := GetCodeByID(items[2].ID); c == nil || c.FaceValue != 20 {
		t.Fatalf("未使用的码应被修改: %+v", c)
	}
}

// 传了 ids 时只在「筛选结果 ∩ 选中项」范围内修改。
func TestUpdateCodesByFilterWithIDs(t *testing.T) {
	batchID, _ := CreateCodesBatch(CreateCodesInput{FaceValue: 1, Count: 3})
	t.Cleanup(func() { DeleteBatch(batchID) })

	items := ListCodes(CodeFilter{BatchID: batchID}, 10, 0)
	if len(items) != 3 {
		t.Fatalf("应取到 3 张，实际 %d", len(items))
	}
	target := []int{items[0].ID}

	// 关键词筛选命中本批（取选中项自己的码值前缀），但只有 1 个 id 在选中项里。
	kw := items[0].Code[:4]
	face := 6.0
	n, e := UpdateCodesByFilter(CodeFilter{Keyword: kw}, UpdateCodesInput{IDs: target, FaceValue: &face})
	if e != nil {
		t.Fatalf("批量修改失败: %v", e)
	}
	if n != 1 {
		t.Fatalf("应只修改选中的 1 张，实际 %d", n)
	}
	if c := GetCodeByID(items[0].ID); c == nil || c.FaceValue != 6 {
		t.Fatalf("选中项应被修改: %+v", c)
	}
	if c := GetCodeByID(items[1].ID); c == nil || c.FaceValue != 1 {
		t.Fatalf("未选中项不应被修改: %+v", c)
	}

	// 筛选条件与选中项无交集时不改任何东西。
	other := 99.0
	if n, e := UpdateCodesByFilter(CodeFilter{Keyword: "ZZZZ-NOT-EXIST"}, UpdateCodesInput{
		IDs: target, FaceValue: &other,
	}); e != nil || n != 0 {
		t.Fatalf("无交集时不应修改: n=%d e=%v", n, e)
	}
}

// 可修改数量只统计未使用 / 已作废的码。
func TestCountUpdatableCodes(t *testing.T) {
	batchID, _ := CreateCodesBatch(CreateCodesInput{FaceValue: 1, Count: 4})
	t.Cleanup(func() { DeleteBatch(batchID) })

	items := ListCodes(CodeFilter{BatchID: batchID}, 10, 0)
	if len(items) != 4 {
		t.Fatalf("应取到 4 张，实际 %d", len(items))
	}
	if !UseBalanceCode(items[0].ID, 1) {
		t.Fatal("核销应成功")
	}
	if n := VoidCodes([]int{items[1].ID}); n != 1 {
		t.Fatalf("应作废 1 张，实际 %d", n)
	}

	f := CodeFilter{BatchID: batchID}
	if got := CountCodes(f); got != 4 {
		t.Fatalf("总数应为 4，实际 %d", got)
	}
	if got := CountUpdatableCodes(f); got != 3 {
		t.Fatalf("可修改应为 3，实际 %d", got)
	}
	if got := CountUpdatableCodes(CodeFilter{BatchID: batchID, Status: "used"}); got != 0 {
		t.Fatalf("按已使用筛选时不应有可修改项，实际 %d", got)
	}
}

// 非法入参应报错而不是静默改库。
func TestUpdateCodesByFilterValidation(t *testing.T) {
	batchID, _ := CreateCodesBatch(CreateCodesInput{FaceValue: 1, Count: 1})
	t.Cleanup(func() { DeleteBatch(batchID) })
	f := CodeFilter{BatchID: batchID}

	gid, face := 940599, 5.0
	start, end := int64(2), int64(1)
	zeroCount := 0

	cases := []struct {
		name string
		in   UpdateCodesInput
	}{
		{"没有任何修改项", UpdateCodesInput{}},
		{"同时改商品和面值", UpdateCodesInput{GoodsID: &gid, FaceValue: &face}},
		{"面值非正", UpdateCodesInput{FaceValue: ptrFloat(-1)}},
		{"件数非正", UpdateCodesInput{GoodsCount: &zeroCount}},
		{"开始晚于结束", UpdateCodesInput{StartAt: &start, EndAt: &end}},
	}
	for _, c := range cases {
		if _, e := UpdateCodesByFilter(f, c.in); e == nil {
			t.Fatalf("%s 应报错", c.name)
		}
	}

	// 校验失败不应改动任何数据。
	if got := CountCodes(f); got != 1 {
		t.Fatalf("总数应为 1，实际 %d", got)
	}
}

func ptrFloat(v float64) *float64 { return &v }
