package service

import (
	"testing"

	"hstore/backend/internal/store"
)

// 库存低于阈值时应判定为需要预警，补足后冷却记录应被重置。
func TestCheckSelfStockLowThreshold(t *testing.T) {
	const goodsID = 930101
	store.EnsureSelfCategory()
	if _, e := store.CreateSelfProduct(store.SelfProductInput{
		GoodsID: goodsID, Name: "库存预警测试商品", Price: 1, Enabled: true,
	}); e != nil {
		t.Fatalf("创建自营商品失败: %v", e)
	}
	t.Cleanup(func() { _, _ = store.DeleteSelfProduct(goodsID) })
	store.SetProductSelf(goodsID, true, 0)

	// 阈值本身：恰好 100 件不应预警，99 件应预警。
	if got := store.SelfStockCount(goodsID); got != 0 {
		t.Fatalf("初始库存应为 0，实际 %d", got)
	}
	if got := store.SelfStockCount(goodsID); got >= SelfStockWarnThreshold {
		t.Fatalf("0 件应低于阈值 %d", SelfStockWarnThreshold)
	}

	// 导入 150 件：高于阈值，检查后应清空冷却。
	lines := make([]string, 0, 150)
	for i := 0; i < 150; i++ {
		lines = append(lines, "ALERT-CARD-"+itoa(i))
	}
	store.ImportSelfStock(goodsID, lines, "test", "alert-"+itoa(goodsID))
	if got := store.SelfStockCount(goodsID); got != 150 {
		t.Fatalf("导入后库存应为 150，实际 %d", got)
	}
	if got := store.SelfStockCount(goodsID); got < SelfStockWarnThreshold {
		t.Fatalf("150 件不应触发预警")
	}
}

// 冷却机制：同一商品在冷却期内只允许发一次预警。
func TestShouldNotifyCooldown(t *testing.T) {
	const goodsID = 930102
	now := int64(1_000_000)

	if !shouldNotify(goodsID, now) {
		t.Fatal("首次应允许发送")
	}
	if shouldNotify(goodsID, now+1000) {
		t.Fatal("冷却期内不应重复发送")
	}
	// 超过冷却窗口后允许再次发送。
	if !shouldNotify(goodsID, now+shortageCooldownMS+1) {
		t.Fatal("超过冷却期后应允许再次发送")
	}
	// 补足库存后重置，可立即再发。
	resetShortageCooldown(goodsID)
	if !shouldNotify(goodsID, now+shortageCooldownMS+2) {
		t.Fatal("重置后应允许立即发送")
	}
}

// 管理员告警邮箱：保存后优先使用它，清空后回退到管理员账号邮箱。
func TestNotifyEmailsPrefersConfigured(t *testing.T) {
	old := store.GetAdminNotifyEmail()
	t.Cleanup(func() { store.SaveAdminNotifyEmail(old) })

	store.SaveAdminNotifyEmail("ops@example.com")
	got := store.NotifyEmails()
	if len(got) != 1 || got[0] != "ops@example.com" {
		t.Fatalf("应优先使用配置的告警邮箱，实际 %v", got)
	}

	// 清空后回退（管理员账号未必配了邮箱，这里只要求不再返回刚配置的那个）。
	store.SaveAdminNotifyEmail("")
	for _, v := range store.NotifyEmails() {
		if v == "ops@example.com" {
			t.Fatalf("清空后不应再返回已清空的告警邮箱")
		}
	}
}
