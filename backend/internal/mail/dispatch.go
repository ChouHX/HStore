package mail

// List 按账号声明的协议分派到 Graph 或 IMAP。
func List(cfg ProviderConfig, account Account, folder string, page, pageSize int, includeBodies bool) (ListResult, error) {
	if account.Protocol == "graph" {
		return GraphList(cfg, account, folder, page, pageSize, includeBodies)
	}
	return IMAPList(cfg, account, folder, page, pageSize, includeBodies)
}

// Detail 按账号声明的协议分派到 Graph 或 IMAP。
func Detail(cfg ProviderConfig, account Account, folder, messageID string) (DetailResult, error) {
	if account.Protocol == "graph" {
		return GraphDetail(cfg, account, folder, messageID)
	}
	return IMAPDetail(cfg, account, folder, messageID)
}

// WithContent 是 /api/temp-messages 用的便捷入口：取第一页并带正文。
func WithContent(cfg ProviderConfig, account Account, top int, folder string) ([]Message, string, error) {
	result, err := List(cfg, account, folder, 1, clampInt(top, 1, 50), true)
	if err != nil {
		return nil, "", err
	}
	return result.Items, result.RefreshToken, nil
}
