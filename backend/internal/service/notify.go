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

// SelfStockWarnThreshold 是自营库存预警阈值：可用库存低于该值时提醒管理员补货。
const SelfStockWarnThreshold = 100

// 同一商品在冷却期内只发一封预警邮件，避免批量导入/高频下单把邮箱打爆。
const (
	shortageCooldownMS = 30 * 60 * 1000
)

var (
	shortageMu   sync.Mutex
	shortageLast = map[int]int64{}
)

// shouldNotify 判断某商品此刻是否允许发预警（冷却期内不重复发）。
func shouldNotify(goodsID int, nowMS int64) bool {
	shortageMu.Lock()
	defer shortageMu.Unlock()
	if last, ok := shortageLast[goodsID]; ok && nowMS-last < shortageCooldownMS {
		return false
	}
	shortageLast[goodsID] = nowMS
	return true
}

// resetShortageCooldown 清空冷却记录（库存补足后调用，下次再次告急可以立即提醒）。
func resetShortageCooldown(goodsID int) {
	shortageMu.Lock()
	delete(shortageLast, goodsID)
	shortageMu.Unlock()
}

// notifySelfStockShortage 在自营库存不足以下单数量时立即通知管理员。
// 用于下单被拒的实时场景。
func notifySelfStockShortage(product *model.Product, need, available int) {
	if !shouldNotify(product.GoodsID, time.Now().UnixMilli()) {
		return
	}
	sendStockAlert(product, fmt.Sprintf(
		"有用户尝试兑换 %d 件，当前可用仅 %d 件，订单已被拒绝。", need, available))
}

// CheckSelfStockLow 检查某个自营商品的可用库存是否低于预警阈值，
// 低于则给管理员发邮件提醒补货。库存补足后会清掉冷却记录。
//
// 在库存发生变动的所有路径（导入、下单扣减、删除卡密）后调用。
func CheckSelfStockLow(goodsID int) {
	product := store.GetProduct(goodsID)
	if product == nil || product.Fulfillment != "self" {
		return
	}
	available := store.SelfStockCount(goodsID)
	if available >= SelfStockWarnThreshold {
		resetShortageCooldown(goodsID)
		return
	}
	if !shouldNotify(goodsID, time.Now().UnixMilli()) {
		return
	}
	sendStockAlert(product, fmt.Sprintf(
		"当前可用库存 %d 件，已低于预警阈值 %d 件，请尽快补货。", available, SelfStockWarnThreshold))
}

// sendStockAlert 按商品同步发预警邮件（异步，不阻塞调用方）。
func sendStockAlert(product *model.Product, detail string) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[hstore] 自营库存告警发送 panic: %v", r)
			}
		}()
		to := store.NotifyEmails()
		cfg := store.GetSMTPConfig()
		available := store.SelfStockCount(product.GoodsID)
		if len(to) == 0 || !cfg.Ready() {
			log.Printf("[hstore] 自营库存告警：%s(#%d) 可用 %d（邮件未发送：管理员邮箱或发信配置缺失）",
				product.Name, product.GoodsID, available)
			return
		}
		subject := fmt.Sprintf("[HShop] 自营库存不足：%s（剩余 %d）", product.Name, available)
		body := fmt.Sprintf(
			"商品：%s（#%d）\n当前可用库存：%d 件\n预警阈值：%d 件\n\n%s\n\n时间：%s\n\n请登录后台「自营商品」尽快补充卡密库存。",
			product.Name, product.GoodsID, available, SelfStockWarnThreshold, detail,
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
