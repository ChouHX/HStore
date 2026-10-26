package mail

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// GraphClient 是 Microsoft Graph 的最小客户端。
type GraphClient struct {
	BaseURL string
	Token   string
	Client  *http.Client
}

func (g *GraphClient) request(pathname string, params map[string]string) (map[string]any, error) {
	target := strings.TrimRight(g.BaseURL, "/") + pathname
	if len(params) > 0 {
		values := url.Values{}
		for key, value := range params {
			values.Set(key, value)
		}
		target += "?" + values.Encode()
	}

	request, err := http.NewRequest(http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+g.Token)
	request.Header.Set("Accept", "application/json")
	// 让 Graph 直接给 HTML 正文，省掉一次转换。
	request.Header.Set("Prefer", `outlook.body-content-type="html"`)

	response, err := g.Client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("Graph 请求失败: %w", err)
	}
	defer response.Body.Close()

	body, err := io.ReadAll(io.LimitReader(response.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		text := strings.TrimSpace(string(body))
		if text == "" {
			text = fmt.Sprintf("Graph request failed (%d)", response.StatusCode)
		}
		return nil, fmt.Errorf("%s", text)
	}

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	return payload, nil
}

const graphListSelect = "id,subject,sender,from,toRecipients,receivedDateTime,bodyPreview"

func graphFolderID(folderKey string) string {
	if folderKey == "spam" {
		return "junkemail"
	}
	return "inbox"
}

func normalizeGraphContentType(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "html", "text/html":
		return "html"
	}
	return "text"
}

func mapGraphAddress(raw any) Address {
	item, _ := raw.(map[string]any)
	email, _ := item["emailAddress"].(map[string]any)
	address, _ := email["address"].(string)
	name, _ := email["name"].(string)
	if strings.TrimSpace(name) == "" {
		name = address
	}
	return newAddress(name, address)
}

func mapGraphRecipients(raw any) []Address {
	list, _ := raw.([]any)
	items := make([]Address, 0, len(list))
	for _, entry := range list {
		items = append(items, mapGraphAddress(entry))
	}
	return items
}

func mapGraphMessage(raw any, folderKey string) Message {
	item, _ := raw.(map[string]any)

	senderRaw := item["sender"]
	if senderRaw == nil {
		senderRaw = item["from"]
	}
	fromRaw := item["from"]
	if fromRaw == nil {
		fromRaw = item["sender"]
	}

	subject, _ := item["subject"].(string)
	if subject == "" {
		subject = "(无主题)"
	}
	received, _ := item["receivedDateTime"].(string)
	if received == "" {
		received = ISOString(time.Time{})
	}
	preview, _ := item["bodyPreview"].(string)
	id, _ := item["id"].(string)

	message := Message{
		ID:               id,
		Folder:           folderKey,
		Subject:          subject,
		Sender:           mapGraphAddress(senderRaw),
		From:             mapGraphAddress(fromRaw),
		ToRecipients:     mapGraphRecipients(item["toRecipients"]),
		ReceivedDateTime: received,
		BodyPreview:      strings.TrimSpace(preview),
	}

	if bodyRaw, exists := item["body"].(map[string]any); exists {
		contentType, _ := bodyRaw["contentType"].(string)
		content, _ := bodyRaw["content"].(string)
		message.Body = &Body{
			ContentType: normalizeGraphContentType(contentType),
			Content:     content,
		}
	} else {
		message.ContentType = "text"
	}
	return message
}

// GraphList 对应 getGraphMailMessagesPaged。
func GraphList(cfg ProviderConfig, account Account, folder string, page, pageSize int, includeBodies bool) (ListResult, error) {
	folderKey := NormalizeFolderKey(folder)
	folderID := graphFolderID(folderKey)
	safePage := clampInt(page, 1, 1<<30)
	safePageSize := clampInt(pageSize, 1, 100)
	skip := (safePage - 1) * safePageSize

	token, err := RefreshAccessToken(cfg.TokenURL, account.ClientID, account.RefreshToken, cfg.GraphOAuthScope, false)
	if err != nil {
		return ListResult{}, err
	}

	client := &GraphClient{BaseURL: cfg.GraphAPIBase, Token: token.AccessToken, Client: cfg.httpClient()}

	selectFields := graphListSelect
	if includeBodies {
		selectFields += ",body"
	}

	// 总数与列表并发取，和 Node 的 Promise.all 一致。
	type totalOutcome struct {
		total int
		err   error
	}
	totalChannel := make(chan totalOutcome, 1)
	go func() {
		payload, err := client.request("/me/mailFolders/"+url.PathEscape(folderID), map[string]string{
			"$select": "totalItemCount",
		})
		if err != nil {
			totalChannel <- totalOutcome{err: err}
			return
		}
		total := 0
		switch typed := payload["totalItemCount"].(type) {
		case float64:
			total = int(typed)
		case json.Number:
			if parsed, err := typed.Int64(); err == nil {
				total = int(parsed)
			}
		}
		if total < 0 {
			total = 0
		}
		totalChannel <- totalOutcome{total: total}
	}()

	payload, listErr := client.request("/me/mailFolders/"+url.PathEscape(folderID)+"/messages", map[string]string{
		"$top":     strconv.Itoa(safePageSize),
		"$skip":    strconv.Itoa(skip),
		"$orderby": "receivedDateTime desc",
		"$select":  selectFields,
	})
	totalOutcomeValue := <-totalChannel
	if listErr != nil {
		return ListResult{}, listErr
	}
	if totalOutcomeValue.err != nil {
		return ListResult{}, totalOutcomeValue.err
	}

	rawItems, _ := payload["value"].([]any)
	items := make([]Message, 0, len(rawItems))
	for _, raw := range rawItems {
		message := mapGraphMessage(raw, folderKey)
		if !includeBodies {
			contentType := "text"
			if message.Body != nil {
				contentType = message.Body.ContentType
			}
			message.Body = nil
			message.ContentType = contentType
		}
		items = append(items, message)
	}

	return ListResult{
		Email:        account.Email,
		Folder:       folderKey,
		Total:        totalOutcomeValue.total,
		Page:         safePage,
		PageSize:     safePageSize,
		Items:        items,
		RefreshToken: token.RefreshToken,
	}, nil
}

// GraphDetail 对应 getGraphMailMessageDetail。
func GraphDetail(cfg ProviderConfig, account Account, folder, messageID string) (DetailResult, error) {
	folderKey := NormalizeFolderKey(folder)
	target := strings.TrimSpace(messageID)
	if target == "" {
		return DetailResult{}, fmt.Errorf("邮件 ID 不能为空")
	}

	token, err := RefreshAccessToken(cfg.TokenURL, account.ClientID, account.RefreshToken, cfg.GraphOAuthScope, false)
	if err != nil {
		return DetailResult{}, err
	}

	client := &GraphClient{BaseURL: cfg.GraphAPIBase, Token: token.AccessToken, Client: cfg.httpClient()}
	payload, err := client.request("/me/messages/"+url.PathEscape(target), map[string]string{
		"$select": graphListSelect + ",body",
	})
	if err != nil {
		return DetailResult{}, err
	}

	return DetailResult{
		Email:        account.Email,
		Folder:       folderKey,
		Item:         mapGraphMessage(payload, folderKey),
		RefreshToken: token.RefreshToken,
	}, nil
}
