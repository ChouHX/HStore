package store

import (
	"testing"

	"hstore/backend/internal/db"
)

// selfCategoryExists 直接查库确认自营分类是否还在。
func selfCategoryExists(t *testing.T) bool {
	t.Helper()
	var n int
	if e := db.Must().QueryRow("SELECT COUNT(*) FROM categories WHERE id = ?", SelfCategoryID).Scan(&n); e != nil {
		t.Fatal(e)
	}
	return n > 0
}

// 上游同步会用 SaveCategories 重写分类表并清理「过期」分类。
// 自营分类（platform='self'，且被自营商品引用）必须留下来，
// 否则商城首页的自营板块会整块消失（线上曾出现过这个问题）。
func TestSaveCategoriesKeepsSelfCategory(t *testing.T) {
	gid, e := CreateSelfProduct(SelfProductInput{
		Name: "同步保护测试商品", Price: 0.5, Enabled: true,
	})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _, _ = DeleteSelfProduct(gid) })

	if !SelfDirs(db.Must())[SelfCategoryID] {
		t.Fatalf("自营分类 %d 未被识别为在用", SelfCategoryID)
	}

	// 模拟一次上游同步：只同步来一个不相干的分类，自营分类不在列表里。
	if e := SaveCategories([]FlatCategory{
		{ID: 777001, Name: "上游测试分类", Platform: "microsoft"},
	}); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _, _ = db.Must().Exec("DELETE FROM categories WHERE id = ?", 777001) })

	if !selfCategoryExists(t) {
		t.Fatalf("上游同步后自营分类被删除，商城自营板块会消失")
	}

	// 商品本身也必须挂在自营分类下，首页才能查到。
	p := GetProduct(gid)
	if p == nil {
		t.Fatalf("自营商品被删除")
	}
	if p.RootCategoryID == nil || *p.RootCategoryID != SelfCategoryID {
		t.Fatalf("自营商品分类被改写: got=%v want=%d", p.RootCategoryID, SelfCategoryID)
	}
}

// 完全没有分类的上游列表（同步异常兜底）也不能清掉自营分类。
func TestSaveCategoriesEmptyListKeepsSelfCategory(t *testing.T) {
	EnsureSelfCategory()
	if e := SaveCategories(nil); e != nil {
		t.Fatal(e)
	}
	if !selfCategoryExists(t) {
		t.Fatalf("空分类列表同步后自营分类丢失")
	}
}
