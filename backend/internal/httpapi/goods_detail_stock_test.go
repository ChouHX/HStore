package httpapi

import (
	"encoding/json"
	"testing"

	"hstore/backend/internal/db"
	"hstore/backend/internal/store"
)

// 上游已缺货（stockCount=0）时，点购买（fresh=1）必须如实返回 0，
// 不能被本地 products.stock_count 里的旧值覆盖——否则弹窗显示有货、
// 用户点了才在支付环节失败。
func TestBuildStoreDetailFreshTrustsUpstreamStock(t *testing.T) {
	const gid = 960101
	// 本地残留一个「有货」的旧值
	if _, e := db.Must().Exec(`INSERT INTO products(goods_id, name, stock_count, platform, updated_at)
		VALUES(?,?,?,?,?) ON CONFLICT(goods_id) DO UPDATE SET stock_count=excluded.stock_count`,
		gid, "库存口径测试", 88, "microsoft", 1); e != nil {
		t.Fatalf("准备商品失败: %v", e)
	}

	raw := json.RawMessage(`{"goodsId":960101,"name":"库存口径测试","stockCount":0,"status":1}`)

	prod := store.GetProduct(gid)

	// 非 fresh：浏览路径用本地值（省上游）
	got, e := buildStoreDetail(gid, prod, raw, false)
	if e != nil {
		t.Fatalf("构建详情失败: %v", e)
	}
	if got.StockCount != 88 {
		t.Fatalf("浏览路径应使用本地库存 88，实际 %d", got.StockCount)
	}

	// fresh：点购买必须以上游为准
	got, e = buildStoreDetail(gid, prod, raw, true)
	if e != nil {
		t.Fatalf("构建详情失败: %v", e)
	}
	if got.StockCount != 0 {
		t.Fatalf("点购买时上游缺货应返回 0，实际 %d（被本地值覆盖了）", got.StockCount)
	}
}

// 上游没给库存（<0）时，即使 fresh 也应退回本地值。
func TestBuildStoreDetailFreshFallsBackWhenUnknown(t *testing.T) {
	const gid = 960102
	if _, e := db.Must().Exec(`INSERT INTO products(goods_id, name, stock_count, platform, updated_at)
		VALUES(?,?,?,?,?) ON CONFLICT(goods_id) DO UPDATE SET stock_count=excluded.stock_count`,
		gid, "库存未知测试", 7, "microsoft", 1); e != nil {
		t.Fatalf("准备商品失败: %v", e)
	}
	raw := json.RawMessage(`{"goodsId":960102,"name":"库存未知测试","stockCount":-1,"status":1}`)
	got, e := buildStoreDetail(gid, store.GetProduct(gid), raw, true)
	if e != nil {
		t.Fatalf("构建详情失败: %v", e)
	}
	if got.StockCount != 7 {
		t.Fatalf("上游库存未知时应退回本地 7，实际 %d", got.StockCount)
	}
}
