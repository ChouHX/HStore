package upstream

import (
	"encoding/json"
	"log"
	"sync"
	"time"

	"hstore/backend/internal/store"
)

// DetailCacheTTL 是商品详情缓存的「新鲜期」。
//
// 超过这个时间就在后台回源刷新，但当前请求仍然用旧缓存直接返回
// （stale-while-revalidate）：商详是浏览路径，宁可慢一拍也不能让人等 WAF 挑战。
const DetailCacheTTL = 10 * time.Minute

// 同一个商品同时只允许一个后台刷新任务，避免热门商品把上游打爆。
var (
	detailMu         sync.Mutex
	detailRefreshing = map[int]bool{}
)

// DetailCached 返回商品详情 JSON：优先本地缓存，未命中才同步回源。
//
// 缓存由「同步商品 / 刷新库存」等后台流程写入（见 GetDetail），
// 这里的回源只是给「从没同步过的新商品」兜底。
func DetailCached(goodsID int) (json.RawMessage, error) {
	if goodsID <= 0 {
		return nil, &APIError{Code: -1, Message: "缺少商品编号"}
	}
	raw, syncedAt, ok := store.GetProductDetail(goodsID)
	if ok {
		if time.Since(time.UnixMilli(syncedAt)) > DetailCacheTTL {
			refreshDetailAsync(goodsID)
		}
		return json.RawMessage(raw), nil
	}
	fresh, e := GetDetailRaw(goodsID)
	if e != nil {
		return nil, e
	}
	store.SaveProductDetail(goodsID, fresh)
	return fresh, nil
}

// refreshDetailAsync 后台刷新单个商品的详情缓存。
func refreshDetailAsync(goodsID int) {
	detailMu.Lock()
	if detailRefreshing[goodsID] {
		detailMu.Unlock()
		return
	}
	detailRefreshing[goodsID] = true
	detailMu.Unlock()

	go func() {
		defer func() {
			detailMu.Lock()
			delete(detailRefreshing, goodsID)
			detailMu.Unlock()
			if r := recover(); r != nil {
				log.Printf("[hstore] 商品详情缓存刷新 panic: %v", r)
			}
		}()
		// 用 GetDetail 而不是 GetDetailRaw：顺带把库存写回本地。
		if d, e := GetDetail(goodsID); e == nil {
			store.UpdateStock(goodsID, d.StockCount)
		}
	}()
}
