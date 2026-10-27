// Package imagecache 把上游商品图缓存到本地磁盘，前台只访问 /api/img/{key}，
// 既避免暴露上游站点，也避免每次浏览都中转上游。
package imagecache

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"hstore/backend/internal/db"
	"hstore/backend/internal/store"
	"hstore/backend/internal/upstream"
)

const (
	maxBytes   = 12 << 20
	userAgent  = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"
	maxWarm    = 500
	warmWorker = 4
)

var (
	dir        string
	keyRe      = regexp.MustCompile("^[a-f0-9]{24}$")
	lockMap    sync.Map
	registered sync.Map
)

// Entry 是缓存条目。
type Entry struct {
	Key         string
	URL         string
	File        string
	ContentType string
}

// Init 指定缓存目录（通常是数据库同级的 images 目录）。
func Init(path string) error {
	dir = path
	return os.MkdirAll(path, 0o755)
}

// Key 由原始 URL 计算，不含明文信息。
func Key(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])[:24]
}

// URLFor 把上游图片地址转换为本地地址；非 http(s) 地址原样返回。
func URLFor(raw string) string {
	raw = strings.TrimSpace(raw)
	if !strings.HasPrefix(raw, "http://") && !strings.HasPrefix(raw, "https://") {
		return raw
	}
	return "/api/img/" + Key(raw)
}

func (e Entry) Path() string { return filepath.Join(dir, e.File) }

// LocalURL 登记原始地址并返回本地地址：只写映射不下载，等浏览器首次请求时再拉取。
func LocalURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if !strings.HasPrefix(raw, "http://") && !strings.HasPrefix(raw, "https://") {
		return raw
	}
	Register(raw)
	return "/api/img/" + Key(raw)
}

// Register 记录 URL 与 key 的映射，已存在则跳过。
func Register(raw string) {
	key := Key(raw)
	if _, loaded := registered.LoadOrStore(key, struct{}{}); loaded {
		return
	}
	now := time.Now().UnixMilli()
	if _, err := db.Must().Exec("INSERT OR IGNORE INTO image_cache(key, url, file, content_type, created_at, updated_at) VALUES(?,?,'','',?,?)", key, raw, now, now); err != nil {
		registered.Delete(key)
	}
}

func extFor(contentType string) string {
	switch {
	case strings.Contains(contentType, "jpeg"), strings.Contains(contentType, "jpg"):
		return ".jpg"
	case strings.Contains(contentType, "png"):
		return ".png"
	case strings.Contains(contentType, "webp"):
		return ".webp"
	case strings.Contains(contentType, "gif"):
		return ".gif"
	case strings.Contains(contentType, "svg"):
		return ".svg"
	case strings.Contains(contentType, "avif"):
		return ".avif"
	case strings.Contains(contentType, "bmp"):
		return ".bmp"
	case strings.Contains(contentType, "icon"):
		return ".ico"
	default:
		return ".bin"
	}
}

func lookup(key string) (Entry, bool) {
	var e Entry
	err := db.Must().QueryRow("SELECT key, url, file, content_type FROM image_cache WHERE key = ?", key).
		Scan(&e.Key, &e.URL, &e.File, &e.ContentType)
	if err != nil || e.File == "" {
		return e, false
	}
	if st, err := os.Stat(e.Path()); err != nil || st.Size() == 0 {
		return e, false
	}
	return e, true
}

func saveEntry(raw, contentType, file string) {
	now := time.Now().UnixMilli()
	_, _ = db.Must().Exec("INSERT INTO image_cache(key, url, file, content_type, created_at, updated_at) VALUES(?,?,?,?,?,?) ON CONFLICT(key) DO UPDATE SET url=excluded.url, file=excluded.file, content_type=excluded.content_type, updated_at=excluded.updated_at",
		Key(raw), raw, file, contentType, now, now)
}

func keyLock(key string) *sync.Mutex {
	v, _ := lockMap.LoadOrStore(key, &sync.Mutex{})
	return v.(*sync.Mutex)
}

// Open 返回可用的缓存条目，文件缺失时会按原始 URL 重新下载。
func Open(key string) (Entry, error) {
	if !keyRe.MatchString(key) {
		return Entry{}, errors.New("图片不存在")
	}
	if e, ok := lookup(key); ok {
		return e, nil
	}
	var raw string
	_ = db.Must().QueryRow("SELECT url FROM image_cache WHERE key = ?", key).Scan(&raw)
	if raw == "" {
		return Entry{}, errors.New("图片不存在")
	}
	return ensure(raw)
}

