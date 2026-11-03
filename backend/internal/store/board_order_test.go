package store

import (
	"testing"

	"hstore/backend/internal/db"
	"hstore/backend/internal/model"
)

// TestBoardOrder 验证后台配置的板块顺序：已配置的排前面且按配置顺序，
// 未配置的保持默认顺序排在后面；重复/非法 ID 会被过滤。
func TestBoardOrder(t *testing.T) {
	if _, err := db.Open(); err != nil {
		t.Fatal(err)
	}
	d := db.Must()
	_, _ = d.Exec("DELETE FROM sync_meta WHERE key = ?", MetaStoreBoardOrder)
	t.Cleanup(func() { _, _ = d.Exec("DELETE FROM sync_meta WHERE key = ?", MetaStoreBoardOrder) })

	if got := BoardOrder(); len(got) != 0 {
		t.Fatalf("初始应为空，实际 %v", got)
	}

	// 重复与非法值应被过滤
	if e := SetBoardOrder([]int{900001, 900001, 0, -3, 527, 30}); e != nil {
		t.Fatal(e)
	}
	got := BoardOrder()
	want := []int{900001, 527, 30}
	if len(got) != len(want) {
		t.Fatalf("期望 %v，实际 %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("期望 %v，实际 %v", want, got)
		}
	}

	// 已配置的按配置顺序，未配置的保持默认（total DESC）
	boards := []model.StoreBoard{
		{ID: 16, Name: "Google", Total: 110},
		{ID: 17, Name: "微软", Total: 37},
		{ID: 527, Name: "韩国", Total: 6},
		{ID: 900001, Name: "自营", Total: 4},
		{ID: 30, Name: "跨境", Total: 10},
	}
	sortBoardsByOrder(boards)
	wantOrder := []int{900001, 527, 30, 16, 17}
	for i, id := range wantOrder {
		if boards[i].ID != id {
			t.Fatalf("第 %d 个板块应为 %d，实际 %d（全量 %v）", i, id, boards[i].ID, boards)
		}
	}

	// 板块选项（定价页）用同一套顺序
	opts := []BoardOption{
		{ID: 16, Name: "Google", Total: 110},
		{ID: 30, Name: "跨境", Total: 10},
		{ID: 900001, Name: "自营", Total: 4},
	}
	SortBoardOptions(opts)
	for i, id := range []int{900001, 30, 16} {
		if opts[i].ID != id {
			t.Fatalf("选项第 %d 个应为 %d，实际 %d", i, id, opts[i].ID)
		}
	}

	// 清空后回到默认顺序
	if e := SetBoardOrder(nil); e != nil {
		t.Fatal(e)
	}
	if got := BoardOrder(); len(got) != 0 {
		t.Fatalf("清空后应为空，实际 %v", got)
	}
}
