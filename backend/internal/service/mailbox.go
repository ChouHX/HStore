package service

import (
	"errors"
	"os"
	"strconv"
	"strings"
	"time"

	"hstore/backend/internal/mail"
	"hstore/backend/internal/model"
	"hstore/backend/internal/store"
)

// 取件邮件服务参数：与上级 Redeem 项目一致，全部内置，后台无需配置。
// 只有接入非 Outlook 租户时才需要改，用环境变量覆盖即可。
const (
	defTokenURL    = "https://login.microsoftonline.com/consumers/oauth2/v2.0/token"
	defClientID    = "dbc8e03a-b00c-46bd-ae65-b683e7707cb0"
	defIMAPScope   = "https://outlook.office.com/IMAP.AccessAsUser.All offline_access"
	defGraphScope  = "https://graph.microsoft.com/.default"
	defGraphBase   = "https://graph.microsoft.com/v1.0"
	defIMAPServer  = "outlook.office365.com"
	defIMAPPort    = 993
	defInboxFolder = "INBOX"
	defJunkFolder  = "Junk"
)

func envOr(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

// MailProviderConfig 组装取件参数：内置默认值，可用环境变量覆盖。
//
//	IMAP_SERVER / IMAP_PORT          邮件服务器
//	MAIL_CLIENT_ID / MAIL_TOKEN_URL  OAuth 应用（默认内置公共客户端）
func MailProviderConfig() mail.ProviderConfig {
	port := defIMAPPort
	if v := envOr("IMAP_PORT", ""); v != "" {
		if n, e := strconv.Atoi(v); e == nil && n > 0 {
			port = n
		}
	}
	return mail.ProviderConfig{
		TokenURL:        envOr("MAIL_TOKEN_URL", defTokenURL),
		ClientID:        envOr("MAIL_CLIENT_ID", defClientID),
		IMAPOAuthScope:  envOr("MAIL_IMAP_SCOPE", defIMAPScope),
		GraphOAuthScope: envOr("MAIL_GRAPH_SCOPE", defGraphScope),
		GraphAPIBase:    envOr("MAIL_GRAPH_BASE", defGraphBase),
		IMAPServer:      envOr("IMAP_SERVER", defIMAPServer),
		IMAPPort:        port,
		InboxFolderName: defInboxFolder,
		JunkFolderName:  defJunkFolder,
		ConnectTimeout:  15 * time.Second,
		GreetingTimeout: 15 * time.Second,
		SocketTimeout:   60 * time.Second,
	}
}

var errNoCard = errors.New("卡券不存在")

// AccountFromCard 把卡密内容解析成取件凭据。
// 支持 "邮箱----密码" 与 "邮箱----密码----refresh_token" 两种格式。
func AccountFromCard(card model.Card) (mail.Account, error) {
	raw := card.CardNo
	if card.CardPwd != "" {
		raw = raw + "----" + card.CardPwd
	}
	fields := splitCardFields(raw)
	if len(fields) == 0 {
		return mail.Account{}, errors.New("卡密内容为空，无法取件")
	}
	acc := mail.Account{Email: fields[0]}
	if len(fields) > 1 {
		acc.Password = fields[1]
	}
	if len(fields) > 2 {
		acc.RefreshToken = fields[2]
	}
	if acc.RefreshToken != "" {
		acc.Protocol = "graph"
	} else {
		acc.Protocol = "imap"
	}
	if acc.Email == "" {
		return mail.Account{}, errors.New("卡密缺少邮箱账号")
	}
	return acc, nil
}

func splitCardFields(raw string) []string {
	replacer := strings.NewReplacer("\r\n", "\n", "\t", "----", "|", "----")
	normalized := replacer.Replace(raw)
	normalized = strings.ReplaceAll(normalized, "\n", "----")
	parts := strings.Split(normalized, "----")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// MailboxCard 定位可访问的卡券。
// 兑换码本身就是凭证，不绑定用户；订单号必须登录且校验归属。
func MailboxCard(uid int, code, orderNumber, cardID string, isAdmin bool) (model.Card, *model.LocalOrder, error) {
	order, _, msg, ok := resolveTarget(uid, code, orderNumber, isAdmin)
	if !ok {
		return model.Card{}, nil, errors.New(msg)
	}
	if len(order.Cards) == 0 {
		return model.Card{}, order, errors.New("订单暂无卡券，无法取件")
	}
	if cardID == "" {
		return order.Cards[0], order, nil
	}
	for _, c := range order.Cards {
		if c.CardID == cardID {
			return c, order, nil
		}
	}
	return model.Card{}, order, errNoCard
}

// looksLikeCode 判断输入是否像兑换码：16 位字母数字，分隔符可有可无。
func looksLikeCode(v string) bool {
	n := 0
	for _, r := range v {
		switch {
		case r == '-':
		case r >= '0' && r <= '9', r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z':
			n++
		default:
			return false
		}
	}
	return n == 16
}

// resolveTarget 按「兑换码」或「订单号」定位订单。两条路径的信任模型不同：
//
//   - 兑换码：码本身就是凭证。后台生成的兑换码不绑定用户，谁持有谁就能取件
//     （与 Redeem 的卡密模型一致），因此不做任何归属校验，匿名也可用。
//   - 订单号：来自上游、可被枚举，必须登录并校验订单归属。
//
// 输入先按兑换码查；查不到且已登录时再按订单号查一次——上游订单号格式不固定，
// 可能长得和兑换码一样，不该让用户卡在前端的格式猜测上。
func resolveTarget(uid int, code, orderNumber string, isAdmin bool) (*model.LocalOrder, bool, string, bool) {
	code = strings.TrimSpace(code)
	orderNumber = strings.TrimSpace(orderNumber)

	if orderNumber == "" {
		if code == "" {
			return nil, false, "请输入兑换码或订单号", false
		}
		if row := store.GetCodeByCode(code); row != nil {
			if row.OrderNumber == "" {
				if row.Status == "redeeming" {
					return nil, true, "兑换正在处理中，请稍后重试", false
				}
				return nil, true, "该兑换码尚未核销", false
			}
			order := store.GetOrder(row.OrderNumber)
			if order == nil {
				return nil, true, "订单不存在", false
			}
			return order, true, "", true
		}
		// 不是兑换码：匿名时直接当作「码不存在」，避免暴露订单号是否存在
		if looksLikeCode(code) || uid <= 0 {
			return nil, true, "兑换码不存在", false
		}
		orderNumber = code
	}

	if uid <= 0 {
		return nil, false, "未登录只能用兑换码取件，订单号请登录后使用", false
	}
	order := store.GetOrder(orderNumber)
	if order == nil {
		return nil, false, "订单不存在", false
	}
	if !isAdmin && (order.UserUID == nil || *order.UserUID != uid) {
		return nil, false, "无权查看该订单", false
	}
	return order, false, "", true
}

// MailList 拉取邮箱邮件列表（取件核心）。
func MailList(uid int, code, orderNumber, cardID, folder string, page, pageSize int, withBody, isAdmin bool) (*mail.ListResult, model.Card, error) {
	card, _, e := MailboxCard(uid, code, orderNumber, cardID, isAdmin)
	if e != nil {
		return nil, card, e
	}
	acc, e := AccountFromCard(card)
	if e != nil {
		return nil, card, e
	}
	cfg := MailProviderConfig()
	res, e := mail.List(cfg, acc, folder, page, pageSize, withBody)
	if e != nil {
		return nil, card, e
	}
	return &res, card, nil
}

// MailDetail 拉取单封邮件详情。
func MailDetail(uid int, code, orderNumber, cardID, folder, messageID string, isAdmin bool) (*mail.DetailResult, model.Card, error) {
	card, _, e := MailboxCard(uid, code, orderNumber, cardID, isAdmin)
	if e != nil {
		return nil, card, e
	}
	acc, e := AccountFromCard(card)
	if e != nil {
		return nil, card, e
	}
	cfg := MailProviderConfig()
	res, e := mail.Detail(cfg, acc, folder, messageID)
	if e != nil {
		return nil, card, e
	}
	return &res, card, nil
}
