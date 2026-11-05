package store

import (
	"testing"
)

// 生成时应记录批次号，并能按批次查回本批的兑换码。
func TestCreateCodesBatchReturnsCodes(t *testing.T) {
	batchID, codes := CreateCodesBatch(CreateCodesInput{FaceValue: 5, Count: 4})
	t.Cleanup(func() { DeleteBatch(batchID) })

	if batchID <= 0 {
		t.Fatalf("batchID 应大于 0，实际 %d", batchID)
	}
	if len(codes) != 4 {
		t.Fatalf("应生成 4 个码值，实际 %d", len(codes))
	}
	for _, c := range codes {
		if len(c) != 19 {
			t.Fatalf("兑换码格式异常: %q", c)
		}
	}
	if got := CountCodes(CodeFilter{BatchID: batchID}); got != 4 {
		t.Fatalf("按批次查询应为 4 条，实际 %d", got)
	}
}

// 不同批次互不干扰。
func TestCreateCodesBatchDistinct(t *testing.T) {
	b1, _ := CreateCodesBatch(CreateCodesInput{FaceValue: 1, Count: 2})
	b2, _ := CreateCodesBatch(CreateCodesInput{FaceValue: 1, Count: 3})
	t.Cleanup(func() { DeleteBatch(b1); DeleteBatch(b2) })

	if b1 == b2 {
		t.Fatalf("两个批次号不应相同: %d", b1)
	}
	if got := CountCodes(CodeFilter{BatchID: b1}); got != 2 {
		t.Fatalf("批次1应为 2 条，实际 %d", got)
	}
	if got := CountCodes(CodeFilter{BatchID: b2}); got != 3 {
		t.Fatalf("批次2应为 3 条，实际 %d", got)
	}
}

// 批次汇总应统计出各状态数量与面值。
func TestListBatchesSummary(t *testing.T) {
	batchID, _ := CreateCodesBatch(CreateCodesInput{FaceValue: 8, Count: 3})
	t.Cleanup(func() { DeleteBatch(batchID) })

	list := ListBatches(50, 0)
	var found *BatchSummary
	for i := range list {
		if list[i].BatchID == batchID {
			found = &list[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("批次 %d 未出现在汇总列表中", batchID)
	}
	if found.Total != 3 || found.Unused != 3 || found.Used != 0 {
		t.Fatalf("汇总统计异常: %+v", *found)
	}
	if found.FaceValue != 8 {
		t.Fatalf("面值应为 8，实际 %v", found.FaceValue)
	}
	if found.GoodsID != nil {
		t.Fatalf("面值码不应带商品ID")
	}
}

// 批量作废只影响未使用的码。
func TestVoidAndDeleteCodes(t *testing.T) {
	batchID, codes := CreateCodesBatch(CreateCodesInput{FaceValue: 1, Count: 3})
	t.Cleanup(func() { DeleteBatch(batchID) })

	items := ListCodes(CodeFilter{BatchID: batchID}, 50, 0)
	ids := make([]int, 0, len(items))
	for _, c := range items {
		ids = append(ids, c.ID)
	}
	if len(ids) != 3 {
		t.Fatalf("应取到 3 条，实际 %d", len(ids))
	}
	_ = codes

	if n := VoidCodes(ids[:2]); n != 2 {
		t.Fatalf("应作废 2 条，实际 %d", n)
	}
	if n := CountCodes(CodeFilter{BatchID: batchID, Status: "voided"}); n != 2 {
		t.Fatalf("已作废应为 2 条，实际 %d", n)
	}
	// 作废后的码允许删除
	if n := DeleteCodes(ids); n != 3 {
		t.Fatalf("应删除 3 条（含未使用+已作废），实际 %d", n)
	}
	if n := CountCodes(CodeFilter{BatchID: batchID}); n != 0 {
		t.Fatalf("删除后应为 0 条，实际 %d", n)
	}
}

// 整批作废/删除。
func TestVoidAndDeleteBatch(t *testing.T) {
	batchID, _ := CreateCodesBatch(CreateCodesInput{FaceValue: 2, Count: 5})
	t.Cleanup(func() { DeleteBatch(batchID) })

	if n := VoidBatch(batchID); n != 5 {
		t.Fatalf("整批作废应为 5 条，实际 %d", n)
	}
	if n := DeleteBatch(batchID); n != 5 {
		t.Fatalf("整批删除应为 5 条，实际 %d", n)
	}
	if n := CountCodes(CodeFilter{BatchID: batchID}); n != 0 {
		t.Fatalf("删除后应为 0 条，实际 %d", n)
	}
}

// 已使用的码不能被删除或作废，避免破坏追溯链。
func TestUsedCodeProtected(t *testing.T) {
	batchID, _ := CreateCodesBatch(CreateCodesInput{FaceValue: 3, Count: 1})
	t.Cleanup(func() { DeleteBatch(batchID) })

	items := ListCodes(CodeFilter{BatchID: batchID}, 10, 0)
	if len(items) != 1 {
		t.Fatalf("应取到 1 条，实际 %d", len(items))
	}
	id := items[0].ID
	if !UseBalanceCode(id, 1) {
		t.Fatal("核销应成功")
	}
	if n := VoidCodes([]int{id}); n != 0 {
		t.Fatalf("已使用的码不应被作废，实际作废 %d", n)
	}
	if n := DeleteCodes([]int{id}); n != 0 {
		t.Fatalf("已使用的码不应被删除，实际删除 %d", n)
	}
}

// 商品筛选下拉只应列出「兑换码实际关联到」的商品，
// 从未被兑换码引用的商品不应出现。
func TestListCodeGoodsIDsOnlyReferenced(t *testing.T) {
	EnsureSelfCategory()
	used, e := CreateSelfProduct(SelfProductInput{GoodsID: 940201, Name: "有兑换码的商品", Price: 0.5, Enabled: true})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _, _ = DeleteSelfProduct(used) })
	unused, e := CreateSelfProduct(SelfProductInput{GoodsID: 940202, Name: "无兑换码的商品", Price: 0.6, Enabled: true})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _, _ = DeleteSelfProduct(unused) })

	gid := used
	batchID, _ := CreateCodesBatch(CreateCodesInput{GoodsID: &gid, GoodsCount: 1, Count: 2})
	t.Cleanup(func() { DeleteBatch(batchID) })

	ids := ListCodeGoodsIDs()
	hasUsed, hasUnused := false, false
	for _, id := range ids {
		if id == used {
			hasUsed = true
		}
		if id == unused {
			hasUnused = true
		}
	}
	if !hasUsed {
		t.Fatalf("有兑换码的商品 %d 应出现在候选集里: %v", used, ids)
	}
	if hasUnused {
		t.Fatalf("无兑换码的商品 %d 不应出现: %v", unused, ids)
	}

	items := ListProductsByGoodsIDs(ids)
	found := false
	for _, p := range items {
		if p.GoodsID == used {
			found = true
			if p.Name == "" {
				t.Fatal("选项应带商品名称")
			}
		}
	}
	if !found {
		t.Fatalf("候选集里应含商品 %d", used)
	}
}

// 商品被删除但兑换码仍在时，仍要给出可读选项，否则没法按该商品筛选。
func TestListProductsByGoodsIDsDeletedFallback(t *testing.T) {
	items := ListProductsByGoodsIDs([]int{99999999})
	if len(items) != 1 {
		t.Fatalf("应返回 1 个占位选项，实际 %d", len(items))
	}
	if items[0].GoodsID != 99999999 || items[0].Name == "" {
		t.Fatalf("占位选项不正确: %+v", items[0])
	}
}
