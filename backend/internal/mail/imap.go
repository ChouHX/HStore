package mail

import (
	"bytes"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net"
	"net/http"
	"net/mail"
	"strconv"
	"strings"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"golang.org/x/net/html/charset"
)

// ProviderConfig 是取件需要的连接参数，全部来自环境变量。
type ProviderConfig struct {
	TokenURL        string
	ClientID        string
	IMAPOAuthScope  string
	GraphOAuthScope string
	GraphAPIBase    string

	IMAPServer      string
	IMAPPort        int
	InboxFolderName string
	JunkFolderName  string

	ConnectTimeout  time.Duration
	GreetingTimeout time.Duration
	SocketTimeout   time.Duration
}

func (c ProviderConfig) httpClient() *http.Client {
	return &http.Client{Timeout: 30 * time.Second}
}

// xoauth2Client 实现 sasl.Client 接口，用于 IMAP 的 XOAUTH2 认证。
type xoauth2Client struct {
	username string
	token    string
	replied  bool
}

func (a *xoauth2Client) Start() (string, []byte, error) {
	payload := "user=" + a.username + "\x01auth=Bearer " + a.token + "\x01\x01"
	return "XOAUTH2", []byte(payload), nil
}

// Next 在服务端返回错误详情时回一个空响应结束握手，这是 RFC 7628 的规定。
func (a *xoauth2Client) Next(challenge []byte) ([]byte, error) {
	if a.replied {
		return nil, errors.New("XOAUTH2 认证失败")
	}
	a.replied = true
	return []byte{}, nil
}

func (c ProviderConfig) folderName(folderKey string) string {
	if folderKey == "spam" {
		if c.JunkFolderName != "" {
			return c.JunkFolderName
		}
		return "Junk"
	}
	if c.InboxFolderName != "" {
		return c.InboxFolderName
	}
	return "INBOX"
}

func (c ProviderConfig) imapAddress() string {
	server := c.IMAPServer
	if server == "" {
		server = "outlook.live.com"
	}
	port := c.IMAPPort
	if port == 0 {
		port = 993
	}
	return net.JoinHostPort(server, strconv.Itoa(port))
}

// dialIMAP 建立 TLS 连接并完成 XOAUTH2 认证。
func dialIMAP(cfg ProviderConfig, account Account, accessToken string) (*imapclient.Client, error) {
	options := &imapclient.Options{
		TLSConfig: &tls.Config{ServerName: cfg.IMAPServer, MinVersion: tls.VersionTLS12},
	}
	client, err := imapclient.DialTLS(cfg.imapAddress(), options)
	if err != nil {
		return nil, fmt.Errorf("连接 IMAP 失败: %w", err)
	}
	// 有 access token 走 XOAUTH2；否则用账号密码走 LOGIN（兼容只发「邮箱----密码」的卡密）
	if accessToken != "" {
		if err := client.Authenticate(&xoauth2Client{username: account.Email, token: accessToken}); err != nil {
			_ = client.Close()
			return nil, fmt.Errorf("IMAP 认证失败: %w", err)
		}
		return client, nil
	}
	if account.Password == "" {
		_ = client.Close()
		return nil, fmt.Errorf("IMAP 认证失败: 缺少密码或 refresh token")
	}
	if err := client.Login(account.Email, account.Password).Wait(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("IMAP 认证失败: %w", err)
	}
	return client, nil
}

// rawMessage 是解析前的中间结构。
type rawMessage struct {
	UID          uint32
	SeqNum       uint32
	Envelope     *imap.Envelope
	InternalDate time.Time
	Size         int64
	Source       []byte
}

func mapEnvelopeAddress(addresses []imap.Address) []Address {
	items := make([]Address, 0, len(addresses))
	for _, address := range addresses {
		if address.Mailbox == "" && address.Host == "" {
			continue
		}
		items = append(items, newAddress(address.Name, address.Addr()))
	}
	return items
}

func firstAddress(addresses []imap.Address) Address {
	if len(addresses) == 0 {
		return newAddress("", "")
	}
	address := addresses[0]
	return newAddress(address.Name, address.Addr())
}

