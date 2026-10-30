package store

import (
	"strings"

	"hstore/backend/internal/db"
	"hstore/backend/internal/model"
)

// NormalizeContact 规整联系方式：去空格、邮箱转小写。
// 查询时两边都用同一套规则，用户填 Big@QQ.com 也能命中 big@qq.com。
func NormalizeContact(v string) string {
	v = strings.TrimSpace(v)
	if strings.Contains(v, "@") {
		v = strings.ToLower(v)
	}
	return v
}

const publicOrderCols = `o.order_number, o.goods_name, o.count,
	CASE WHEN COALESCE(o.sale_money,0) > 0 THEN o.sale_money ELSE o.money END,
	o.status, COALESCE(o.contact,''), o.created_at,
	(SELECT COUNT(*) FROM cards c WHERE c.order_number = o.order_number)`

func scanPublicOrder(sc interface{ Scan(...any) error }) (model.PublicOrderItem, error) {
	var it model.PublicOrderItem
	it.Kind = "order"
	err := sc.Scan(&it.OrderNumber, &it.GoodsName, &it.Count, &it.Money,
		&it.Status, &it.Contact, &it.CreatedAt, &it.CardCount)
	it.StatusText = OrderStatusText(it.Status)
	return it, err
}

// OrderStatusText 把订单状态码转成给人看的文案。
func OrderStatusText(status int) string {
	switch status {
	case 0:
		return "待处理"
	case 1:
		return "处理中"
	case 2:
		return "部分完成"
	case 3:
		return "已完成"
	case 4:
		return "已退款"
	case 5:
		return "已取消"
	case 7:
		return "待补单"
	case 8:
		return "异常"
	default:
		return "处理中"
	}
}

// QueryPublicOrders 按订单号或联系方式查订单摘要。
// 联系方式精确匹配（邮箱忽略大小写），订单号精确匹配；游客凭证也认。
func QueryPublicOrders(keyword string, limit int) []model.PublicOrderItem {
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		return []model.PublicOrderItem{}
	}
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	contact := NormalizeContact(keyword)

	rows, err := db.Must().Query(`SELECT `+publicOrderCols+`
		FROM orders o
		WHERE o.order_number = ? OR lower(COALESCE(o.contact,'')) = ?
		ORDER BY o.created_at DESC LIMIT ?`, keyword, contact, limit)
	out := []model.PublicOrderItem{}
	if err == nil {
		for rows.Next() {
			if it, e := scanPublicOrder(rows); e == nil {
				out = append(out, it)
			}
		}
		rows.Close()
	}

	// 已发货的游客单会在 orders 里出现一次，这里按上游订单号去重。
	seen := map[string]bool{}
	for _, it := range out {
		seen[it.OrderNumber] = true
	}
	for _, g := range queryGuestPurchases(keyword, contact, limit) {
		if g.OrderNumber != "" && seen[g.OrderNumber] {
			continue
		}
		out = append(out, g)
	}
	return out
}

const guestOrderCols = `token, goods_name, goods_count, amount_cny, status,
	COALESCE(contact,''), created_at, COALESCE(upstream_order_no,''), COALESCE(last_error,'')`

func scanGuestOrder(sc interface{ Scan(...any) error }) (model.PublicOrderItem, error) {
	var (
		it       model.PublicOrderItem
		status   string
		orderNo  string
		lastErr  string
		created  int64
		count    int
		money    float64
		goods    string
		contactV string
		token    string
	)
	if e := sc.Scan(&token, &goods, &count, &money, &status, &contactV, &created, &orderNo, &lastErr); e != nil {
		return it, e
	}
	it.Kind = "guest"
	it.Token = token
	it.GoodsName = goods
	it.Count = count
	it.Money = money
	it.Contact = contactV
	it.CreatedAt = created
	it.OrderNumber = orderNo
	it.StatusText = guestStatusText(status)
	it.Status = guestStatusToOrderStatus(status)
	it.Error = lastErr
	return it, nil
}

// queryGuestPurchases 查还没进入 orders 表的游客单（等待支付 / 发货失败等）。
func queryGuestPurchases(keyword, contact string, limit int) []model.PublicOrderItem {
	rows, err := db.Must().Query(`SELECT `+guestOrderCols+`
		FROM guest_purchases
		WHERE token = ? OR lower(COALESCE(contact,'')) = ?
		ORDER BY created_at DESC LIMIT ?`, keyword, contact, limit)
	if err != nil {
		return nil
	}
	defer rows.Close()
	out := []model.PublicOrderItem{}
	for rows.Next() {
		if it, e := scanGuestOrder(rows); e == nil {
			out = append(out, it)
		}
	}
	return out
}

// QueryGuestByTokens 按游客凭证批量取订单摘要，供「本机记录」回填使用。
// 凭证 token 是随机串，拿到即代表拥有该订单（与 /g/{token} 的权限模型一致）。
func QueryGuestByTokens(tokens []string) []model.PublicOrderItem {
	clean := make([]any, 0, len(tokens))
	for _, t := range tokens {
		t = strings.TrimSpace(t)
		if t != "" && len(clean) < 50 {
			clean = append(clean, t)
		}
	}
	if len(clean) == 0 {
		return []model.PublicOrderItem{}
	}
	holders := strings.TrimSuffix(strings.Repeat("?,", len(clean)), ",")
	rows, err := db.Must().Query(`SELECT `+guestOrderCols+`
		FROM guest_purchases WHERE token IN (`+holders+`)
		ORDER BY created_at DESC`, clean...)
	if err != nil {
		return []model.PublicOrderItem{}
	}
	defer rows.Close()
	out := []model.PublicOrderItem{}
	for rows.Next() {
		if it, e := scanGuestOrder(rows); e == nil {
			out = append(out, it)
		}
	}
	return out
}

func guestStatusText(status string) string {
	switch status {
	case "waiting":
		return "等待支付"
	case "paid":
		return "已支付，待发货"
	case "fulfilling":
		return "正在发货"
	case "fulfilled":
		return "已完成"
	case "failed":
		return "发货失败"
	case "expired":
		return "已过期"
	case "closed":
		return "已关闭"
	case "cancelled":
		return "已取消"
	case "disputed":
		return "支付争议"
	default:
		return status
	}
}

func guestStatusToOrderStatus(status string) int {
	switch status {
	case "fulfilled":
		return 3
	case "failed", "disputed":
		return 8
	case "expired", "closed", "cancelled":
		return 5
	default:
		return 1
	}
}

// QueryOrdersByNumbers 批量按订单号取摘要，供「本机记录」回填使用。
// 只返回摘要，不含卡密；看卡密仍需订单号 + 联系方式。
func QueryOrdersByNumbers(numbers []string) []model.PublicOrderItem {
	clean := make([]any, 0, len(numbers))
	for _, n := range numbers {
		n = strings.TrimSpace(n)
		if n != "" && len(clean) < 50 {
			clean = append(clean, n)
		}
	}
	if len(clean) == 0 {
		return []model.PublicOrderItem{}
	}
	holders := strings.TrimSuffix(strings.Repeat("?,", len(clean)), ",")
	rows, err := db.Must().Query(`SELECT `+publicOrderCols+`
		FROM orders o WHERE o.order_number IN (`+holders+`)
		ORDER BY o.created_at DESC`, clean...)
	if err != nil {
		return []model.PublicOrderItem{}
	}
	defer rows.Close()
	out := []model.PublicOrderItem{}
	for rows.Next() {
		if it, e := scanPublicOrder(rows); e == nil {
			out = append(out, it)
		}
	}
	return out
}
