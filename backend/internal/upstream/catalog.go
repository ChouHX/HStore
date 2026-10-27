package upstream

import (
	"encoding/json"
	"sync"
	"time"

	"hstore/backend/internal/db"
	"hstore/backend/internal/store"
)

// PullRemoteCategories 拉取上游全部分类并落库。
func PullRemoteCategories() (total, roots int, ids []int, err error) {
	dirs, e := GetDirs()
	if e != nil {
		return 0, 0, nil, e
	}
	flat := store.FlattenCategories(dirs)
	if e := store.SaveCategories(flat); e != nil {
		return 0, 0, nil, e
	}
	ids = make([]int, 0, len(flat))
	for _, c := range flat {
		ids = append(ids, c.ID)
	}
	return len(flat), len(dirs), ids, nil
}

// RefreshProducts 同步本地已有商品的上游信息（价格/库存/状态…）。
func RefreshProducts(ids []int) store.RefreshResult {
	result := store.RefreshResult{Total: len(ids), Missing: []int{}}
	if len(ids) == 0 {
		return result
	}
	exists := map[int]bool{}
	for _, id := range ids {
		if store.GetProduct(id) != nil {
			exists[id] = true
		}
	}

	jobs := make(chan int)
	var mu sync.Mutex
	var wg sync.WaitGroup
	workers := 4
	if len(ids) < workers {
		workers = len(ids)
	}
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for id := range jobs {
				if !exists[id] {
					mu.Lock()
					result.Missing = append(result.Missing, id)
					mu.Unlock()
					continue
				}
				d, e := GetDetail(id)
				if e == nil {
					fallback := ""
					if p := store.GetProduct(id); p != nil {
						fallback = p.Name
					}
					store.UpdateProductFromDetail(*d, fallback)
					mu.Lock()
					result.Updated++
					mu.Unlock()
				} else {
					mu.Lock()
					result.Failed++
					mu.Unlock()
				}
				time.Sleep(80 * time.Millisecond)
			}
		}()
	}
	for _, id := range ids {
		jobs <- id
	}
	close(jobs)
	wg.Wait()
	return result
}

// EnrichImportedStock 导入后低频补全库存（最多 30 个）。
func EnrichImportedStock(ids []int) {
	if len(ids) == 0 {
		return
	}
	if len(ids) > 30 {
		ids = ids[:30]
	}
	ch := make(chan int)
	done := make(chan struct{}, 2)
	for i := 0; i < 2; i++ {
		go func() {
			for id := range ch {
				if d, e := GetDetail(id); e == nil {
					store.UpdateStock(id, d.StockCount)
				}
				time.Sleep(150 * time.Millisecond)
				done <- struct{}{}
			}
		}()
	}
	go func() {
		for _, id := range ids {
			ch <- id
		}
		close(ch)
	}()
	for range ids {
		<-done
	}
}

// StockItem 是商城库存刷新入参/出参。
type StockItem struct {
	GoodsID    int `json:"goodsId"`
	StockCount int `json:"stockCount"`
}

// RefreshStockForProducts 商城加载商品后批量刷新库存（60 秒节流）。
func RefreshStockForProducts(items []StockItem, max int) {
	if max <= 0 {
		max = 20
	}
	var ids []int
	for _, it := range items {
		if len(ids) >= max {
			break
		}
		if it.GoodsID > 0 {
			ids = append(ids, it.GoodsID)
		}
	}
	if len(ids) == 0 {
		return
	}
	stale := store.StaleStockIDs(ids)
	if len(stale) == 0 {
		return
	}
	ch := make(chan int)
	done := make(chan struct{}, 4)
	workers := 4
	if len(stale) < workers {
		workers = len(stale)
	}
	for i := 0; i < workers; i++ {
		go func() {
			for id := range ch {
				if d, e := GetDetail(id); e == nil {
					store.UpdateStock(id, d.StockCount)
				}
				time.Sleep(80 * time.Millisecond)
				done <- struct{}{}
			}
		}()
	}
	go func() {
		for _, id := range stale {
			ch <- id
		}
		close(ch)
	}()
	for range stale {
		<-done
	}
}

