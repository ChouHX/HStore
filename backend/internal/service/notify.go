package service

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"hstore/backend/internal/mail"
	"hstore/backend/internal/model"
	"hstore/backend/internal/store"
)

var (
	shortageMu   sync.Mutex
	shortageLast = map[int]int64{}
)

// notifySelfStockShortage 在自营库存不足且没有兜底商品时通知管理员。
// 10 分钟内同一商品只发一封，避免兑换高峰把邮箱打爆。
func notifySelfStockShortage(product *model.Product, need, available int) {
	now := time.Now().UnixMilli()
	shortageMu.Lock()
	if last := shortageLast[product.GoodsID]; now-last < 10*60*1000 {
		shortageMu.Unlock()
		return
	}
	shortageLast[product.GoodsID] = now
	shortageMu.Unlock()

	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[hstore] 自营库存告警发送 panic: %v", r)
			}
		}()
		to := store.AdminEmails()
		cfg := store.GetSMTPConfig()
		if len(to) == 0 || !cfg.Ready() {
			log.Printf("[hstore] 自营库存不足：%s(#%d) 需要 %d，可用 %d（邮件未发送：管理员邮箱或发信配置缺失）",
				product.Name, product.GoodsID, need, available)
			return
		}
		subject := fmt.Sprintf("[HShop] 自营库存不足：%s", product.Name)
		body := fmt.Sprintf("商品：%s（#%d）\n需求数量：%d\n当前可用：%d\n缺少：%d\n时间：%s\n\n请尽快补充自营库存，或为商品配置兜底上游商品 ID。",
			product.Name, product.GoodsID, need, available, need-available,
			time.Now().Format("2006-01-02 15:04:05"))
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		for _, addr := range to {
			if e := mail.Send(ctx, cfg, mail.SendOptions{To: addr, Subject: subject, Text: body}); e != nil {
				log.Printf("[hstore] 自营库存告警发送到 %s 失败: %v", addr, e)
			}
		}
	}()
}
