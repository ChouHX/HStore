package upstream

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"hstore/backend/internal/db"
)

const (
	bybitMainnet = "https://api.bybit.com"
	bybitTestnet = "https://api-testnet.bybit.com"
	recvWindow   = "20000"
)

// BybitConfig 是 Bybit API 配置。
type BybitConfig struct {
	APIKey    string
	APISecret string
	BaseURL   string
}

// GetBybitConfig 读取 Bybit 配置。
// 全部来自管理后台写入的 sync_meta，不读环境变量。
func GetBybitConfig() BybitConfig {
	env := db.GetMeta("bybit_env")
	if env == "" {
		env = "mainnet"
	}
	key := db.GetMeta("bybit_api_key")
	secret := db.GetMeta("bybit_api_secret")
	base := bybitMainnet
	if env == "testnet" {
		base = bybitTestnet
	}
	return BybitConfig{APIKey: key, APISecret: secret, BaseURL: base}
}

// IsBybitConfigured 是否已配置。
func IsBybitConfigured() bool {
	c := GetBybitConfig()
	return c.APIKey != "" && c.APISecret != ""
}

// bybitClient 是 Bybit 专用客户端，强制直连、忽略 HTTP(S)_PROXY。
//
// 容器为绕过上游 WAF 配置了 HTTPS_PROXY 指向家宽 SOCKS5 隧道，而该隧道
// 只放行天启域名，连接 Bybit 会被代理直接关闭。若沿用默认 Transport
// （http.ProxyFromEnvironment），查单会全部失败且错误被静默吞掉，
// 表现为「链上已到账但系统一直显示未到账」。
var bybitClient = &http.Client{
	Timeout: 30 * time.Second,
	Transport: &http.Transport{
		Proxy:               nil,
		MaxIdleConns:        16,
		MaxIdleConnsPerHost: 8,
		IdleConnTimeout:     90 * time.Second,
	},
}

type bybitEnvelope struct {
	RetCode int             `json:"retCode"`
	RetMsg  string          `json:"retMsg"`
	Result  json.RawMessage `json:"result"`
}

func signedGet(path string, params map[string]string) (json.RawMessage, error) {
	cfg := GetBybitConfig()
	if cfg.APIKey == "" || cfg.APISecret == "" {
		return nil, fmt.Errorf("未配置 Bybit API Key")
	}
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var pairs []string
	for _, k := range keys {
		pairs = append(pairs, k+"="+url.QueryEscape(params[k]))
	}
	qs := strings.Join(pairs, "&")

	client := bybitClient
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		ts := fmt.Sprintf("%d", time.Now().UnixMilli())
		mac := hmac.New(sha256.New, []byte(cfg.APISecret))
		mac.Write([]byte(ts + cfg.APIKey + recvWindow + qs))
		sign := hex.EncodeToString(mac.Sum(nil))

		target := cfg.BaseURL + path
		if qs != "" {
			target += "?" + qs
		}
		req, e := http.NewRequest(http.MethodGet, target, nil)
		if e != nil {
			return nil, e
		}
		req.Header.Set("X-BAPI-API-KEY", cfg.APIKey)
		req.Header.Set("X-BAPI-TIMESTAMP", ts)
		req.Header.Set("X-BAPI-RECV-WINDOW", recvWindow)
		req.Header.Set("X-BAPI-SIGN", sign)

		res, e := client.Do(req)
		if e != nil {
			lastErr = fmt.Errorf("网络错误：%v", e)
			time.Sleep(800 * time.Millisecond)
			continue
		}
		raw, _ := io.ReadAll(io.LimitReader(res.Body, 4<<20))
		res.Body.Close()

		var env bybitEnvelope
		if e := json.Unmarshal(raw, &env); e != nil {
			lastErr = fmt.Errorf("解析 Bybit 返回失败")
			continue
		}
		if env.RetCode == 0 {
			return env.Result, nil
		}
		// 10002: timestamp 过期，重试
		if env.RetCode == 10002 && attempt < 2 {
			time.Sleep(500 * time.Millisecond)
			continue
		}
		msg := env.RetMsg
		if msg == "" {
			msg = "Bybit 请求失败"
		}
		return nil, fmt.Errorf("%s", msg)
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("Bybit 请求失败")
	}
	return nil, lastErr
}

// SubMemberAddress 查询子账户充值地址。
func SubMemberAddress(subUID, coin, chainCode string) (string, error) {
	raw, e := signedGet("/v5/asset/deposit/query-sub-member-address", map[string]string{
		"coin": coin, "chainType": chainCode, "subMemberId": subUID,
	})
	if e != nil {
		return "", e
	}
	var payload struct {
		Chains json.RawMessage `json:"chains"`
	}
	_ = json.Unmarshal(raw, &payload)
	var list []struct {
		AddressDeposit string `json:"addressDeposit"`
	}
	if len(payload.Chains) > 0 && payload.Chains[0] == '[' {
		_ = json.Unmarshal(payload.Chains, &list)
	} else if len(payload.Chains) > 0 {
		var one struct {
			AddressDeposit string `json:"addressDeposit"`
		}
		if e := json.Unmarshal(payload.Chains, &one); e == nil {
			list = append(list, one)
		}
	}
	for _, c := range list {
		if c.AddressDeposit != "" {
			return c.AddressDeposit, nil
		}
	}
	return "", nil
}

// DepositRow 是子账户充值记录。
type DepositRow struct {
	ID        string `json:"id"`
	Coin      string `json:"coin"`
	Chain     string `json:"chain"`
	Amount    string `json:"amount"`
	TxID      string `json:"txID"`
	Status    int    `json:"status"`
	ToAddress string `json:"toAddress"`
	SuccessAt string `json:"successAt"`
}

// SubMemberDeposits 查询子账户充值记录。
func SubMemberDeposits(subUID, coin string, startTime int64) ([]DepositRow, error) {
	params := map[string]string{"subMemberId": subUID, "coin": coin, "limit": "50"}
	if startTime > 0 {
		params["startTime"] = fmt.Sprintf("%d", startTime)
	}
	raw, e := signedGet("/v5/asset/deposit/query-sub-member-record", params)
	if e != nil {
		return nil, e
	}
	var payload struct {
		Rows []DepositRow `json:"rows"`
	}
	_ = json.Unmarshal(raw, &payload)
	return payload.Rows, nil
}
