package service

import (
	"strings"
	"time"

	"hstore/backend/internal/model"
	"hstore/backend/internal/store"
)

// redeemLockMS 上游下单最坏耗时预留窗口，超过则允许重新占用。
const redeemLockMS = int64(3 * 60 * 1000)

// RedeemResult 是核销结果。
type RedeemResult struct {
	OK          bool              `json:"ok"`
	Message     string            `json:"message"`
	Kind        string            `json:"kind"`
	Amount      float64           `json:"amount,omitempty"`
	Balance     float64           `json:"balance,omitempty"`
	Order       *model.LocalOrder `json:"order,omitempty"`
	OrderNumber string            `json:"orderNumber,omitempty"`
}

// Redeem 核销兑换码：面值码充余额，商品码调上游下单取件。
func Redeem(uid int, rawCode string) RedeemResult {
	code := strings.ToUpper(strings.TrimSpace(rawCode))
	if code == "" {
		return RedeemResult{Message: "请输入兑换码", Kind: "balance"}
	}
	if store.RecentFailureCount(uid) >= store.FailLimit() {
		return RedeemResult{Message: "失败次数过多，请 1 分钟后再试", Kind: "balance"}
	}

	row := store.GetCodeByCode(code)
	if row == nil {
		store.RecordFailure(uid)
		return RedeemResult{Message: "兑换码不存在", Kind: "balance"}
	}
	kind := "balance"
	if row.GoodsID != nil {
		kind = "product"
	}
	switch row.Status {
	case "voided":
		store.RecordFailure(uid)
		return RedeemResult{Message: "兑换码已作废", Kind: kind}
	case "used":
		// 兑换码本身就是凭证：再次输入时直接载入原订单，方便老用户回看/取件
		if kind == "product" && row.OrderNumber != "" {
			if order := store.GetOrder(row.OrderNumber); order != nil {
				return RedeemResult{
					OK: true, Kind: "product", Message: "兑换码已使用，已载入原订单",
					Order: order, OrderNumber: order.OrderNumber,
				}
			}
		}
		store.RecordFailure(uid)
		return RedeemResult{Message: "兑换码已被使用", Kind: kind}
	case "redeeming":
		// 占用未超时可提示稍后查看；超时视为可重新占用
		if row.UsedAt != nil && time.Now().UnixMilli()-*row.UsedAt < redeemLockMS {
			return RedeemResult{Message: "兑换码正在处理中，请稍后查看订单", Kind: kind}
		}
	}

	ts := time.Now().UnixMilli()
	if row.StartAt != nil && ts < *row.StartAt {
		return RedeemResult{Message: "兑换活动尚未开始", Kind: kind}
	}
	if row.EndAt != nil && ts > *row.EndAt {
		store.RecordFailure(uid)
		return RedeemResult{Message: "兑换码已过期", Kind: kind}
	}

	if kind == "product" {
		return redeemProduct(uid, row)
	}
	return redeemBalance(uid, row)
}

func redeemBalance(uid int, row *model.RedeemCode) RedeemResult {
	done, after := store.RedeemBalanceCode(row.ID, uid, row.FaceValue)
	if !done {
		store.RecordFailure(uid)
		return RedeemResult{Message: "兑换码已被使用", Kind: "balance"}
	}
	store.ClearFailures(uid)
	return RedeemResult{
		OK: true, Kind: "balance", Message: "兑换成功",
		Amount: row.FaceValue, Balance: after,
	}
}

func redeemProduct(uid int, row *model.RedeemCode) RedeemResult {
	user := store.GetUser(uid)
	if user == nil {
		return RedeemResult{Message: "用户不存在", Kind: "product"}
	}
	if user.Status != "active" {
		return RedeemResult{Message: "账号已被冻结", Kind: "product"}
	}
	if !store.ClaimCode(row.ID, uid, redeemLockMS) {
		store.RecordFailure(uid)
		return RedeemResult{Message: "兑换码正在处理中或已被使用", Kind: "product"}
	}

	r := PlaceOrder(PlaceOrderOptions{
		User:    user,
		GoodsID: *row.GoodsID,
		Count:   row.GoodsCount,
		Source:  "user",
		Payment: "code",
		Remark:  "兑换码取件",
		// 直接把兑换码当作业务订单号：用户手里的凭证就是兑换码，
		// 查询/取件也是输兑换码，不必再记一个内部单号。
		OrderNumber: redeemOrderNo(row.Code),
	})
	if !r.OK {
		store.ReleaseCode(row.ID)
		store.RecordFailure(uid)
		msg := r.Message
		if msg == "" {
			msg = "下单失败，兑换码未扣减"
		}
		return RedeemResult{Message: msg, Kind: "product"}
	}

	store.FinishCode(row.ID, r.Order.OrderNumber)
	store.ClearFailures(uid)

	msg := "兑换成功，已生成订单"
	if len(r.Order.Cards) == 0 {
		msg = "兑换成功，上游正在发货，请稍后取件"
	}
	return RedeemResult{
		OK: true, Kind: "product", Message: msg,
		Order: r.Order, OrderNumber: r.Order.OrderNumber,
	}
}

// PickupResult 是取件结果。
type PickupResult struct {
	OK      bool              `json:"ok"`
	Message string            `json:"message"`
	Order   *model.LocalOrder `json:"order,omitempty"`
}

// Pickup 用兑换码或订单号取回商品内容（卡号/卡密）。
// 判定规则与邮箱取件一致：兑换码是凭证本身、不绑定用户；订单号必须校验归属。
func Pickup(uid int, code, orderNumber string, isAdmin bool) PickupResult {
	order, _, msg, ok := resolveTarget(uid, code, orderNumber, isAdmin)
	if !ok {
		return PickupResult{Message: msg}
	}
	if len(order.Cards) == 0 {
		msg = "上游正在发货，请稍后刷新取件"
		if order.Status == 3 {
			msg = "该订单没有可取的卡券"
		}
		return PickupResult{Message: msg, Order: order}
	}
	return PickupResult{OK: true, Message: "取件成功", Order: order}
}

// redeemOrderNo 返回兑换码核销单要用的业务订单号。
//
// 兑换码本身就是凭证，直接拿它当订单号最省事；但如果码值不符合标准格式
// （人为导入的脏数据、旧库遗留的短码等），仍然回退到系统单号，
// 避免把不合规的字符串写成主键、破坏「单号格式统一」的约定。
func redeemOrderNo(code string) string {
	// 先归一化：用户/导入可能带不带分隔符、大小写混写。
	// NormalizeCode 只在原始字符数为 16 时才补分隔符，其余原样返回，
	// 所以归一化后再判格式才能真正拦住脏数据。
	if normalized := store.NormalizeCode(code); store.IsRedeemOrderNo(normalized) {
		return normalized
	}
	return store.NewSystemOrderNo()
}
