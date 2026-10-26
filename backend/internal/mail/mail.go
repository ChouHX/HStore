// Package mail 提供取件能力：IMAP 与 Microsoft Graph 两条通道。
// 输出结构与 Node 版 imap.js / graph.js 完全一致，前端不需要改动。
package mail

import (
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

// Address 复现 Graph 的收件人结构。
type Address struct {
	EmailAddress struct {
		Name    string `json:"name"`
		Address string `json:"address"`
	} `json:"emailAddress"`
}

// Body 是邮件正文。
type Body struct {
	ContentType string `json:"contentType"`
	Content     string `json:"content"`
}

// Message 是列表与详情共用的邮件结构。
// 列表只填 ContentType，详情只填 Body，与 Node 的两种序列化保持一致。
type Message struct {
	ID               string    `json:"id"`
	Folder           string    `json:"folder"`
	Subject          string    `json:"subject"`
	Sender           Address   `json:"sender"`
	From             Address   `json:"from"`
	ToRecipients     []Address `json:"toRecipients"`
	ReceivedDateTime string    `json:"receivedDateTime"`
	BodyPreview      string    `json:"bodyPreview"`
	ContentType      string    `json:"contentType,omitempty"`
	Body             *Body     `json:"body,omitempty"`
}

// ListResult 是分页列表的返回。
type ListResult struct {
	Email        string    `json:"email"`
	Folder       string    `json:"folder"`
	Total        int       `json:"total"`
	Page         int       `json:"page"`
	PageSize     int       `json:"page_size"`
	Items        []Message `json:"items"`
	RefreshToken string    `json:"-"`
}

// DetailResult 是单封邮件详情的返回。
type DetailResult struct {
	Email        string  `json:"email"`
	Folder       string  `json:"folder"`
	Item         Message `json:"item"`
	RefreshToken string  `json:"-"`
}

// Account 是取件凭据。
type Account struct {
	Email        string
	Password     string
	ClientID     string
	RefreshToken string
	Protocol     string
}

func newAddress(name, address string) Address {
	var item Address
	if strings.TrimSpace(name) == "" {
		name = address
	}
	item.EmailAddress.Name = name
	item.EmailAddress.Address = address
	return item
}

// NormalizeFolderKey 与 normalizeFolderKey 同义：junk 归到 spam，其余归 inbox。
func NormalizeFolderKey(folder string) string {
	normalized := strings.ToLower(strings.TrimSpace(folder))
	if normalized == "spam" || normalized == "junk" {
		return "spam"
	}
	return "inbox"
}

var (
	tagPattern   = regexp.MustCompile(`(?s)<[^>]+>`)
	spacePattern = regexp.MustCompile(`\s+`)
)

// stripTagBlocks 删除 <style> / <script> 整块。
// Go 的 regexp 是 RE2，不支持 (?!...) 前瞻，这里改用大小写无关的线性扫描。
func stripTagBlocks(text, tag string) string {
	open := "<" + tag
	close := "</" + tag
	var builder strings.Builder
	lower := strings.ToLower(text)
	for {
		start := strings.Index(lower, open)
		if start < 0 {
			builder.WriteString(text)
			break
		}
		// 确认是标签边界：<style 后面必须是非字母（空格、>、换行等）。
		after := start + len(open)
		if after < len(lower) && isTagNameRune(lower[after]) {
			builder.WriteString(text[:after])
			text = text[after:]
			lower = lower[after:]
			continue
		}
		builder.WriteString(text[:start])
		builder.WriteString(" ")
		end := strings.Index(lower[start:], close)
		if end < 0 {
			break
		}
		cut := start + end + len(close)
		// 跳过闭合标签的剩余部分（</style>）。
		if closing := strings.Index(text[cut:], ">"); closing >= 0 {
			cut += closing + 1
		}
		text = text[cut:]
		lower = lower[cut:]
	}
	return builder.String()
}

func isTagNameRune(value byte) bool {
	return (value >= 'a' && value <= 'z') || (value >= 'A' && value <= 'Z') || (value >= '0' && value <= '9')
}

// StripHTMLTags 对应 stripHtmlTags。
func StripHTMLTags(html string) string {
	text := stripTagBlocks(html, "style")
	text = stripTagBlocks(text, "script")
	text = tagPattern.ReplaceAllString(text, " ")
	replacer := strings.NewReplacer(
		"&nbsp;", " ", "&amp;", "&", "&lt;", "<", "&gt;", ">",
		"&#39;", "'", "&quot;", `"`,
	)
	text = replacer.Replace(text)
	return strings.TrimSpace(spacePattern.ReplaceAllString(text, " "))
}

// BodyPreviewFromText 截前 160 个字符（按字符而非字节）。
func BodyPreviewFromText(text string) string {
	normalized := strings.TrimSpace(spacePattern.ReplaceAllString(text, " "))
	if utf8.RuneCountInString(normalized) <= 160 {
		return normalized
	}
	runes := []rune(normalized)
	return string(runes[:160])
}

// ISOString 对应 toIsoString：零值回落到当前时间。
func ISOString(value time.Time) string {
	if value.IsZero() {
		return time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	}
	return value.UTC().Format("2006-01-02T15:04:05.000Z")
}

func clampInt(value, min, max int) int {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}
