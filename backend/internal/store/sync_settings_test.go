package store

import (
	"testing"

	"hstore/backend/internal/db"
)

// 同步间隔：未配置回退默认 30 分钟，非法值同样回退，超范围自动夹紧。
func TestSyncIntervalSetting(t *testing.T) {
	old := GetSyncIntervalMinutes()
	t.Cleanup(func() { SaveSyncIntervalMinutes(old) })

	// 默认值
	_ = db.SetMeta(SKSyncIntervalMinutes, "")
	if got := GetSyncIntervalMinutes(); got != SyncIntervalDefaultMinutes {
		t.Fatalf("未配置时应为默认 %d，实际 %d", SyncIntervalDefaultMinutes, got)
	}

	// 正常保存
	if got := SaveSyncIntervalMinutes(45); got != 45 {
		t.Fatalf("保存 45 应返回 45，实际 %d", got)
	}
	if got := GetSyncIntervalMinutes(); got != 45 {
		t.Fatalf("读取应为 45，实际 %d", got)
	}

	// 低于下限夹紧
	if got := SaveSyncIntervalMinutes(1); got != SyncIntervalMinMinutes {
		t.Fatalf("1 分钟应夹到 %d，实际 %d", SyncIntervalMinMinutes, got)
	}
	// 高于上限夹紧
	if got := SaveSyncIntervalMinutes(99999); got != SyncIntervalMaxMinutes {
		t.Fatalf("超上限应夹到 %d，实际 %d", SyncIntervalMaxMinutes, got)
	}

	// 脏数据回退默认值（不能因为配置被改坏就停摆）
	_ = db.SetMeta(SKSyncIntervalMinutes, "abc")
	if got := GetSyncIntervalMinutes(); got != SyncIntervalDefaultMinutes {
		t.Fatalf("非法值应回退默认，实际 %d", got)
	}
	_ = db.SetMeta(SKSyncIntervalMinutes, "0")
	if got := GetSyncIntervalMinutes(); got != SyncIntervalDefaultMinutes {
		t.Fatalf("0 应回退默认，实际 %d", got)
	}
}
