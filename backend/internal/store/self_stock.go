package store

import (
	"errors"
	"strconv"
	"strings"
	"time"

	"hstore/backend/internal/db"
	"hstore/backend/internal/model"
)

// ===== 自营库存 =====

// SelfStockCount 返回商品当前可用（未售出）的本地卡密条数。
func SelfStockCount(goodsID int) int {
	var n int
	_ = db.Must().QueryRow(
		"SELECT COUNT(*) FROM self_stock WHERE goods_id=? AND status='available'", goodsID,
	).Scan(&n)
	return n
}

// SetProductSelf 切换商品的发货方式；切到自营时同步刷新库存数。
// SetProductSelf 切换商品的发货方式；切到自营时同步刷新库存数。
//
// 兜底已下线：不再写 fallback_goods_id，保留列中已有的历史值不动
// （历史订单追溯需要）。fallbackGoodsID 参数仅为兼容旧调用方保留。
func SetProductSelf(goodsID int, self bool, fallbackGoodsID int) bool {
	_ = fallbackGoodsID
	fulfillment := "upstream"
	if self {
		fulfillment = "self"
	}
	res, e := db.Must().Exec(
		"UPDATE products SET fulfillment=?, stock_count=? WHERE goods_id=?",
		fulfillment, SelfStockCount(goodsID), goodsID,
	)
	if e != nil {
		return false
	}
	n, _ := res.RowsAffected()
	return n > 0
}

// ReserveSelfStock 原子占用 n 条库存，供下单时锁定，避免并发超卖。
func ReserveSelfStock(goodsID, n int, orderNumber string) ([]model.SelfStock, error) {
	if n <= 0 {
		return nil, nil
	}
	tx, e := db.Must().Begin()
	if e != nil {
		return nil, e
	}
	defer func() { _ = tx.Rollback() }()

	rows, e := tx.Query(
		"SELECT id, content, created_at FROM self_stock WHERE goods_id=? AND status='available' ORDER BY id LIMIT ?",
		goodsID, n,
	)
	if e != nil {
		return nil, e
	}
	var ids []int
	var items []model.SelfStock
	for rows.Next() {
		var it model.SelfStock
		if e := rows.Scan(&it.ID, &it.Content, &it.CreatedAt); e != nil {
			rows.Close()
			return nil, e
		}
		it.GoodsID = goodsID
		it.Status = "reserved"
		it.OrderNumber = orderNumber
		ids = append(ids, it.ID)
		items = append(items, it)
	}
	rows.Close()
	if len(items) < n {
		return nil, errors.New("自营库存不足")
	}
	for _, id := range ids {
		res, e := tx.Exec(
			"UPDATE self_stock SET status='reserved', order_number=? WHERE id=? AND status='available'",
			orderNumber, id,
		)
		if e != nil {
			return nil, e
		}
		if ra, _ := res.RowsAffected(); ra != 1 {
			return nil, errors.New("自营库存被其他订单占用，请重试")
		}
	}
	if e := tx.Commit(); e != nil {
		return nil, e
	}
	return items, nil
}

// ReleaseSelfStock 下单失败时把已占用库存放回可用池。
func ReleaseSelfStock(orderNumber string) {
	_, _ = db.Must().Exec(
		"UPDATE self_stock SET status='available', order_number='' WHERE order_number=? AND status='reserved'",
		orderNumber,
	)
}

// FinishSelfStock 订单落库成功后把占用标记为已售出。
func FinishSelfStock(orderNumber string) {
	_, _ = db.Must().Exec(
		"UPDATE self_stock SET status='sold', sold_at=? WHERE order_number=? AND status='reserved'",
		now(), orderNumber,
	)
}

// ImportSelfStock 批量导入卡密，一行一条。
// 同一批次内重复内容会被跳过；不同批次的重复内容允许入库（补货场景）。
func ImportSelfStock(goodsID int, content []string, source, sourceID string) (int, int) {
	added, skipped := 0, 0
	d := db.Must()
	// source_id 为空时不会命中唯一索引，这里补一个纳秒级批次号，
	// 既保证同批次重复行只在第一条入库，也不影响不同批次导入相同内容。
	if sourceID == "" {
		sourceID = "batch-" + strconv.FormatInt(time.Now().UnixNano(), 10) + "-" + itoa(goodsID)
	}
	seen := make(map[string]bool, len(content))
	for i, line := range content {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if seen[line] {
			skipped++
			continue
		}
		seen[line] = true
		res, e := d.Exec(
			"INSERT OR IGNORE INTO self_stock(goods_id, content, status, source, source_id, created_at) VALUES(?,?,?,?,?,?)",
			goodsID, line, "available", source, sourceID+"-"+itoa(i), now(),
		)
		if e != nil {
			skipped++
			continue
		}
		if n, _ := res.RowsAffected(); n > 0 {
			added++
		} else {
			skipped++
		}
	}
	return added, skipped
}

