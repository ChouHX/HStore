package upstream

import (
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"hstore/backend/internal/model"
	"hstore/backend/internal/store"
)

const apiVersion = "3.0"

var httpClient = &http.Client{Timeout: 15 * time.Second}

// Config 是上游调用所需的凭据。
type Config struct {
	BaseURL   string
	AppID     string
	AppSecret string
	Cookie    string
}

// GetConfig 读取当前配置。
func GetConfig() Config {
	s := store.GetTianqiSettings()
	return Config{BaseURL: strings.TrimRight(s.BaseURL, "/"), AppID: s.AppID, AppSecret: s.AppSecret, Cookie: store.GetCookie()}
}

// IsConfigured 是否已配置上游。
func IsConfigured() bool {
	c := GetConfig()
	return c.AppID != "" && c.AppSecret != "" && c.BaseURL != ""
}

// APIError 是上游业务错误。
type APIError struct {
	Code    int
	Message string
}

func (e *APIError) Error() string { return e.Message }

// Sign 计算 X-Signature：MD5(appId + appSecret + version + timestamp + bodyJson)。
func Sign(appID, appSecret, timestamp, bodyJSON string) string {
	sum := md5.Sum([]byte(appID + appSecret + apiVersion + timestamp + bodyJSON))
	return hex.EncodeToString(sum[:])
}

type envelope struct {
	Code int             `json:"code"`
	Msg  string          `json:"msg"`
	Data json.RawMessage `json:"data"`
}

func request(path string, body any, out any) error {
	cfg := GetConfig()
	if cfg.AppID == "" || cfg.AppSecret == "" {
		return &APIError{Code: -1, Message: "未配置客户编号 / API 密钥，请在「系统设置」中填写"}
	}
	bodyJSON := "{}"
	if body != nil {
		raw, e := json.Marshal(body)
		if e != nil {
			return e
		}
		bodyJSON = string(raw)
	}

	do := func(cookie string) (*http.Response, string, error) {
		ts := strconv.FormatInt(time.Now().Unix(), 10)
		req, e := http.NewRequest(http.MethodPost, cfg.BaseURL+path, bytes.NewReader([]byte(bodyJSON)))
		if e != nil {
			return nil, "", e
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-App-Id", cfg.AppID)
		req.Header.Set("X-Version", apiVersion)
		req.Header.Set("X-Timestamp", ts)
		req.Header.Set("X-Signature", Sign(cfg.AppID, cfg.AppSecret, ts, bodyJSON))
		if cookie != "" {
			req.Header.Set("Cookie", cookie)
		}
		res, e := httpClient.Do(req)
		if e != nil {
			return nil, "", e
		}
		defer res.Body.Close()
		raw, e := io.ReadAll(io.LimitReader(res.Body, 8<<20))
		if e != nil {
			return nil, "", e
		}
		return res, string(raw), nil
	}

	parse := func(text string) (bool, error) {
		var env envelope
		if e := json.Unmarshal([]byte(text), &env); e != nil {
			return false, nil
		}
		if env.Code != 1000 {
			msg := env.Msg
			if msg == "" {
				msg = "接口调用失败"
			}
			return true, &APIError{Code: env.Code, Message: msg}
		}
		if out != nil && len(env.Data) > 0 && string(env.Data) != "null" {
			if e := json.Unmarshal(env.Data, out); e != nil {
				return true, fmt.Errorf("解析返回数据失败: %w", e)
			}
		}
		return true, nil
	}

	cookie := cfg.Cookie
	if cookie == "" {
		if r := RefreshWafCookieOnce(cfg.BaseURL); r.OK {
			cookie = r.Cookie
			store.SaveCookie(cookie)
		}
	}

	res, text, e := do(cookie)
	if e != nil {
		return &APIError{Code: -1, Message: "网络错误：" + e.Error()}
	}
	parsed, e := parse(text)
	if parsed {
		return e
	}

	// 非 JSON（WAF 人机校验）→ 重新取 Cookie 再试一次
	refreshed := RefreshWafCookieOnce(cfg.BaseURL)
	if refreshed.OK {
		cookie = refreshed.Cookie
		store.SaveCookie(cookie)
		res, text, e = do(cookie)
		if e != nil {
			return &APIError{Code: -1, Message: "网络错误：" + e.Error()}
		}
		if parsed, e = parse(text); parsed {
			return e
		}
	}
	status := 0
	if res != nil {
		status = res.StatusCode
	}
	hint := "失败：" + refreshed.Message
	if refreshed.OK {
		hint = "成功但仍被拦截"
	}
	return &APIError{Code: status, Message: "接口返回非 JSON（可能被站点 WAF 拦截）。自动获取 Cookie " + hint}
}

// IsEmptyDataError 判断「该分类没有商品」这类空数据错误。
func IsEmptyDataError(err error) bool {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return strings.Contains(apiErr.Message, "无商品数据") || strings.Contains(apiErr.Message, "无数据")
	}
	return false
}