// Ensure 确保原始 URL 已缓存并返回条目。
func Ensure(raw string) (Entry, error) { return ensure(raw) }

func ensure(raw string) (Entry, error) {
	key := Key(raw)
	if e, ok := lookup(key); ok {
		return e, nil
	}
	mu := keyLock(key)
	mu.Lock()
	defer mu.Unlock()
	if e, ok := lookup(key); ok {
		return e, nil
	}

	data, contentType, err := fetch(raw)
	if err != nil {
		return Entry{}, err
	}
	file := key + extFor(contentType)
	tmp := filepath.Join(dir, file+".tmp")
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return Entry{}, err
	}
	if err := os.Rename(tmp, filepath.Join(dir, file)); err != nil {
		_ = os.Remove(tmp)
		return Entry{}, err
	}
	saveEntry(raw, contentType, file)
	return Entry{Key: key, URL: raw, File: file, ContentType: contentType}, nil
}

func fetch(raw string) ([]byte, string, error) {
	target, err := url.Parse(raw)
	if err != nil || (target.Scheme != "http" && target.Scheme != "https") {
		return nil, "", errors.New("图片地址无效")
	}
	cfg := store.GetTianqiSettings()
	base, _ := url.Parse(cfg.BaseURL)
	isUpstream := base != nil && base.Hostname() != "" &&
		(target.Hostname() == base.Hostname() || strings.HasSuffix(target.Hostname(), "."+base.Hostname()))

	referer := target.Scheme + "://" + target.Host + "/"
	if isUpstream && cfg.BaseURL != "" {
		referer = strings.TrimRight(cfg.BaseURL, "/") + "/"
	}
	client := &http.Client{Timeout: 20 * time.Second}

	do := func(cookie string) (*http.Response, error) {
		req, err := http.NewRequest(http.MethodGet, target.String(), nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", userAgent)
		req.Header.Set("Referer", referer)
		req.Header.Set("Accept", "image/avif,image/webp,image/apng,image/*,*/*;q=0.8")
		if cookie != "" {
			req.Header.Set("Cookie", cookie)
		}
		return client.Do(req)
	}

	cookie := ""
	if isUpstream {
		cookie = store.GetCookie()
		if cookie == "" {
			if r := upstream.RefreshWafCookieOnce(cfg.BaseURL); r.OK {
				cookie = r.Cookie
				store.SaveCookie(cookie)
			}
		}
	}

	res, err := do(cookie)
	if err != nil {
		return nil, "", err
	}
	if isUpstream && (res.StatusCode != http.StatusOK || !strings.HasPrefix(res.Header.Get("Content-Type"), "image/")) {
		res.Body.Close()
		if r := upstream.RefreshWafCookieOnce(cfg.BaseURL); r.OK {
			store.SaveCookie(r.Cookie)
			if res, err = do(r.Cookie); err != nil {
				return nil, "", err
			}
		} else {
			return nil, "", errors.New("上游图片不可用")
		}
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, "", errors.New("上游图片不可用")
	}

	data, err := io.ReadAll(io.LimitReader(res.Body, maxBytes+1))
	if err != nil {
		return nil, "", err
	}
	if len(data) == 0 || len(data) > maxBytes {
		return nil, "", errors.New("图片大小异常")
	}
	contentType := res.Header.Get("Content-Type")
	if !strings.HasPrefix(contentType, "image/") {
		contentType = http.DetectContentType(data)
	}
	if !strings.HasPrefix(contentType, "image/") {
		return nil, "", errors.New("上游返回的不是图片")
	}
	if i := strings.Index(contentType, ";"); i > 0 {
		contentType = strings.TrimSpace(contentType[:i])
	}
	return data, contentType, nil
}

// WarmAll 在同步完成后后台预热全部商品图，最多 maxWarm 张、4 个并发。
func WarmAll() {
	urls := store.ProductImageURLs()
	if len(urls) == 0 {
		return
	}
	if len(urls) > maxWarm {
		urls = urls[:maxWarm]
	}
	sem := make(chan struct{}, warmWorker)
	var wg sync.WaitGroup
	for _, u := range urls {
		u := u
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			_, _ = ensure(u)
		}()
	}
	wg.Wait()
}
