package store

import (
	"strconv"
	"strings"

	"hstore/backend/internal/db"
)

// SKSyncIntervalMinutes 是「上游商品同步间隔」在 sync_meta 里的键（单位：分钟）。
const SKSyncIntervalMinutes = "sync_interval_minutes"

// 同步间隔的取值范围：太短会把上游打爆（WAF 限流），太长价格/库存又会过期。
const (
	SyncIntervalMinMinutes = 5
	SyncIntervalMaxMinutes = 24 * 60
	// SyncIntervalDefaultMinutes 默认半小时一轮
	SyncIntervalDefaultMinutes = 30
)

// GetSyncIntervalMinutes 返回当前配置的同步间隔（分钟）。
// 未配置或非法时回退默认值，保证后台改坏配置也不会停摆。
func GetSyncIntervalMinutes() int {
	raw := strings.TrimSpace(db.GetMeta(SKSyncIntervalMinutes))
	if raw == "" {
		return SyncIntervalDefaultMinutes
	}
	n, e := strconv.Atoi(raw)
	if e != nil || n < SyncIntervalMinMinutes || n > SyncIntervalMaxMinutes {
		return SyncIntervalDefaultMinutes
	}
	return n
}

// SaveSyncIntervalMinutes 保存同步间隔，自动夹到合法区间。
func SaveSyncIntervalMinutes(minutes int) int {
	if minutes < SyncIntervalMinMinutes {
		minutes = SyncIntervalMinMinutes
	}
	if minutes > SyncIntervalMaxMinutes {
		minutes = SyncIntervalMaxMinutes
	}
	_ = db.SetMeta(SKSyncIntervalMinutes, strconv.Itoa(minutes))
	return minutes
}