// decodeMIMEHeader 解 RFC 2047 编码（=?UTF-8?B?...?=）。
func decodeMIMEHeader(value string) string {
	decoder := &mime.WordDecoder{
		CharsetReader: func(charsetLabel string, input io.Reader) (io.Reader, error) {
			return charset.NewReaderLabel(charsetLabel, input)
		},
	}
	decoded, err := decoder.DecodeHeader(value)
	if err != nil {
		return value
	}
	return decoded
}

// toUTF8 按声明的字符集转码，失败时原样返回。
func toUTF8(body []byte, charsetLabel string) string {
	label := strings.TrimSpace(strings.ToLower(charsetLabel))
	if label == "" || label == "utf-8" || label == "us-ascii" {
		return string(body)
	}
	reader, err := charset.NewReaderLabel(label, bytes.NewReader(body))
	if err != nil {
		return string(body)
	}
	converted, err := io.ReadAll(reader)
	if err != nil {
		return string(body)
	}
	return string(converted)
}

// decodeTransfer 解 Content-Transfer-Encoding。
func decodeTransfer(body []byte, encoding string) []byte {
	switch strings.ToLower(strings.TrimSpace(encoding)) {
	case "base64":
		cleaned := bytes.Map(func(r rune) rune {
			if r == '\r' || r == '\n' || r == ' ' || r == '\t' {
				return -1
			}
			return r
		}, body)
		decoded := make([]byte, base64.StdEncoding.DecodedLen(len(cleaned)))
		written, err := base64.StdEncoding.Decode(decoded, cleaned)
		if err != nil {
			return body
		}
		return decoded[:written]
	case "quoted-printable":
		decoded, err := io.ReadAll(quotedprintable.NewReader(bytes.NewReader(body)))
		if err != nil {
			return body
		}
		return decoded
	}
	return body
}

// parsePart 递归遍历 MIME，收集正文的 html 与 text 版本。
func parsePart(header mail.Header, body []byte, htmlOut *strings.Builder, textOut *strings.Builder) {
	contentType := header.Get("Content-Type")
	if contentType == "" {
		contentType = "text/plain"
	}
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		mediaType = "text/plain"
		params = map[string]string{}
	}

	if strings.HasPrefix(mediaType, "multipart/") {
		boundary := params["boundary"]
		if boundary == "" {
			return
		}
		reader := multipart.NewReader(bytes.NewReader(body), boundary)
		for {
			part, err := reader.NextPart()
			if err != nil {
				return
			}
			partBody, err := io.ReadAll(io.LimitReader(part, 8<<20))
			if err != nil {
				return
			}
			partHeader := mail.Header{}
			for key, values := range part.Header {
				if len(values) > 0 {
					partHeader[key] = values
				}
			}
			// 附件的正文一律不解析。
			disposition := partHeader.Get("Content-Disposition")
			if strings.HasPrefix(strings.ToLower(disposition), "attachment") {
				continue
			}
			parsePart(partHeader, partBody, htmlOut, textOut)
		}
	}

	if mediaType != "text/plain" && mediaType != "text/html" {
		return
	}

	decoded := decodeTransfer(body, header.Get("Content-Transfer-Encoding"))
	text := toUTF8(decoded, params["charset"])
	if mediaType == "text/html" {
		if htmlOut.Len() == 0 {
			htmlOut.WriteString(text)
		}
		return
	}
	if textOut.Len() == 0 {
		textOut.WriteString(text)
	}
}

// extractBody 复现 extractBody：有 html 就用 html，否则用纯文本。
func extractBody(raw []byte) (*Body, string) {
	message, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		return &Body{ContentType: "text", Content: ""}, ""
	}
	body, err := io.ReadAll(io.LimitReader(message.Body, 8<<20))
	if err != nil {
		return &Body{ContentType: "text", Content: ""}, ""
	}

	var htmlBuffer, textBuffer strings.Builder
	parsePart(message.Header, body, &htmlBuffer, &textBuffer)

	if htmlBuffer.Len() > 0 {
		return &Body{ContentType: "html", Content: htmlBuffer.String()}, textBuffer.String()
	}
	return &Body{ContentType: "text", Content: textBuffer.String()}, textBuffer.String()
}

