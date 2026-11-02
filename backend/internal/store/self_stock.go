package store

import (
	"errors"
	"strings"

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
func SetProductSelf(goodsID int, self bool, fallbackGoodsID int) bool {
	fulfillment := "upstream"
	if self {
		fulfillment = "self"
	}
	res, e := db.Must().Exec(
		"UPDATE products SET fulfillment=?, fallback_goods_id=?, stock_count=? WHERE goods_id=?",
		fulfillment, fallbackGoodsID, SelfStockCount(goodsID), goodsID,
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

// ImportSelfStock 批量导入卡密，一行一条；重复内容不会重复入库（按 source/source_id 去重）。
func ImportSelfStock(goodsID int, content []string, source, sourceID string) (int, int) {
	added, skipped := 0, 0
	d := db.Must()
	for i, line := range content {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		sid := ""
		if sourceID != "" {
			sid = sourceID + "-" + itoa(i)
		}
		res, e := d.Exec(
			"INSERT OR IGNORE INTO self_stock(goods_id, content, status, source, source_id, created_at) VALUES(?,?,?,?,?,?)",
			goodsID, line, "available", source, sid, now(),
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