// FetchStockCounts 并发拉取一组上游商品的详情库存，用于商品同步页展示。
// 失败的商品不会出现在返回值中，由调用方显示为未知。
func FetchStockCounts(ids []int, max int) map[int]int {
	if max <= 0 {
		max = 20
	}
	seen := map[int]bool{}
	uniq := make([]int, 0, len(ids))
	for _, id := range ids {
		if id <= 0 || seen[id] || len(uniq) >= max {
			continue
		}
		seen[id] = true
		uniq = append(uniq, id)
	}
	out := make(map[int]int, len(uniq))
	if len(uniq) == 0 {
		return out
	}
	type result struct {
		id    int
		stock int
	}
	jobs := make(chan int)
	results := make(chan result, len(uniq))
	workers := 4
	if len(uniq) < workers {
		workers = len(uniq)
	}
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for id := range jobs {
				if detail, err := GetDetail(id); err == nil {
					results <- result{id: id, stock: detail.StockCount}
				}
				time.Sleep(80 * time.Millisecond)
			}
		}()
	}
	go func() {
		for _, id := range uniq {
			jobs <- id
		}
		close(jobs)
	}()
	go func() {
		wg.Wait()
		close(results)
	}()
	for item := range results {
		out[item.id] = item.stock
	}
	return out
}

// SyncResult 是全量同步结果。
type SyncResult struct {
	OK         bool   `json:"ok"`
	Source     string `json:"source"`
	Categories int    `json:"categories"`
	Products   int    `json:"products"`
	Microsoft  int    `json:"microsoft"`
	Google     int    `json:"google"`
	Message    string `json:"message"`
}

const maxPages = 60

// SyncFromRemote 拉取分类 + 微软/谷歌商品写入本地。
func SyncFromRemote() (SyncResult, error) {
	ts := time.Now().UnixMilli()
	dirs, e := GetDirs()
	if e != nil {
		return SyncResult{}, e
	}
	flat := store.FlattenCategories(dirs)
	dirRoot := map[int]int{}
	for _, c := range flat {
		dirRoot[c.ID] = c.RootID
	}

	d := db.Must()

	// 同步是「先删后插」，这里把已有详情缓存带过去。
	// 否则刚同步完所有商品的详情缓存都会变空，用户点开商品又要一个个回源。
	type detailSnap struct {
		json string
		at   int64
	}
	cached := map[int]detailSnap{}
	if rows, e := d.Query("SELECT goods_id, detail_json, detail_synced_at FROM products WHERE platform IN ('microsoft','google')"); e == nil {
		for rows.Next() {
			var gid int
			var js string
			var at int64
			if rows.Scan(&gid, &js, &at) == nil && js != "" {
				cached[gid] = detailSnap{json: js, at: at}
			}
		}
		_ = rows.Close()
	}

	if _, e := d.Exec("DELETE FROM products WHERE platform IN ('microsoft','google')"); e != nil {
		return SyncResult{}, e
	}
	if e := store.SaveCategories(flat); e != nil {
		return SyncResult{}, e
	}

	byPlatform := map[string][]int{"microsoft": {}, "google": {}, "other": {}}
	for _, c := range flat {
		if c.Platform != "other" {
			byPlatform[c.Platform] = append(byPlatform[c.Platform], c.ID)
		}
	}

	seen := map[int]bool{}
	count := 0
	for _, platform := range []string{"microsoft", "google"} {
		for _, dirID := range byPlatform[platform] {
			page := 1
			totalPages := 1
			for {
				data, e := GetList(page, strconvItoa(dirID))
				if e != nil {
					if IsEmptyDataError(e) {
						break
					}
					return SyncResult{}, e
				}
				for _, p := range data.Items {
					if seen[p.GoodsID] {
						continue
					}
					seen[p.GoodsID] = true
					dirIDs := p.DirIDs
					if len(dirIDs) == 0 {
						dirIDs = []int{dirID}
					}
					dirJSON, _ := json.Marshal(dirIDs)
					rootID := 0
					if r, ok := dirRoot[dirIDs[0]]; ok {
						rootID = r
					} else if r, ok := dirRoot[dirID]; ok {
						rootID = r
					}
					snap := cached[p.GoodsID]
					_, _ = d.Exec(`INSERT INTO products(
						goods_id, name, face_value, sales_price, cost_price, final_price, goods_type, warranty_days, status,
						multiple, is_repeat, is_ref_order, is_ref_money, sku_type, img_url, dir_ids,
						platform, root_category_id, updated_at, detail_json, detail_synced_at)
						VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
						ON CONFLICT(goods_id) DO UPDATE SET
						  name=excluded.name, face_value=excluded.face_value, sales_price=excluded.sales_price,
						  cost_price=excluded.cost_price, final_price=excluded.final_price,
						  goods_type=excluded.goods_type, warranty_days=excluded.warranty_days, status=excluded.status,
						  multiple=excluded.multiple, is_repeat=excluded.is_repeat, is_ref_order=excluded.is_ref_order,
						  is_ref_money=excluded.is_ref_money, sku_type=excluded.sku_type, img_url=excluded.img_url,
						  dir_ids=excluded.dir_ids, platform=excluded.platform,
						  root_category_id=excluded.root_category_id, updated_at=excluded.updated_at,
						  detail_json=CASE WHEN excluded.detail_json<>'' THEN excluded.detail_json ELSE products.detail_json END,
						  detail_synced_at=CASE WHEN excluded.detail_json<>'' THEN excluded.detail_synced_at ELSE products.detail_synced_at END`,
						p.GoodsID, p.Name, p.FaceValue, p.SalesPrice, p.SalesPrice, p.SalesPrice,
						defInt(p.GoodsType, 1), store.NormalizeWarranty(p), defInt(p.Status, 1),
						defInt(p.Multiple, 1), p.IsRepeat, p.IsRefOrder, p.IsRefMoney, p.SkuType,
						p.ImgURL, string(dirJSON), platform, nullInt(rootID), ts, snap.json, snap.at)
					count++
				}
				totalPages = data.AllPage
				if totalPages < 1 {
					totalPages = 1
				}
				page++
				if page > totalPages || page > maxPages {
					break
				}
			}
		}
	}

	ids := make([]int, 0, len(seen))
	for id := range seen {
		ids = append(ids, id)
	}
	enrichStock(ids)

	store.RecomputeAllPrices()
	_ = db.SetMeta("last_sync", strconvItoa64(ts))
	_ = db.SetMeta("last_source", "remote")

	counts := store.PlatformCounts()
	return SyncResult{
		OK: true, Source: "remote", Categories: len(flat), Products: count,
		Microsoft: counts["microsoft"], Google: counts["google"],
		Message: "已从远端同步 " + strconvItoa(len(flat)) + " 个分类（" + strconvItoa(len(dirs)) +
			" 个板块）、" + strconvItoa(count) + " 个商品（微软 " + strconvItoa(counts["microsoft"]) +
			" / 谷歌 " + strconvItoa(counts["google"]) + "）",
	}, nil
}