func buildPreview(html, text string) string {
	if strings.TrimSpace(text) != "" {
		return BodyPreviewFromText(text)
	}
	return BodyPreviewFromText(StripHTMLTags(html))
}

// summaryFromRaw 生成列表项（不含正文）。
func summaryFromRaw(raw rawMessage, folderKey string) Message {
	subject := "(无主题)"
	var from Address
	var recipients []Address
	received := raw.InternalDate

	if raw.Envelope != nil {
		if strings.TrimSpace(raw.Envelope.Subject) != "" {
			subject = decodeMIMEHeader(raw.Envelope.Subject)
		}
		if len(raw.Envelope.From) > 0 {
			from = firstAddress(raw.Envelope.From)
		} else if len(raw.Envelope.Sender) > 0 {
			from = firstAddress(raw.Envelope.Sender)
		}
		recipients = mapEnvelopeAddress(raw.Envelope.To)
		if received.IsZero() {
			received = raw.Envelope.Date
		}
	}
	if len(recipients) == 0 {
		recipients = []Address{}
	}

	return Message{
		ID:               strconv.FormatUint(uint64(raw.UID), 10),
		Folder:           folderKey,
		Subject:          subject,
		Sender:           from,
		From:             from,
		ToRecipients:     recipients,
		ReceivedDateTime: ISOString(received),
		BodyPreview:      "",
		ContentType:      "text",
	}
}

// detailFromRaw 生成详情项（含正文）。
func detailFromRaw(raw rawMessage, folderKey string) Message {
	message := summaryFromRaw(raw, folderKey)
	message.ContentType = ""
	if len(raw.Source) == 0 {
		message.Body = &Body{ContentType: "text", Content: ""}
		return message
	}

	body, text := extractBody(raw.Source)
	message.Body = body
	message.BodyPreview = buildPreview(body.Content, text)

	// 头部信息缺失时用解析结果兜底。
	if parsed, err := mail.ReadMessage(bytes.NewReader(raw.Source)); err == nil {
		if message.Subject == "(无主题)" {
			if subject := decodeMIMEHeader(parsed.Header.Get("Subject")); strings.TrimSpace(subject) != "" {
				message.Subject = subject
			}
		}
		if message.ReceivedDateTime == "" {
			if date, err := parsed.Header.Date(); err == nil {
				message.ReceivedDateTime = ISOString(date)
			}
		}
	}
	return message
}

// IMAPList 对应 getMailMessagesPaged 的 IMAP 分支。
func IMAPList(cfg ProviderConfig, account Account, folder string, page, pageSize int, includeBodies bool) (ListResult, error) {
	folderKey := NormalizeFolderKey(folder)
	folderName := cfg.folderName(folderKey)
	safePage := clampInt(page, 1, 1<<30)
	safePageSize := clampInt(pageSize, 1, 100)

	accessToken := ""
	refreshOut := account.RefreshToken
	if account.RefreshToken != "" {
		tk, err := RefreshAccessToken(cfg.TokenURL, account.ClientID, account.RefreshToken, cfg.IMAPOAuthScope, false)
		if err != nil {
			return ListResult{}, err
		}
		accessToken = tk.AccessToken
		refreshOut = tk.RefreshToken
	}

	client, err := dialIMAP(cfg, account, accessToken)
	if err != nil {
		return ListResult{}, err
	}
	defer client.Close()

	selectData, err := client.Select(folderName, &imap.SelectOptions{ReadOnly: true}).Wait()
	if err != nil {
		return ListResult{}, fmt.Errorf("打开邮箱目录失败: %w", err)
	}

	total := int(selectData.NumMessages)
	endSeq := total - (safePage-1)*safePageSize
	if endSeq < 1 {
		return ListResult{
			Email:        account.Email,
			Folder:       folderKey,
			Total:        total,
			Page:         safePage,
			PageSize:     safePageSize,
			Items:        []Message{},
			RefreshToken: refreshOut,
		}, nil
	}
	startSeq := endSeq - safePageSize + 1
	if startSeq < 1 {
		startSeq = 1
	}

	sequenceSet := imap.SeqSet{}
	sequenceSet.AddRange(uint32(startSeq), uint32(endSeq))

	options := &imap.FetchOptions{
		UID:          true,
		Envelope:     true,
		InternalDate: true,
		RFC822Size:   true,
	}
	if includeBodies {
		options.BodySection = []*imap.FetchItemBodySection{{Peek: true}}
	}

	buffers, err := client.Fetch(sequenceSet, options).Collect()
	if err != nil {
		return ListResult{}, fmt.Errorf("拉取邮件失败: %w", err)
	}

	items := make([]Message, 0, len(buffers))
	for _, buffer := range buffers {
		raw := rawMessage{
			UID:          uint32(buffer.UID),
			SeqNum:       buffer.SeqNum,
			Envelope:     buffer.Envelope,
			InternalDate: buffer.InternalDate,
			Size:         buffer.RFC822Size,
		}
		if len(buffer.BodySection) > 0 {
			raw.Source = buffer.BodySection[0].Bytes
		}
		if includeBodies {
			items = append(items, detailFromRaw(raw, folderKey))
		} else {
			items = append(items, summaryFromRaw(raw, folderKey))
		}
	}
	// 服务端按序号升序返回，列表按最新在前展示。
	for left, right := 0, len(items)-1; left < right; left, right = left+1, right-1 {
		items[left], items[right] = items[right], items[left]
	}

	return ListResult{
		Email:        account.Email,
		Folder:       folderKey,
		Total:        total,
		Page:         safePage,
		PageSize:     safePageSize,
		Items:        items,
		RefreshToken: refreshOut,
	}, nil
}