// ListSelfStock 返回某商品的库存（默认最新 200 条）。
func ListSelfStock(goodsID, limit int) []model.SelfStock {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	rows, e := db.Must().Query(
		"SELECT id, goods_id, content, status, order_number, source, source_id, created_at, sold_at FROM self_stock WHERE goods_id=? ORDER BY id DESC LIMIT ?",
		goodsID, limit,
	)
	if e != nil {
		return nil
	}
	defer rows.Close()
	out := []model.SelfStock{}
	for rows.Next() {
		var it model.SelfStock
		if rows.Scan(&it.ID, &it.GoodsID, &it.Content, &it.Status, &it.OrderNumber,
			&it.Source, &it.SourceID, &it.CreatedAt, &it.SoldAt) == nil {
			out = append(out, it)
		}
	}
	return out
}

// ListSelfStockPaged 分页查询自营卡密，供后台卡密管理使用。
// goodsID 为 0 时跨全部自营商品查询；status 为空表示不限状态。
func ListSelfStockPaged(goodsID int, status, keyword string, page, pageSize int) ([]map[string]any, int) {
	if page < 1 {
		page = 1
	}
	if pageSize <= 0 || pageSize > 200 {
		pageSize = 20
	}
	where := []string{"1=1"}
	args := []any{}
	if goodsID > 0 {
		where = append(where, "s.goods_id = ?")
		args = append(args, goodsID)
	}
	if status != "" && status != "all" {
		where = append(where, "s.status = ?")
		args = append(args, status)
	}
	if kw := strings.TrimSpace(keyword); kw != "" {
		where = append(where, "(s.content LIKE ? OR s.order_number LIKE ?)")
		like := "%" + kw + "%"
		args = append(args, like, like)
	}
	cond := strings.Join(where, " AND ")
	d := db.Must()

	var total int
	if e := d.QueryRow("SELECT COUNT(*) FROM self_stock s WHERE "+cond, args...).Scan(&total); e != nil {
		return []map[string]any{}, 0
	}

	sqlArgs := append(append([]any{}, args...), pageSize, (page-1)*pageSize)
	rows, e := d.Query(`SELECT s.id, s.goods_id, COALESCE(p.name,''), s.content, s.status,
		s.order_number, s.source, s.created_at, s.sold_at
		FROM self_stock s LEFT JOIN products p ON p.goods_id = s.goods_id
		WHERE `+cond+` ORDER BY s.id DESC LIMIT ? OFFSET ?`, sqlArgs...)
	if e != nil {
		return []map[string]any{}, total
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var (
			id, gid, createdAt, soldAt int
			goodsName, content, st     string
			orderNumber, source        string
		)
		if rows.Scan(&id, &gid, &goodsName, &content, &st, &orderNumber, &source,
			&createdAt, &soldAt) != nil {
			continue
		}
		out = append(out, map[string]any{
			"id":          id,
			"goodsId":     gid,
			"goodsName":   goodsName,
			"content":     content,
			"status":      st,
			"orderNumber": orderNumber,
			"source":      source,
			"createdAt":   createdAt,
			"soldAt":      soldAt,
		})
	}
	return out, total
}

// DeleteSelfStock 删除一条未售出的库存。
func DeleteSelfStock(goodsID, id int) bool {
	res, e := db.Must().Exec(
		"DELETE FROM self_stock WHERE id=? AND goods_id=? AND status='available'", id, goodsID,
	)
	if e != nil {
		return false
	}
	n, _ := res.RowsAffected()
	return n > 0
}

// itoa 避免再引入 strconv（本文件只用一个转换）。
func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var b [20]byte
	i := len(b)
	for v > 0 {
		i--
		b[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