func enrichStock(ids []int) {
	if len(ids) == 0 {
		return
	}
	ch := make(chan int)
	done := make(chan struct{}, 3)
	workers := 3
	if len(ids) < workers {
		workers = len(ids)
	}
	for i := 0; i < workers; i++ {
		go func() {
			for id := range ch {
				if d, e := GetDetail(id); e == nil {
					store.UpdateStock(id, d.StockCount)
				}
				time.Sleep(150 * time.Millisecond)
				done <- struct{}{}
			}
		}()
	}
	go func() {
		for _, id := range ids {
			ch <- id
		}
		close(ch)
	}()
	for range ids {
		<-done
	}

	// 库存仍未知的再低频重试一次
	remaining := store.UnknownStockIDs()
	if len(remaining) == 0 {
		return
	}
	for _, id := range remaining {
		if d, e := GetDetail(id); e == nil {
			store.UpdateStock(id, d.StockCount)
		}
		time.Sleep(400 * time.Millisecond)
	}
}

// RunSync 执行同步（未配置上游时返回提示）。
func RunSync() (SyncResult, error) {
	if IsConfigured() {
		return SyncFromRemote()
	}
	return SyncResult{
		OK: false, Source: "demo",
		Message: "未检测到 X-App-Id（客户编号），无法调用远端接口。请在「系统设置」中填写客户编号与 API 密钥。",
	}, nil
}

func defInt(v, def int) int {
	if v == 0 {
		return def
	}
	return v
}

func nullInt(v int) any {
	if v == 0 {
		return nil
	}
	return v
}

func strconvItoa(v int) string { return itoa(v) }

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

func strconvItoa64(v int64) string { return itoa(int(v)) }
