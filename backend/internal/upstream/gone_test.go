package upstream

import (
	"errors"
	"fmt"
	"testing"
)

// 「商品不存在」这类错误必须被识别，否则每轮同步都白拉一次。
func TestIsGoneError(t *testing.T) {
	gone := []error{
		&APIError{Code: 404, Message: "商品不存在"},
		errors.New("wget: server returned error: HTTP/1.1 404 Not Found"),
		errors.New("商品详情为空"),
		&APIError{Code: -1, Message: "Not Found"},
	}
	for _, e := range gone {
		if !IsGoneError(e) {
			t.Errorf("应识别为已下架: %v", e)
		}
	}

	// 限流/网络错误不能误判为下架，否则会把正常商品下架
	keep := []error{
		&APIError{Code: 500, Message: "请求过于频繁，请稍后重试"},
		errors.New("网络错误：context deadline exceeded"),
		fmt.Errorf("解析商品详情失败: %w", errors.New("unexpected end of JSON")),
	}
	for _, e := range keep {
		if IsGoneError(e) {
			t.Errorf("不应识别为已下架: %v", e)
		}
	}
	if IsGoneError(nil) {
		t.Error("nil 不应识别为已下架")
	}
}
