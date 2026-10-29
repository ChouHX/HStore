package service

import (
	"errors"
	"fmt"
	"log"
	"math"
	"strings"
	"time"

	"hstore/backend/internal/db"
	"hstore/backend/internal/model"
	"hstore/backend/internal/store"
	"hstore/backend/internal/upstream"
)

// RefreshResult 是地址刷新结果。
type RefreshResult struct {
	Updated int      `json:"updated"`
	Failed  int      `json:"failed"`
	Errors  []string `json:"errors"`
}

// RefreshPoolAddresses 刷新地址池的 TRX/BSC 地址。
func RefreshPoolAddresses() RefreshResult {
	res := RefreshResult{Errors: []string{}}
	if !upstream.IsBybitConfigured() {
		res.Errors = append(res.Errors, "未配置 Bybit API")
		return res
	}
	d := db.Must()
	for _, slot := range store.ListPool() {
		if slot.Status == "reserved" {
			continue
		}
		trx, evm := slot.AddressTrx, slot.AddressEvm
		changed := false
		for _, c := range store.CryptoChains {
			addr, e := upstream.SubMemberAddress(slot.SubUID, "USDT", c.Code)
			if e != nil {
				res.Failed++
				res.Errors = append(res.Errors, slot.SubUID+"/"+c.Code+": "+e.Error())
				continue
			}
			if addr == "" {
				res.Failed++
				res.Errors = append(res.Errors, slot.SubUID+"/"+c.Code+": 未返回地址")
				continue
			}
			if c.EVM {
				// 所有 EVM 链共用同一地址；首次拿到就记下，后续链应一致
				evm = addr
			} else {
				trx = addr
			}
			changed = true
		}
		if changed {
			main := trx
			if trx == "" {
				main = evm
			}
			_, _ = d.Exec(`UPDATE bybit_pool SET address_trx=?, address_bsc=?, address_evm=?, address=?, updated_at=? WHERE id=?`,
				trx, evm, evm, main, time.Now().UnixMilli(), slot.ID)
			res.Updated++
		}
	}
	return res
}

// CreateCryptoOrder 用户按 CNY 充值，自选链路。
func CreateCryptoOrder(uid int, cnyInput float64, chainInput string) (*model.CryptoOrder, error) {
	return createCryptoOrder(uid, cnyInput, chainInput, true)
}

// CreateGuestCryptoOrder 为游客直接购买商品创建 USDT 支付订单，不受充值最小金额限制。
func CreateGuestCryptoOrder(uid int, cnyInput float64, chainInput string) (*model.CryptoOrder, error) {
	return createCryptoOrder(uid, cnyInput, chainInput, false)
}

func createCryptoOrder(uid int, cnyInput float64, chainInput string, enforceMin bool) (*model.CryptoOrder, error) {
	cfg := store.GetCryptoConfig()
	if !cfg.Enabled {
		return nil, errors.New("USDT 充值功能未开启")
	}
	if !upstream.IsBybitConfigured() {
		return nil, errors.New("管理员尚未配置 Bybit API")
	}
	chain := cfg.Chain
	if chainInput != "" {
		for _, c := range store.CryptoChains {
			if c.Code == chainInput {
				chain = chainInput
			}
		}
	}
	if cnyInput <= 0 || math.IsNaN(cnyInput) {
		return nil, errors.New("请输入充值金额")
	}
	// 汇率必须为正：为 0 会让下面的除法得到 +Inf，订单金额直接写坏。
	if cfg.Rate <= 0 {
		return nil, errors.New("USDT 汇率未配置，请联系管理员")
	}
	requested := cnyInput / cfg.Rate
	if enforceMin && requested < cfg.MinUsdt {
		return nil, fmt.Errorf("最小充值 %s USDT（约 ¥%.2f）", store.TrimFloat(cfg.MinUsdt), cfg.MinUsdt*cfg.Rate)
	}
	if !enforceMin && requested < 0.005 {
		return nil, errors.New("商品金额过低，暂不支持 USDT 支付")
	}
	if requested > 100000 {
		return nil, errors.New("单笔充值金额过大")
	}

	store.ExpireCryptoOrders()

	// 地址池为空时先刷新一次
	col := store.AddressColumn(chain)
	var ready int
	_ = db.Must().QueryRow("SELECT COUNT(*) FROM bybit_pool WHERE status='available' AND " + col + " <> ''").Scan(&ready)
	if ready == 0 {
		RefreshPoolAddresses()
	}

	base := math.Round(requested*100) / 100
	if !enforceMin {
		// 游客直购必须保证到账金额不少于商品售价，向上取整到分。
		base = math.Ceil(requested*100) / 100
		if base < 0.01 {
			base = 0.01
		}
	}

	// 选地址 + 定金额 + 插单 必须原子完成，否则并发时多个人会拿到同一个
	// 空闲地址，「一人一址」会退化成共用。
	// 金额策略：空闲地址用原始金额；地址被占才在该地址内做金额去重。
	expires := time.Now().Add(30 * time.Minute).UnixMilli()
	id, e := store.CreateCryptoOrderAtomic(store.NewCryptoOrderInput{
		UID:       uid,
		Chain:     chain,
		CNY:       math.Round(cnyInput*100) / 100,
		Base:      base,
		Rate:      cfg.Rate,
		Requested: fmt.Sprintf("%.2f", base),
		Expires:   expires,
	})
	if e != nil {
		return nil, e
	}
	return store.GetCryptoOrder(id), nil
}

