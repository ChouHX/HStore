package upstream

import (
	"database/sql"
	"encoding/json"
	"log"
	"strconv"
	"strings"
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

// UpstreamSnapshot 是一个商品从上游取到的最新状态。
type UpstreamSnapshot struct {
	Stock int     // 可用库存
	Cost  float64 // 上游售价，即我们的成本价
	// HasCost 为 false 表示上游没给价格（或为 0），此时不应覆盖本地成本价
	HasCost bool
}

// FetchSnapshots 批量拉取商品的库存与成本价。
//
// 与 FetchStockCounts 走同一个详情接口，但把 salesPrice 也带回来——
// 上游涨价后如果只同步库存不同步成本，售价会停留在旧价，
// 出现「卖一单亏一单」（线上 2026-11-25 就出过一笔 -0.09 的订单）。
func FetchSnapshots(ids []int, max int) map[int]UpstreamSnapshot {
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
	out := make(map[int]UpstreamSnapshot, len(uniq))
	if len(uniq) == 0 {
		return out
	}
	type result struct {
		id   int
		snap UpstreamSnapshot
	}
	var (
		failMu    sync.Mutex
		failCount int
	)
	jobs := make(chan int)
	results := make(chan result, len(uniq))
	// 必须串行：上游限流对并发非常敏感。实测单请求连续 5 次全通，
	// 但 4 并发 + 80ms 整轮全挂、2 并发 + 250ms 仍有 2/3 失败。
	// 半小时一轮时间充裕，串行 230 个 × 600ms ≈ 2.3 分钟完全够用。
	workers := 1
	if len(uniq) < workers {
		workers = len(uniq)
	}
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for id := range jobs {
				detail, err := GetDetail(id)
				if err != nil {
					// 失败重试一次：限流多半是瞬时的
					time.Sleep(500 * time.Millisecond)
					detail, err = GetDetail(id)
				}
				if err == nil {
					snap := UpstreamSnapshot{Stock: detail.StockCount}
					if detail.SalesPrice > 0 {
						snap.Cost = detail.SalesPrice
						snap.HasCost = true
					}
					results <- result{id: id, snap: snap}
				} else {
					// 记录前几个失败原因，便于判断是限流还是接口变更
					failMu.Lock()
					if failCount < 5 {
						log.Printf("[upstream] 拉取商品 %d 失败: %v", id, err)
						failCount++
					}
					failMu.Unlock()
				}
				time.Sleep(600 * time.Millisecond)
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
		out[item.id] = item.snap
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

	// 保留本地已有商品的上下架、手动锁价、自定义图文与详情缓存，不再先 DELETE 后 INSERT；
	// 远端已下线的旧商品在同步完成后按是否含本地配置/业务关联做精准清理或下架处理。
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
					_, _ = d.Exec(`INSERT INTO products(
						goods_id, name, face_value, sales_price, cost_price, final_price, goods_type, warranty_days, status,
						multiple, is_repeat, is_ref_order, is_ref_money, sku_type, img_url, dir_ids,
						platform, root_category_id, updated_at, detail_json, detail_synced_at)
						VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
						ON CONFLICT(goods_id) DO UPDATE SET
						  name=excluded.name, face_value=excluded.face_value, sales_price=excluded.sales_price,
						  cost_price=excluded.cost_price,
						  final_price=CASE
						    -- 自营商品与锁价商品一律保留原有售价，不被上游同步覆盖。
						    WHEN COALESCE(products.fulfillment,'upstream')='self'
						      OR COALESCE(products.price_locked,0)=1
						    THEN products.final_price ELSE excluded.final_price END,
						  goods_type=excluded.goods_type, warranty_days=excluded.warranty_days, status=excluded.status,
						  multiple=excluded.multiple, is_repeat=excluded.is_repeat, is_ref_order=excluded.is_ref_order,
						  is_ref_money=excluded.is_ref_money, sku_type=excluded.sku_type, img_url=excluded.img_url,
						  dir_ids=excluded.dir_ids, platform=excluded.platform,
						  root_category_id=CASE WHEN COALESCE(products.fulfillment,'upstream')='self'
						    THEN products.root_category_id ELSE excluded.root_category_id END,
						  updated_at=excluded.updated_at`,
						p.GoodsID, p.Name, p.FaceValue, p.SalesPrice, p.SalesPrice, p.SalesPrice,
						defInt(p.GoodsType, 1), store.NormalizeWarranty(p), defInt(p.Status, 1),
						defInt(p.Multiple, 1), p.IsRepeat, p.IsRefOrder, p.IsRefMoney, p.SkuType,
						p.ImgURL, string(dirJSON), platform, nullInt(rootID), ts, "", 0)
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
	pruneRemovedUpstreamProducts(d, seen, ts)
	enrichStock(ids)

	store.RecomputeAllPrices()
	// 定价规则重算后再回填自营商品价，保证「绑定兜底 = 跟随上游价」不被覆盖。
	SyncSelfFallbackPrices()
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

// SyncSelfFallbackPrices 已废弃：自营商品与上游完全分离，不再有「自营售价
// 跟随兜底商品」的行为。保留空实现仅为兼容旧调用点，永远返回 0。
func SyncSelfFallbackPrices() int {
	return 0
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

// pruneRemovedUpstreamProducts 处理远端已下线的上游微软/谷歌商品：
//   - 无本地自定义信息、未锁价且无关联兑换码/待处理游客单的旧商品直接清理；
//   - 有本地配置或业务关联的记录保留，并自动标记为下架（enabled=0, status=2），防止继续售卖失效商品。
func pruneRemovedUpstreamProducts(d interface {
	Query(string, ...any) (*sql.Rows, error)
	Exec(string, ...any) (sql.Result, error)
}, seen map[int]bool, ts int64) {
	rows, e := d.Query(`SELECT p.goods_id,
		CASE WHEN COALESCE(p.price_locked,0)=1
		       OR p.manual_price IS NOT NULL
		       OR (p.custom_name IS NOT NULL AND p.custom_name <> '')
		       OR (p.custom_desc IS NOT NULL AND p.custom_desc <> '')
		       OR (p.custom_img IS NOT NULL AND p.custom_img <> '')
		       OR EXISTS(SELECT 1 FROM redeem_codes rc WHERE rc.goods_id = p.goods_id)
		       OR EXISTS(SELECT 1 FROM guest_purchases gp WHERE gp.goods_id = p.goods_id AND gp.status IN ('waiting','fulfilling','failed'))
		     THEN 1 ELSE 0 END AS keep_record
		FROM products p
		WHERE p.platform IN ('microsoft','google') AND COALESCE(p.fulfillment,'upstream') <> 'self'`)
	if e != nil {
		return
	}
	var toDelete, toDisable []int
	for rows.Next() {
		var gid, keep int
		if rows.Scan(&gid, &keep) != nil {
			continue
		}
		if seen[gid] {
			continue
		}
		if keep == 1 {
			toDisable = append(toDisable, gid)
		} else {
			toDelete = append(toDelete, gid)
		}
	}
	_ = rows.Close()

	if len(toDelete) > 0 {
		parts := make([]string, len(toDelete))
		for i, id := range toDelete {
			parts[i] = strconv.Itoa(id)
		}
		_, _ = d.Exec("DELETE FROM products WHERE goods_id IN (" + strings.Join(parts, ",") + ")")
	}
	if len(toDisable) > 0 {
		parts := make([]string, len(toDisable))
		for i, id := range toDisable {
			parts[i] = strconv.Itoa(id)
		}
		_, _ = d.Exec("UPDATE products SET enabled = 0, status = 2, updated_at = ? WHERE goods_id IN ("+strings.Join(parts, ",")+")", ts)
	}
}

func strconvItoa64(v int64) string { return strconv.FormatInt(v, 10) }
