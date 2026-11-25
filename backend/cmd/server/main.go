// Command server 启动 HStore 的 Go 后端服务。
//
// 环境变量：
//
//	PORT         监听端口，默认 3001
//	FAKA_DB_PATH SQLite 文件路径，默认 ./data/faka.db
//	FAKA_ADDR    监听地址，默认 127.0.0.1
//
// 取件邮件服务参数全部内置（见 internal/service/mailbox.go），无需配置；
// 只有接入非 Outlook 租户时才需要 IMAP_SERVER / IMAP_PORT / MAIL_CLIENT_ID / MAIL_TOKEN_URL。
package main

import (
	"context"
	"errors"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"hstore/backend/internal/db"
	"hstore/backend/internal/httpapi"
	"hstore/backend/internal/imagecache"
	"hstore/backend/internal/service"
	"hstore/backend/internal/store"
	"hstore/backend/internal/upstream"
	"hstore/backend/web"
)

func main() {
	if _, err := db.Open(); err != nil {
		log.Fatalf("[hstore] 打开数据库失败: %v", err)
	}
	// 提前初始化会话/收银台 token 密钥，避免首次在 SQLite 事务内回查配置。
	_ = db.SessionSecret()
	if err := imagecache.Init(filepath.Join(filepath.Dir(db.DBPath()), "images")); err != nil {
		log.Printf("[hstore] 图片缓存目录初始化失败: %v", err)
	}
	// 启动时把家宽节点 token 同步给 sockspool（它定期热加载该文件）
	if err := store.WritePoolTokens(); err != nil {
		log.Printf("[hstore] 家宽节点 token 文件写入失败: %v", err)
	}

	log.Printf("[hstore] 数据库: %s", db.DBPath())

	addr := getenv("FAKA_ADDR", "127.0.0.1")
	port := getenv("PORT", "3001")

	srv := &http.Server{
		Addr:              addr + ":" + port,
		Handler:           withStatic(httpapi.New().Handler()),
		ReadHeaderTimeout: 15 * time.Second,
		ReadTimeout:       120 * time.Second,
		WriteTimeout:      180 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go cryptoLoop(ctx)
	go alipayLoop(ctx)
	go stockLoop(ctx)
	go service.RunOrderSync(ctx)
	go service.RunShortageNotifications(ctx)

	go func() {
		log.Printf("[hstore] 监听 http://%s", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("[hstore] 服务异常: %v", err)
		}
	}()

	<-ctx.Done()
	log.Printf("[hstore] 收到退出信号，正在关闭…")
	shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutCtx)
}

// cryptoLoop 每 30 秒轮询一次链上订单，自动结算/过期。
func cryptoLoop(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			func() {
				defer func() {
					if r := recover(); r != nil {
						log.Printf("[hstore] 加密轮询 panic: %v", r)
					}
				}()
				for _, o := range pendingCryptoOrders() {
					service.CheckCryptoOrder(o)
				}
			}()
		}
	}
}

// alipayLoop 每 5 秒检查一次是否到查账时间，自动拉取支付宝账单并对账。
func alipayLoop(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			func() {
				defer func() {
					if r := recover(); r != nil {
						log.Printf("[hstore] 支付宝查账 panic: %v", r)
					}
				}()
				service.ExpireAlipayOrders()
				service.FulfillPendingGuestPurchases(20)
				if !service.AlipayScanDue() {
					return
				}
				result, err := service.ScanAlipay(ctx)
				if err != nil {
					log.Printf("[hstore] 支付宝查账失败: %s", shortLog(err.Error()))
					return
				}
				if result.Status == "ok" && (result.Entries > 0 || result.Settled > 0) {
					log.Printf("[hstore] 支付宝查账: 流水 %d 条，自动确认 %d 笔", result.Entries, result.Settled)
				}
			}()
		}
	}
}

// stockLoop 后台定时同步上游商品的库存与成本价。
//
// 库存不再跟着「用户打开首页」刷新：那会让每个访客都替全站商品回源一次上游，
// 首屏还得等它。改成后台按配置的间隔（默认 30 分钟，后台可调）取一批最久
// 没同步的在售上游商品，4 并发 + 80ms 间隔慢慢刷完，前端列表直接读本地
// stock_count；用户点「购买」时才用 fresh=1 单独回源拿实时库存。
//
// 成本价必须和库存一起同步：只更库存会导致上游涨价后售价还是旧的，
// 出现负毛利订单（2026-11-25 的 -0.09 就是这么来的）。
func stockLoop(ctx context.Context) {
	// 启动后先等一会儿，避免和首屏流量、其它启动任务抢上游配额。
	select {
	case <-ctx.Done():
		return
	case <-time.After(20 * time.Second):
	}
	ticker := time.NewTicker(stockTickMin)
	defer ticker.Stop()
	var lastRun time.Time
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		// 每 tick 重新读配置：后台改完间隔一个 tick 内就生效
		interval := time.Duration(store.GetSyncIntervalMinutes()) * time.Minute
		if !lastRun.IsZero() && time.Since(lastRun) < interval {
			continue
		}
		lastRun = time.Now()
		func() {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("[hstore] 库存同步 panic: %v", r)
				}
			}()
			syncStaleStock()
		}()
	}
}

