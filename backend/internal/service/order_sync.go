package service

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"hstore/backend/internal/mail"
	"hstore/backend/internal/model"
	"hstore/backend/internal/store"
	"hstore/backend/internal/upstream"
)

// RunOrderSync keeps accepted purchases moving even after the browser is closed.
// Only reads existing remote orders; it never places or retries a purchase.
func RunOrderSync(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			return
		}
		if upstream.IsConfigured() {
			syncPendingOrders(ctx)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// Mail runs independently so a slow provider never blocks delivery checks.
func RunShortageNotifications(ctx context.Context) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	backfilled := false
	for {
		if ctx.Err() != nil {
			return
		}
		if !backfilled {
			if err := store.EnsureShortageNotifications(); err != nil {
				log.Printf("[hstore] 缺货通知补录失败: %v", err)
			} else {
				backfilled = true
			}
		}
		processShortageNotifications(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func processShortageNotifications(ctx context.Context) {
	item, err := store.ClaimShortageNotification()
	if err != nil || item == nil {
		if err != nil {
			log.Printf("[hstore] 缺货通知队列读取失败: %v", err)
		}
		return
	}
	sendErr := sendShortageNotification(ctx, item, store.NotifyEmails(), store.GetSMTPConfig(), mail.Send)
	if err := store.CompleteShortageNotification(item.OrderNumber, sendErr); err != nil {
		log.Printf("[hstore] 缺货通知结果保存失败 %s: %v", item.OrderNumber, err)
	} else if sendErr != nil {
		log.Printf("[hstore] 缺货通知待重试 %s: %v", item.OrderNumber, sendErr)
	} else {
		log.Printf("[hstore] 缺货通知已发送 %s", item.OrderNumber)
	}
}

func sendShortageNotification(ctx context.Context, item *store.ShortageNotification, to []string, cfg store.SMTPConfig, send func(context.Context, store.SMTPConfig, mail.SendOptions) error) error {
	if len(to) == 0 {
		return fmt.Errorf("未配置管理员通知邮箱")
	}
	if !cfg.Ready() {
		return fmt.Errorf("未配置可用发信服务")
	}
	subject := fmt.Sprintf("[HShop] 商品缺货待处理：%s（订单 %s）", item.GoodsName, item.OrderNumber)
	body := fmt.Sprintf("订单号：%s\n商品：%s\n数量：%d\n用户实付金额：¥%.4f\n供货方退回金额：¥%.4f\n用户联系方式：%s\n缺货原因：%s\n下单时间：%s\n\n请登录后台订单管理，联系用户处理退款。",
		item.OrderNumber, item.GoodsName, item.Count, item.SaleMoney, item.RetMoney, item.Contact,
		item.Result, time.UnixMilli(item.CreatedAt).Format("2006-01-02 15:04:05"))
	if store.OrderBalanceRefunded(item.OrderNumber) {
		body += "\n注意：该订单已自动退回账户余额，请核对账本，避免重复退款。"
	}
	sent := map[string]bool{}
	for _, addr := range item.SentRecipients {
		sent[addr] = true
	}
	var sendErr error
	for _, addr := range to {
		addr = strings.ToLower(strings.TrimSpace(addr))
		if addr == "" || sent[addr] {
			continue
		}
		mailCtx, cancel := context.WithTimeout(ctx, 25*time.Second)
		err := send(mailCtx, cfg, mail.SendOptions{To: addr, Subject: subject, Text: body})
		cancel()
		if err != nil {
			sendErr = err
			continue
		}
		if err := store.RecordShortageRecipient(item.OrderNumber, addr); err != nil {
			return err
		}
		sent[addr] = true
	}
	return sendErr
}

func syncPendingOrders(ctx context.Context) {
	numbers, err := store.PendingOrderSyncNumbers(20)
	if err != nil {
		log.Printf("[hstore] 读取待发货订单失败: %v", err)
		return
	}
	// Bound concurrency independently of page views and other background jobs.
	jobs := make(chan string, len(numbers))
	for _, number := range numbers {
		jobs <- number
	}
	close(jobs)
	var wg sync.WaitGroup
	for i := 0; i < 2 && i < len(numbers); i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for number := range jobs {
				if ctx.Err() != nil {
					return
				}
				func() {
					defer func() {
						if r := recover(); r != nil {
							log.Printf("[hstore] 订单同步 panic %s: %v", number, r)
						}
					}()
					if err := syncPendingOrder(ctx, number, upstream.GetOrderDetailContext); err != nil && ctx.Err() == nil {
						log.Printf("[hstore] 订单同步暂未成功 %s: %v", number, err)
					}
				}()
			}
		}()
	}
	wg.Wait()
}

func syncPendingOrder(ctx context.Context, number string, fetch func(context.Context, string) (*model.OrderDetail, error)) error {
	claimed, err := store.ClaimOrderSync(number)
	if err != nil || !claimed {
		return err
	}
	order := store.GetOrder(number)
	if order == nil || order.UpstreamOrderNo == "" || !store.OrderPending(order.Status) {
		return nil
	}
	queryCtx, cancel := context.WithTimeout(ctx, 40*time.Second)
	defer cancel()
	detail, err := fetch(queryCtx, order.UpstreamOrderNo)
	if err != nil {
		return err
	}
	if detail == nil || detail.OrderNumber != order.UpstreamOrderNo {
		return fmt.Errorf("remote response does not match order")
	}
	if err := store.ApplyOrderSync(number, *detail); err != nil {
		return err
	}
	if detail.Status != order.Status {
		log.Printf("[hstore] 订单发货状态 %s: %d -> %d", number, order.Status, detail.Status)
	}
	return nil
}
