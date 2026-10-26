package service

import (
	"encoding/json"
	"strconv"
	"time"

	"hstore/backend/internal/db"
)

// demoGoods 是演示商品定义。
type demoGoods struct {
	Name       string
	Price      float64
	Face       float64
	Warranty   int
	Status     int
	IsRepeat   int
	IsRefOrder int
	IsRefMoney int
}

var demoMicrosoft = []demoGoods{
	{Name: "微软邮箱 Outlook 随机账号（全新注册）", Price: 1.2, Warranty: 24, IsRefOrder: 1, IsRefMoney: 1},
	{Name: "微软邮箱 Outlook 已绑辅助邮箱", Price: 2.5, Warranty: 72, IsRefOrder: 1},
	{Name: "Hotmail 老号 2018 年注册 无手机", Price: 6.8, Warranty: 168, IsRepeat: 1, IsRefMoney: 1},
	{Name: "Outlook 独享号 可改密 长期稳定", Price: 9.9, Warranty: 720, IsRefOrder: 1, IsRefMoney: 1},
	{Name: "微软邮箱 批量拿货 10 个起", Price: 1.0, Warranty: 12, IsRepeat: 1},
	{Name: "Outlook 成品号 已开通两步验证", Price: 4.5, Warranty: 72, IsRefOrder: 1},
	{Name: "Hotmail 白号 秒发 支持 API", Price: 1.8, Warranty: 24, IsRepeat: 1, IsRefMoney: 1},
	{Name: "微软企业邮箱 Office365 子账号", Price: 15.0, Warranty: 1440, IsRefOrder: 1, IsRefMoney: 1},
	{Name: "Outlook 邮箱 带密保 高权重", Price: 7.5, Warranty: 168},
	{Name: "微软邮箱 停售示例（暂停）", Price: 2.0, Warranty: 24, Status: 2},
	{Name: "Hotmail 随机号 支持换绑", Price: 2.2, Warranty: 48, IsRefOrder: 1},
	{Name: "微软邮箱 长期稳定 秒发秒提", Price: 3.6, Warranty: 96, IsRepeat: 1, IsRefMoney: 1},
}

var demoGoogle = []demoGoods{
	{Name: "谷歌邮箱 Gmail 随机账号（全新）", Price: 1.5, Warranty: 24, IsRefOrder: 1, IsRefMoney: 1},
	{Name: "Gmail 老号 2019 年注册 高权重", Price: 5.8, Warranty: 168, IsRepeat: 1},
	{Name: "谷歌邮箱 已绑手机号 可辅助验证", Price: 3.2, Warranty: 72, IsRefOrder: 1},
	{Name: "Google 企业邮箱 独享 可改密", Price: 12.8, Warranty: 720, IsRefMoney: 1},
	{Name: "Gmail 批量号 5 个起 秒发", Price: 1.1, Warranty: 12, IsRepeat: 1},
	{Name: "谷歌邮箱 成品号 已开通 2FA", Price: 4.6, Warranty: 72, IsRefOrder: 1},
	{Name: "Gmail 白号 支持 API 对接", Price: 1.9, Warranty: 24, IsRepeat: 1, IsRefMoney: 1},
	{Name: "谷歌邮箱 带恢复邮箱 长期稳定", Price: 6.5, Warranty: 168, IsRefOrder: 1},
	{Name: "Gmail 个人号 无绑定 即买即用", Price: 2.4, Warranty: 48},
	{Name: "谷歌邮箱 停售示例（禁售）", Price: 2.0, Warranty: 24, Status: 3},
	{Name: "Gmail 高信誉老号 2016 年", Price: 8.9, Warranty: 720, IsRefOrder: 1, IsRefMoney: 1},
	{Name: "谷歌邮箱 秒发稳定 支持退单", Price: 3.0, Warranty: 96, IsRepeat: 1, IsRefOrder: 1},
}

// LoadDemoData 生成演示数据（未配置上游 App-Id 时用于预览完整 UI），返回写入的商品数。
func LoadDemoData() int {
	conn := db.Must()
	now := time.Now().UnixMilli()

	// 清理演示平台的旧数据，保证可重复执行
	if _, e := conn.Exec("DELETE FROM products WHERE platform IN ('microsoft','google')"); e != nil {
		return 0
	}
	if _, e := conn.Exec("DELETE FROM categories WHERE platform IN ('microsoft','google')"); e != nil {
		return 0
	}

	insertCategory := `INSERT INTO categories(id, name, img, parent_id, platform, updated_at)
VALUES(?, ?, '', NULL, ?, ?)
ON CONFLICT(id) DO UPDATE SET name = excluded.name, platform = excluded.platform, updated_at = excluded.updated_at`

	insertProduct := `INSERT INTO products(
goods_id, name, face_value, sales_price, cost_price, final_price, goods_type, warranty_days, status,
multiple, is_repeat, is_ref_order, is_ref_money, sku_type, img_url, dir_ids,
platform, root_category_id, enabled, updated_at)
VALUES(?, ?, ?, ?, ?, ?, 1, ?, ?, 1, ?, ?, ?, 0, '', ?, ?, ?, 1, ?)
ON CONFLICT(goods_id) DO UPDATE SET
name = excluded.name, face_value = excluded.face_value, sales_price = excluded.sales_price,
cost_price = excluded.cost_price, final_price = excluded.final_price,
warranty_days = excluded.warranty_days, status = excluded.status, is_repeat = excluded.is_repeat,
is_ref_order = excluded.is_ref_order, is_ref_money = excluded.is_ref_money,
dir_ids = excluded.dir_ids, platform = excluded.platform,
root_category_id = excluded.root_category_id, enabled = 1, updated_at = excluded.updated_at`

	tx, e := conn.Begin()
	if e != nil {
		return 0
	}
	defer func() { _ = tx.Rollback() }()

	if _, e = tx.Exec(insertCategory, 90001, "微软邮箱", "microsoft", now); e != nil {
		return 0
	}
	if _, e = tx.Exec(insertCategory, 90002, "谷歌邮箱", "google", now); e != nil {
		return 0
	}

	products := 0
	insertAll := func(list []demoGoods, baseID int, dirID int, platform string) bool {
		dirs, _ := json.Marshal([]int{dirID})
		for i, g := range list {
			status := g.Status
			if status == 0 {
				status = 1
			}
			if _, e := tx.Exec(insertProduct,
				baseID+i, g.Name, g.Face, g.Price, g.Price, g.Price,
				g.Warranty, status, g.IsRepeat, g.IsRefOrder, g.IsRefMoney,
				string(dirs), platform, dirID, now,
			); e != nil {
				return false
			}
			products++
		}
		return true
	}

	if !insertAll(demoMicrosoft, 990001, 90001, "microsoft") {
		return 0
	}
	if !insertAll(demoGoogle, 991001, 90002, "google") {
		return 0
	}
	if e = tx.Commit(); e != nil {
		return 0
	}

	_ = db.SetMeta("last_sync", strconv.FormatInt(time.Now().UnixMilli(), 10))
	_ = db.SetMeta("last_source", "demo")
	return products
}