// CheckCryptoOrder 查询链上到账并结算。
func CheckCryptoOrder(id int) *model.CryptoOrder {
	store.ExpireCryptoOrders()
	order := store.GetCryptoOrder(id)
	if order == nil {
		return nil
	}
	if order.Status == "paid" {
		_ = FulfillGuestPurchaseByPayment("usdt", order.ID)
		return order
	}
	if order.Status != "waiting" && order.Status != "expired" {
		return order
	}
	if !upstream.IsBybitConfigured() {
		return order
	}
	rows, e := upstream.SubMemberDeposits(order.SubUID, "USDT", order.CreatedAt-10*60*1000)
	if e != nil {
		// 查单失败必须留下痕迹：过去这里静默 return，导致链上已到账
		// 却因为代理不可用而永远无法结算，且日志里毫无线索。
		log.Printf("[hstore] 查询 Bybit 充值记录失败 (订单 %d / 子账户 %s): %s",
			order.ID, order.SubUID, e.Error())
		return order
	}
	for _, r := range rows {
		if r.Status != 3 {
			continue
		}
		if !strings.EqualFold(r.Chain, order.Chain) {
			continue
		}
		if !strings.EqualFold(r.ToAddress, order.Address) {
			continue
		}
		if math.Abs(parseFloatSafe(r.Amount)-parseFloatSafe(order.UsdtAmount)) > 1e-8 {
			continue
		}
		successAt := parseFloatSafe(r.SuccessAt)
		if successAt > 0 && successAt < float64(order.CreatedAt-120000) {
			continue
		}
		// 已过期订单只补「在有效期内完成付款」的单：地址在过期后会被回收给
		// 新订单复用，若放宽到任意时间，晚到账可能被算到别人的订单上。
		if order.Status == "expired" && (successAt <= 0 || successAt > float64(order.ExpiresAt)) {
			continue
		}
		if store.TxidUsed(r.TxID, order.ID) {
			continue
		}
		settled := store.SettleCryptoOrder(order, r.TxID, r.Amount, "USDT 充值到账（"+order.ChainLabel+"）")
		if settled != nil && settled.Status == "paid" {
			_ = FulfillGuestPurchaseByPayment("usdt", settled.ID)
		}
		return settled
	}
	return order
}

// CancelCryptoOrderFor 用户取消订单。
func CancelCryptoOrderFor(id int, uid int, isAdmin bool) (*model.CryptoOrder, error) {
	order := store.GetCryptoOrder(id)
	if order == nil {
		return nil, errors.New("订单不存在")
	}
	if !isAdmin && order.UID != uid {
		return nil, errors.New("无权操作")
	}
	if order.Status != "waiting" {
		return order, nil
	}
	store.CancelCryptoOrder(id)
	return store.GetCryptoOrder(id), nil
}

// SimulateCryptoOrder 管理员测试：模拟到账。
func SimulateCryptoOrder(id int) *model.CryptoOrder {
	order := store.GetCryptoOrder(id)
	if order == nil || order.Status != "waiting" {
		return order
	}
	txid := "SIM-" + store.TrimFloat(float64(id)) + "-" + store.TrimFloat(float64(time.Now().UnixMilli()))
	return store.SettleCryptoOrder(order, txid, order.UsdtAmount, "模拟到账（测试）")
}

func parseFloatSafe(s string) float64 {
	var f float64
	var neg bool
	i := 0
	if i < len(s) && (s[i] == '-' || s[i] == '+') {
		neg = s[i] == '-'
		i++
	}
	for ; i < len(s); i++ {
		c := s[i]
		if c == '.' {
			frac := 0.1
			i++
			for ; i < len(s); i++ {
				c = s[i]
				if c < '0' || c > '9' {
					break
				}
				f += float64(c-'0') * frac
				frac /= 10
			}
			break
		}
		if c < '0' || c > '9' {
			break
		}
		f = f*10 + float64(c-'0')
	}
	if neg {
		return -f
	}
	return f
}
