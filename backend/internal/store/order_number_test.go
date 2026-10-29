package store

import (
	"testing"
	"time"

	"hstore/backend/internal/db"
	"hstore/backend/internal/model"
)

// TestOrderNumberIndependentFromUpstream 本地单号必须与上游单号分离：
// 上游单号单独存 upstream_order_no，且能反查回同一条本地订单。
func TestOrderNumberIndependentFromUpstream(t *testing.T) {
	if _, err := db.Open(); err != nil {
		t.Fatal(err)
	}

	const upstreamNo = "2026103000000000001"
	orderNo := NewSystemOrderNo()
	if orderNo == upstreamNo {
		t.Fatal("本地单号不应等于上游单号")
	}

	UpsertOrder(UpsertOrderInput{
		OrderNumber:     orderNo,
		UpstreamOrderNo: upstreamNo,
		OuterNumber:     "FAKA-TEST-1",
		GoodsID:         5329,
		GoodsName:       "测试商品",
		Count:           1,
		Money:           7,
		Status:          3,
		Result:          "下单成功",
		Platform:        "other",
		Source:          "user",
		Contact:         "buyer@example.com",
	})

	got := GetOrder(orderNo)
	if got == nil {
		t.Fatal("按本地单号查不到订单")
	}
	if got.UpstreamOrderNo != upstreamNo {
		t.Fatalf("上游单号未保存，实际 %q", got.UpstreamOrderNo)
	}

	// 按上游单号反查本地订单
	byUpstream := GetOrderByUpstreamNo(upstreamNo)
	if byUpstream == nil || byUpstream.OrderNumber != orderNo {
		t.Fatalf("按上游单号反查失败: %+v", byUpstream)
	}

	// 用上游单号当本地单号查，应该查不到（这正是旧实现的错误）
	if GetOrder(upstreamNo) != nil {
		t.Fatal("上游单号不应能当作本地单号查到订单")
	}
}

// TestApplyRemoteOrderMatchesByUpstreamNo 上游回传详情时按上游单号匹配本地订单，
// 不能新建一条重复记录。
func TestApplyRemoteOrderMatchesByUpstreamNo(t *testing.T) {
	if _, err := db.Open(); err != nil {
		t.Fatal(err)
	}
	const upstreamNo = "2026103000000000002"
	orderNo := NewSystemOrderNo()
	UpsertOrder(UpsertOrderInput{
		OrderNumber:     orderNo,
		UpstreamOrderNo: upstreamNo,
		GoodsID:         5329,
		GoodsName:       "测试商品",
		Count:           1,
		Money:           7,
		Status:          1,
		Platform:        "other",
		Source:          "user",
		Contact:         "buyer2@example.com",
	})

	before := CountOrders(OrderFilter{})
	out := ApplyRemoteOrder(model.OrderDetail{
		OrderNumber: upstreamNo,
		OuterNumber: "FAKA-TEST-2",
		Count:       1,
		Money:       7,
		Status:      3,
		Result:      "上游已完成",
	})
	if out == nil {
		t.Fatal("ApplyRemoteOrder 返回 nil")
	}
	if out.OrderNumber != orderNo {
		t.Fatalf("应更新本地订单 %q，实际 %q", orderNo, out.OrderNumber)
	}
	if out.Status != 3 {
		t.Fatalf("状态未按上游更新: %d", out.Status)
	}
	if after := CountOrders(OrderFilter{}); after != before {
		t.Fatalf("不应新增订单：before=%d after=%d", before, after)
	}
}

// TestNewAlipayOrderNoSharesFormat 支付宝单号与业务单号同一套规则。
func TestNewAlipayOrderNoSharesFormat(t *testing.T) {
	no := NewAlipayOrderNo()
	if len(no) != len(PrefixAlipay)+24 {
		t.Fatalf("支付宝单号格式不一致: %q", no)
	}
	if _, err := time.Parse("20060102150405", no[len(PrefixAlipay):len(PrefixAlipay)+14]); err != nil {
		t.Fatalf("支付宝单号时间部分不可解析: %q", no)
	}
}
