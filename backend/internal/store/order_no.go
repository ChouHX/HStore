package store

import (
	"crypto/rand"
	"encoding/hex"
	"strings"
	"time"
)

// 单号前缀：所有对外展示的单号共用同一套生成逻辑，只是前缀区分业务。
const (
	// PrefixOrder 是系统业务订单号前缀（orders.order_number）。
	PrefixOrder = "HS"
	// PrefixCrypto 是 USDT 链上充值/购物支付单前缀（crypto_orders.order_no）。
	PrefixCrypto = "US"
	// PrefixAlipay 是支付宝经营码支付单前缀（alipay_orders.order_no）。
	PrefixAlipay = "AL"
)

// NewOrderNo 生成统一格式的业务单号：前缀 + 秒级时间 + 10 位随机十六进制。
//
// 全站共用这一套规则（订单号、支付宝支付单、USDT 支付单），保证：
//   - 长度固定、易读、可按时间排序；
//   - 随机部分 40 bit，并发下不冲突（写库仍有唯一约束兜底）。
//
// 注意：这是**我们自己的**单号，与上游（天启）的订单号完全独立。
// 上游单号只存库、只给管理员看，不对外暴露，避免用户凭单号去上游侧探测。
func NewOrderNo(prefix string) string {
	buf := make([]byte, 5)
	_, _ = rand.Read(buf)
	return prefix + time.Now().Format("20060102150405") + strings.ToUpper(hex.EncodeToString(buf))
}

// NewSystemOrderNo 生成系统业务订单号。
func NewSystemOrderNo() string {
	return NewOrderNo(PrefixOrder)
}
