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
		processShortageNotifications(ctx)
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

func processShortageNotifications(ctx context.Context) {
	store.EnsureShortageNotifications()
	item, err := store.ClaimShortageNotification()
	if err != nil || item == nil {
		if err != nil {
			log.Printf("[hstore] 缺货通知队列读取失败: %v", err)
		}
		return
	}
	to := store.NotifyEmails()
	cfg := store.GetSMTPConfig()
	if len(to) == 0 {
		store.CompleteShortageNotification(item.OrderNumber, fmt.Errorf("未配置管理员通知邮箱"))
		return
	}
	if !cfg.Ready() {
		store.CompleteShortageNotification(item.OrderNumber, fmt.Errorf("未配置可用发信服务"))
		return
	}
	subject := fmt.Sprintf("[HShop] 商品缺货待处理：%s（订单 %s）", item.GoodsName, item.OrderNumber)
	body := fmt.Sprintf("订单号：%s\n商品：%s\n数量：%d\n用户实付金额：¥%.4f\n供货方退回金额：¥%.4f\n用户联系方式：%s\n缺货原因：%s\n下单时间：%s\n\n请登录后台订单管理，联系用户处理退款。",
		item.OrderNumber, item.GoodsName, item.Count, item.SaleMoney, item.RetMoney, item.Contact,
		item.Result, time.UnixMilli(item.CreatedAt).Format("2006-01-02 15:04:05"))
	var sendErr error
	for _, addr := range to {
		if err := mail.Send(ctx, cfg, mail.SendOptions{To: addr, Subject: subject, Text: body}); err != nil {
			sendErr = err
			log.Printf("[hstore] 缺货通知发送到 %s 失败: %v", addr, err)
		}
	}
	store.CompleteShortageNotification(item.OrderNumber, sendErr)
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