// GetDirs 拉取分类树。
func GetDirs() ([]model.RemoteCategory, error) {
	var out []model.RemoteCategory
	if e := request("/api/v3/goods/getDirs", map[string]any{}, &out); e != nil {
		return nil, e
	}
	return out, nil
}

// GoodsListData 是商品列表返回。
type GoodsListData struct {
	AllCount int                   `json:"allCount"`
	AllPage  int                   `json:"allPage"`
	Items    []model.RemoteProduct `json:"items"`
}

// GetList 拉取商品列表。
func GetList(page int, dirID string) (*GoodsListData, error) {
	var out GoodsListData
	body := map[string]any{
		"page": page, "goodsType": "", "keyWord": "", "skuType": "",
		"showDirId": "1", "dirId": dirID, "brandId": "",
	}
	if e := request("/api/v3/goods/getList", body, &out); e != nil {
		return nil, e
	}
	return &out, nil
}

// GetDetail 拉取商品详情。
func GetDetail(goodsID int) (*model.GoodsDetail, error) {
	var out model.GoodsDetail
	if e := request("/api/v3/goods/getDetail", map[string]any{"goodsId": goodsID}, &out); e != nil {
		return nil, e
	}
	if out.GoodsID == 0 {
		out.GoodsID = goodsID
	}
	return &out, nil
}

// GetAccount 查询上游账户余额。
func GetAccount() (float64, error) {
	var out struct {
		Balance float64 `json:"balance"`
	}
	if e := request("/api/v3/user/getAccount", map[string]any{}, &out); e != nil {
		return 0, e
	}
	return out.Balance, nil
}

// CreateOrderParams 是下单参数。
type CreateOrderParams struct {
	GoodsID     int
	Count       int
	OuterNumber string
	SafePrice   float64
	Sku         string
	Attach      []map[string]string
}

// CreateOrder 调用上游下单。
func CreateOrder(p CreateOrderParams) (*model.CreateOrderResult, error) {
	attach := p.Attach
	if attach == nil {
		attach = []map[string]string{}
	}
	body := map[string]any{
		"goodsId": p.GoodsID, "count": p.Count, "notifyUrl": "",
		"outerNumber": p.OuterNumber, "safePrice": p.SafePrice, "sku": p.Sku, "attach": attach,
	}
	var out model.CreateOrderResult
	if e := request("/api/v3/order/create", body, &out); e != nil {
		return nil, e
	}
	return &out, nil
}

// GetOrderDetail 查询上游订单详情。
func GetOrderDetail(orderNumber string) (*model.OrderDetail, error) {
	var out model.OrderDetail
	body := map[string]any{"orderNumber": orderNumber, "outerNumber": ""}
	if e := request("/api/v3/order/getDetail", body, &out); e != nil {
		return nil, e
	}
	return &out, nil
}

// VoidCard 作废卡券。
func VoidCard(cardID string) (int, string, error) {
	var out struct {
		Status int    `json:"status"`
		Result string `json:"result"`
	}
	if e := request("/api/v3/card/void", map[string]any{"cardId": cardID}, &out); e != nil {
		return 0, "", e
	}
	return out.Status, out.Result, nil
}

// 并发去重的 Cookie 刷新
type wafCall struct {
	done chan struct{}
	res  wafResult
}

var (
	wafMu              sync.Mutex
	wafCur             *wafCall
	refreshWafCookieFn = RefreshWafCookie
)

// RefreshWafCookieOnce 并发去重的 Cookie 刷新。
//
// 注意：挑战脚本可能 panic、死循环或网络请求长时间无响应。这里必须保证：
//   - in-flight 的 done 一定被 close，避免后续所有请求永久等待；
//   - 清理 wafCur 只清理自己，避免覆盖后来者；
//   - 等待方最多等 30 秒，而不是无限等待。
func RefreshWafCookieOnce(baseURL string) wafResult {
	wafMu.Lock()
	if c := wafCur; c != nil {
		wafMu.Unlock()
		select {
		case <-c.done:
			return c.res
		case <-time.After(30 * time.Second):
			wafMu.Lock()
			if wafCur == c {
				wafCur = nil
			}
			wafMu.Unlock()
			return wafResult{Message: "获取 Cookie 超时，请检查服务器出网、DNS 或上游 WAF 状态"}
		}
	}
	c := &wafCall{done: make(chan struct{})}
	wafCur = c
	wafMu.Unlock()

	func() {
		defer func() {
			if r := recover(); r != nil {
				c.res = wafResult{Message: "获取 Cookie 失败：挑战脚本异常"}
			}
			close(c.done)
			wafMu.Lock()
			if wafCur == c {
				wafCur = nil
			}
			wafMu.Unlock()
		}()
		c.res = refreshWafCookieFn(baseURL)
	}()
	return c.res
}
