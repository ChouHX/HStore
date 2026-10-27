package store

import (
	"database/sql"
	"errors"

	"hstore/backend/internal/db"
)

// 单条详情上限，防止异常大的 HTML 把库撑爆（上游正文一般几 KB）。
const maxDetailJSON = 512 << 10

// SaveProductDetail 缓存上游商品详情的原始 JSON。
//
// 存原始字节而不是解析后的结构体：上游字段会变，结构体一旦漏声明就会丢数据
// （早期 goodsDescribe / goodsDetail / rechargeTemplates 就是这么丢的）。
func SaveProductDetail(goodsID int, raw []byte) {
	if goodsID <= 0 || len(raw) == 0 {
		return
	}
	if len(raw) > maxDetailJSON {
		raw = raw[:maxDetailJSON]
	}
	_, _ = db.Must().Exec(
		"UPDATE products SET detail_json = ?, detail_synced_at = ? WHERE goods_id = ?",
		string(raw), now(), goodsID)
}

// GetProductDetail 读取详情缓存。ok=false 表示没有缓存（或商品不存在）。
func GetProductDetail(goodsID int) (raw []byte, syncedAt int64, ok bool) {
	var text string
	var ts sql.NullInt64
	e := db.Must().QueryRow(
		"SELECT detail_json, detail_synced_at FROM products WHERE goods_id = ?", goodsID).
		Scan(&text, &ts)
	if e != nil || text == "" {
		return nil, 0, false
	}
	return []byte(text), ts.Int64, true
}

// StaleDetailIDs 返回详情缓存缺失或超过 maxAge 毫秒的商品 ID（最多 limit 个）。
func StaleDetailIDs(ids []int, maxAgeMs int64, limit int) []int {
	if len(ids) == 0 {
		return nil
	}
	if limit <= 0 {
		limit = 50
	}
	cutoff := now() - maxAgeMs
	out := make([]int, 0, limit)
	for _, id := range ids {
		if len(out) >= limit {
			break
		}
		var ts sql.NullInt64
		var length int
		e := db.Must().QueryRow(
			"SELECT detail_synced_at, length(detail_json) FROM products WHERE goods_id = ?", id).
			Scan(&ts, &length)
		if e != nil {
			continue
		}
		if length == 0 || ts.Int64 < cutoff {
			out = append(out, id)
		}
	}
	return out
}

// ErrNoDetailCache 表示该商品还没有详情缓存。
var ErrNoDetailCache = errors.New("no cached detail")