// IMAPDetail 对应 getMailMessageDetail 的 IMAP 分支。
func IMAPDetail(cfg ProviderConfig, account Account, folder, messageID string) (DetailResult, error) {
	folderKey := NormalizeFolderKey(folder)
	folderName := cfg.folderName(folderKey)
	target := strings.TrimSpace(messageID)
	if target == "" {
		return DetailResult{}, errors.New("邮件 ID 不能为空")
	}
	uidValue, err := strconv.ParseUint(target, 10, 32)
	if err != nil {
		return DetailResult{}, errors.New("邮件 ID 不合法")
	}

	accessToken := ""
	refreshOut := account.RefreshToken
	if account.RefreshToken != "" {
		tk, err := RefreshAccessToken(cfg.TokenURL, account.ClientID, account.RefreshToken, cfg.IMAPOAuthScope, false)
		if err != nil {
			return DetailResult{}, err
		}
		accessToken = tk.AccessToken
		refreshOut = tk.RefreshToken
	}

	client, err := dialIMAP(cfg, account, accessToken)
	if err != nil {
		return DetailResult{}, err
	}
	defer client.Close()

	if _, err := client.Select(folderName, &imap.SelectOptions{ReadOnly: true}).Wait(); err != nil {
		return DetailResult{}, fmt.Errorf("打开邮箱目录失败: %w", err)
	}

	uidSet := imap.UIDSet{}
	uidSet.AddNum(imap.UID(uidValue))

	buffers, err := client.Fetch(uidSet, &imap.FetchOptions{
		UID:          true,
		Envelope:     true,
		InternalDate: true,
		RFC822Size:   true,
		BodySection:  []*imap.FetchItemBodySection{{Peek: true}},
	}).Collect()
	if err != nil {
		return DetailResult{}, fmt.Errorf("拉取邮件失败: %w", err)
	}
	if len(buffers) == 0 {
		return DetailResult{}, errors.New("未找到指定邮件")
	}

	buffer := buffers[0]
	raw := rawMessage{
		UID:          uint32(buffer.UID),
		SeqNum:       buffer.SeqNum,
		Envelope:     buffer.Envelope,
		InternalDate: buffer.InternalDate,
		Size:         buffer.RFC822Size,
	}
	if len(buffer.BodySection) > 0 {
		raw.Source = buffer.BodySection[0].Bytes
	}

	return DetailResult{
		Email:        account.Email,
		Folder:       folderKey,
		Item:         detailFromRaw(raw, folderKey),
		RefreshToken: refreshOut,
	}, nil
}
