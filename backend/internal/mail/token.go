package mail

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// tokenCache 复现 auth.js 的 accessTokenCache：
// 同一个 refresh token 在过期前复用 access token，避免每次取件都换令牌。
type tokenCacheEntry struct {
	accessToken  string
	refreshToken string
	expiresAt    time.Time
}

type tokenCacheStore struct {
	mutex   sync.Mutex
	entries map[string]tokenCacheEntry
}

var tokenCache = &tokenCacheStore{entries: map[string]tokenCacheEntry{}}

// cacheSkew 与 ACCESS_TOKEN_CACHE_SKEW_MS 一致：提前 60 秒视为过期。
const cacheSkew = 60 * time.Second

// TokenResult 是换令牌的返回。
type TokenResult struct {
	AccessToken  string
	RefreshToken string
	Cached       bool
}

func cacheKey(clientID, scope, refreshToken string) string {
	if scope == "" {
		scope = "(scope omitted)"
	}
	return clientID + "::" + scope + "::" + refreshToken
}

// RefreshAccessToken 对应 auth.js 的 refreshAccessToken。
func RefreshAccessToken(tokenURL, clientID, refreshToken, scope string, omitScope bool) (TokenResult, error) {
	if clientID == "" {
		clientID = "dbc8e03a-b00c-46bd-ae65-b683e7707cb0"
	}
	effectiveScope := scope
	if omitScope {
		effectiveScope = ""
	}

	key := cacheKey(clientID, effectiveScope, refreshToken)

	tokenCache.mutex.Lock()
	if entry, exists := tokenCache.entries[key]; exists {
		if entry.accessToken != "" && entry.expiresAt.After(time.Now().Add(cacheSkew)) {
			tokenCache.mutex.Unlock()
			return TokenResult{
				AccessToken:  entry.accessToken,
				RefreshToken: entry.refreshToken,
				Cached:       true,
			}, nil
		}
	}
	tokenCache.mutex.Unlock()

	form := url.Values{}
	form.Set("client_id", clientID)
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", refreshToken)
	if effectiveScope != "" {
		form.Set("scope", effectiveScope)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	response, err := client.PostForm(tokenURL, form)
	if err != nil {
		return TokenResult{}, fmt.Errorf("换令牌失败: %w", err)
	}
	defer response.Body.Close()

	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return TokenResult{}, fmt.Errorf("换令牌失败: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		text := strings.TrimSpace(string(body))
		if text == "" {
			text = "Refresh token已过期或无效"
		}
		return TokenResult{}, errors.New(text)
	}

	var payload struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
		ErrorDesc    string `json:"error_description"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return TokenResult{}, fmt.Errorf("解析令牌响应失败: %w", err)
	}
	if payload.AccessToken == "" {
		if payload.ErrorDesc != "" {
			return TokenResult{}, errors.New(payload.ErrorDesc)
		}
		return TokenResult{}, errors.New("获取 access_token 失败")
	}

	nextRefresh := payload.RefreshToken
	if nextRefresh == "" {
		nextRefresh = refreshToken
	}
	expiresIn := payload.ExpiresIn
	if expiresIn < 60 {
		expiresIn = 3600
	}
	entry := tokenCacheEntry{
		accessToken:  payload.AccessToken,
		refreshToken: nextRefresh,
		expiresAt:    time.Now().Add(time.Duration(expiresIn) * time.Second),
	}

	tokenCache.mutex.Lock()
	tokenCache.entries[key] = entry
	tokenCache.entries[cacheKey(clientID, effectiveScope, nextRefresh)] = entry
	tokenCache.mutex.Unlock()

	return TokenResult{
		AccessToken:  payload.AccessToken,
		RefreshToken: nextRefresh,
		Cached:       false,
	}, nil
}

// ClearTokenCache 在令牌被判定失效时清掉缓存。
func ClearTokenCache(clientID, scope, refreshToken string) {
	tokenCache.mutex.Lock()
	delete(tokenCache.entries, cacheKey(clientID, scope, refreshToken))
	tokenCache.mutex.Unlock()
}
