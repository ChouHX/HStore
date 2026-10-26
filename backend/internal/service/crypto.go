package service

import (
	"errors"
	"fmt"
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
		trx, bsc := slot.AddressTrx, slot.AddressBsc
		changed := false
		for _, code := range []string{"TRX", "BSC"} {
			addr, e := upstream.SubMemberAddress(slot.SubUID, "USDT", code)
			if e != nil {
				res.Failed++
				res.Errors = append(res.Errors, slot.SubUID+"/"+code+": "+e.Error())
				continue
			}
			if addr == "" {
				res.Failed++
				res.Errors = append(res.Errors, slot.SubUID+"/"+code+": 未返回地址")
				continue
			}
			if code == "TRX" {
				trx = addr
			} else {
				bsc = addr
			}
			changed = true
		}
		if changed {
			chain := slot.Chain
			main := trx
			if trx == "" {
				main = slot.Address
			} else {
				chain = "TRX"
			}
			_, _ = d.Exec("UPDATE bybit_pool SET address_trx=?, address_bsc=?, address=?, chain=?, updated_at=? WHERE id=?",
				trx, bsc, main, chain, time.Now().UnixMilli(), slot.ID)
			res.Updated++
		}
	}
	return res
}

// CreateCryptoOrder 用户按 CNY 充值，自选链路。
func CreateCryptoOrder(uid int, cnyInput float64, chainInput string) (*model.CryptoOrder, error) {
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
	requested := cnyInput / cfg.Rate
	if requested < cfg.MinUsdt {
		return nil, fmt.Errorf("最小充值 %s USDT（约 ¥%.2f）", store.TrimFloat(cfg.MinUsdt), cfg.MinUsdt*cfg.Rate)
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
	expected, ok := store.UniqueExpectedAmount(base)
	if !ok {
		return nil, errors.New("生成唯一支付金额失败，请重试")
	}
	expires := time.Now().Add(30 * time.Minute).UnixMilli()
	slot, ok := store.ReservePoolSlot(chain, expires)
	if !ok {
		return nil, errors.New("充值通道繁忙，请稍后重试")
	}
	id := store.InsertCryptoOrder(uid, slot.ID, slot.SubUID, slot.Address, chain,
		math.Round(cnyInput*100)/100,
		fmt.Sprintf("%.2f", base), expected, cfg.Rate,
		round4(parseFloatSafe(expected)*cfg.Rate), expires)
	if id == 0 {
		return nil, errors.New("创建订单失败，请重试")
	}
	return store.GetCryptoOrder(id), nil
}

// CheckCryptoOrder 查询链上到账并结算。
func CheckCryptoOrder(id int) *model.CryptoOrder {
	store.ExpireCryptoOrders()
	order := store.GetCryptoOrder(id)
	if order == nil || order.Status != "waiting" {
		return order
	}
	if !upstream.IsBybitConfigured() {
		return order
	}
	rows, e := upstream.SubMemberDeposits(order.SubUID, "USDT", order.CreatedAt-10*60*1000)
	if e != nil {
		return order
	}
	for _, r := range rows {
		if r.Status != 3 {
			continue
		}
		if !strings.EqualFold(r.Chain, order.Chain) {
			continue
		}
		if r.ToAddress != order.Address {
			continue
		}
		if math.Abs(parseFloatSafe(r.Amount)-parseFloatSafe(order.UsdtAmount)) > 1e-8 {
			continue
		}
		if r.SuccessAt != "" && parseFloatSafe(r.SuccessAt) > 0 && parseFloatSafe(r.SuccessAt) < float64(order.CreatedAt-120000) {
			continue
		}
		if store.TxidUsed(r.TxID, order.ID) {
			continue
		}
		return store.SettleCryptoOrder(order, r.TxID, r.Amount, "USDT 充值到账（"+order.ChainLabel+"）")
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
