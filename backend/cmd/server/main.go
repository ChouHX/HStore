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
	"strings"
	"syscall"
	"time"

	"hstore/backend/internal/db"
	"hstore/backend/internal/httpapi"
	"hstore/backend/internal/service"
	"hstore/backend/web"
)

func main() {
	if _, err := db.Open(); err != nil {
		log.Fatalf("[hstore] 打开数据库失败: %v", err)
	}
	// 提前初始化会话/收银台 token 密钥，避免首次在 SQLite 事务内回查配置。
	_ = db.SessionSecret()
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
				if !service.AlipayScanDue() {
					return
				}
				result, err := service.ScanAlipay(ctx)
				if err != nil {
					log.Printf("[hstore] 支付宝查账失败: %v", err)
					return
				}
				if result.Status == "ok" && (result.Entries > 0 || result.Settled > 0) {
					log.Printf("[hstore] 支付宝查账: 流水 %d 条，自动确认 %d 笔", result.Entries, result.Settled)
				}
			}()
		}
	}
}

// pendingCryptoOrders 返回仍在等待中的链上订单 ID。
func pendingCryptoOrders() []int {
	rows, err := db.Must().Query(
		"SELECT id FROM crypto_orders WHERE status = 'waiting' ORDER BY id DESC LIMIT 50")
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

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
