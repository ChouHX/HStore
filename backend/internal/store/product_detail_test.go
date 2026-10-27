package store

import (
	"strings"
	"testing"
	"time"

	"hstore/backend/internal/db"
)

func TestProductDetailCache(t *testing.T) {
	const gid = 990001
	if _, e := db.Must().Exec(
		`INSERT INTO products(goods_id, name, updated_at) VALUES(?,?,?)
		 ON CONFLICT(goods_id) DO NOTHING`, gid, "缓存测试商品", time.Now().UnixMilli()); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _, _ = db.Must().Exec("DELETE FROM products WHERE goods_id = ?", gid) })

	// 1) 没有缓存时应返回 ok=false
	if _, _, ok := GetProductDetail(gid); ok {
		t.Fatalf("新商品不应有详情缓存")
	}

	// 2) 写入后原样读回（原始 JSON 不做任何改写，保证上游新字段不丢）
	raw := []byte(`{"goodsId":990001,"name":"缓存测试商品","goodsDescribe":"简介","goodsDetail":"<p>正文</p>","rechargeTemplates":[{"title":"账号","required":1}]}`)
	SaveProductDetail(gid, raw)
	got, syncedAt, ok := GetProductDetail(gid)
	if !ok {
		t.Fatalf("缓存写入后读不到")
	}
	if string(got) != string(raw) {
		t.Fatalf("缓存内容被改动:\n got=%s\nwant=%s", got, raw)
	}
	if syncedAt <= 0 {
		t.Fatalf("detail_synced_at 未写入")
	}

	// 3) 刚写入的缓存不该被判为过期
	if ids := StaleDetailIDs([]int{gid}, int64(time.Hour/time.Millisecond), 10); len(ids) != 0 {
		t.Fatalf("新鲜缓存被判过期: %v", ids)
	}
	// 4) 缓存超过 maxAge 应判为过期（sleep 避开毫秒精度边界）
	time.Sleep(5 * time.Millisecond)
	if ids := StaleDetailIDs([]int{gid}, 1, 10); len(ids) != 1 || ids[0] != gid {
		t.Fatalf("超时缓存应判过期, got=%v", ids)
	}

	// 5) 超长详情要截断，避免异常大的 HTML 把库撑爆
	big := strings.Repeat("x", maxDetailJSON+100)
	SaveProductDetail(gid, []byte(big))
	got, _, _ = GetProductDetail(gid)
	if len(got) != maxDetailJSON {
		t.Fatalf("超长详情未截断: %d", len(got))
	}

	// 6) 空内容不覆盖已有缓存
	SaveProductDetail(gid, nil)
	if got, _, ok := GetProductDetail(gid); !ok || len(got) != maxDetailJSON {
		t.Fatalf("空内容不应清空缓存")
	}
}