// 同步节奏
//
// 每轮把所有「超过一个间隔没同步」的商品刷一遍，因此：
//   - 单个商品的实际刷新周期 ≈ 配置的间隔（默认 30 分钟）
//   - 一轮里要刷完全部在售商品，所以 stockBatch 取 200 覆盖当前约 230 个
//     中的绝大多数，剩下的下一轮补上
//
// 间隔可在后台「系统设置」调整（store.GetSyncIntervalMinutes），
// 运行时每轮重新读取，改完无需重启。
const (
	// stockBatch 是单轮最多刷新的商品数。
	// 上游限流要求串行拉取（约 600ms/个），一轮 80 个 ≈ 50 秒，
	// 剩下的一分钟后补上，不会让单轮拖太久。
	stockBatch = 80
	// stockTickMin 是 ticker 的最小粒度；实际是否开跑由「距上次同步是否
	// 超过配置间隔」决定，这样改配置能在一个 tick 内生效
	stockTickMin = time.Minute
)

// syncStaleStock 刷新一批陈旧商品的库存，返回本轮实际更新的商品数。
func syncStaleStock() int {
	if !upstream.IsConfigured() {
		return 0
	}
	// 陈旧阈值 = 配置的同步间隔：超过一个周期没同步的商品才需要刷新
	ageMS := int64(store.GetSyncIntervalMinutes()) * 60 * 1000
	ids := store.StaleEnabledStockIDs(ageMS, stockBatch)
	if len(ids) == 0 {
		return 0
	}
	snaps := upstream.FetchSnapshots(ids, stockBatch)
	priceChanged := 0
	for id, snap := range snaps {
		// 库存必更；成本价也一起更——只更库存会导致上游涨价后售价还是旧的，
		// 卖一单亏一单（2026-11-25 出现过 -0.09 的订单）。
		if snap.HasCost {
			if store.UpdateCostPrice(id, snap.Cost, snap.Stock) {
				priceChanged++
			}
		} else {
			store.UpdateStock(id, snap.Stock)
		}
	}
	// 有成本价变化就重算全站售价，让加价模板重新生效
	if priceChanged > 0 {
		n := store.RecomputeAllPrices()
		upstream.SyncSelfFallbackPrices()
		log.Printf("[hstore] 成本价变更 %d 个，已重算 %d 个商品售价", priceChanged, n)
	}
	stocks := snaps
	if len(stocks) > 0 {
		log.Printf("[hstore] 库存同步: %d/%d 个商品已更新", len(stocks), len(ids))
	} else {
		// 全失败通常意味着 App-Id / 上游不可用，留痕方便排查。
		log.Printf("[hstore] 库存同步失败: %d 个商品全部未取到库存", len(ids))
	}
	return len(stocks)
}

// pendingCryptoOrders 返回仍在等待中的链上订单 ID。
//
// 同时带上最近 24 小时内过期、但仍未支付的订单：轮询中断（例如查单接口
// 持续报错）期间用户可能已经真实付款，恢复后应该能补结算，而不是让用户
// 的钱白白卡在过期单里。补结算只认「有效期内完成」的链上记录，见 CheckCryptoOrder。
func pendingCryptoOrders() []int {
	rows, err := db.Must().Query(
		`SELECT id FROM crypto_orders
		 WHERE status = 'waiting'
		    OR (status = 'expired' AND expires_at > ?)
		 ORDER BY id DESC LIMIT 50`, time.Now().Add(-24*time.Hour).UnixMilli())
	if err != nil {
		return nil
	}
	defer rows.Close()
	var ids []int
	for rows.Next() {
		var id int
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	return ids
}

// withStatic 托管前端构建产物（go:embed），并为 SPA 提供 history 回退。
// /api/* 一律交给后端；其余路径命中文件就返回文件，否则回退 index.html。
func withStatic(api http.Handler) http.Handler {
	dist := web.Dist()
	if dist == nil {
		log.Printf("[hstore] 未内嵌前端产物，仅提供 API")
		return api
	}
	files := http.FileServer(http.FS(dist))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 前端与 API 同源（生产内嵌、开发走 Vite 代理），不需要 CORS；
		// 仅对预检请求直接放行，避免落到 API 路由的 405。
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/") {
			api.ServeHTTP(w, r)
			return
		}
		name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if name == "" || name == "." {
			name = "index.html"
		}
		if info, err := fs.Stat(dist, name); err == nil && !info.IsDir() {
			if strings.HasPrefix(name, "assets/") {
				// 文件名带内容 hash，可长期缓存
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			} else if strings.HasSuffix(name, ".svg") || strings.HasSuffix(name, ".png") || strings.HasSuffix(name, ".ico") {
				// favicon / 支付图标等静态小图缓存一周，避免反复回源
				w.Header().Set("Cache-Control", "public, max-age=604800")
			}
			files.ServeHTTP(w, r)
			return
		}
		index, err := fs.ReadFile(dist, "index.html")
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		_, _ = w.Write(index)
	})
}

// shortLog 限制日志长度，避免异常响应体把 Docker json-file 日志和磁盘 IO 放大。
func shortLog(value string) string {
	if len(value) <= 300 {
		return value
	}
	return value[:300] + "…"
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
