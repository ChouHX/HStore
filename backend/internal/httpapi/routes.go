package httpapi

import "net/http"

// routes 注册全部 API 路由（与 Next 版 app/api 一一对应）。
func (a *API) routes() {
	// 认证
	a.mux.HandleFunc("POST /api/auth/login", a.handleLogin)
	a.mux.HandleFunc("POST /api/auth/logout", a.handleLogout)
	a.mux.HandleFunc("GET /api/auth/me", a.handleMe)
	a.mux.HandleFunc("POST /api/auth/register/code", a.handleRegisterCode)
	a.mux.HandleFunc("POST /api/auth/register", a.handleRegister)

	// 公开信息
	a.mux.HandleFunc("GET /api/announcements", a.handleAnnouncements)
	a.mux.HandleFunc("GET /api/site/config", a.handleSiteConfig)

	// 商城
	a.mux.HandleFunc("GET /api/store/home", a.handleStoreHome)
	a.mux.HandleFunc("GET /api/store/products", a.handleStoreProducts)
	a.mux.HandleFunc("GET /api/store/stock", a.handleStoreStock)
	a.mux.HandleFunc("GET /api/goods/detail", a.handleGoodsDetail)
	a.mux.HandleFunc("GET /api/img/{key}", a.handleCachedImage)

	// 账户
	a.mux.HandleFunc("GET /api/account", a.handleUpstreamAccount)
	a.mux.HandleFunc("GET /api/me", a.handleMyProfile)
	a.mux.HandleFunc("GET /api/me/ledger", a.handleMyLedger)
	a.mux.HandleFunc("POST /api/me/password", a.handleChangePassword)

	// 订单 / 卡券
	a.mux.HandleFunc("POST /api/order/create", a.handleOrderCreate)
	a.mux.HandleFunc("POST /api/order/detail", a.handleOrderDetail)
	a.mux.HandleFunc("GET /api/order/list", a.handleOrderList)
	a.mux.HandleFunc("POST /api/card/void", a.handleCardVoid)

	// 公开订单查询（游客可用，凭订单号或联系方式）
	a.mux.HandleFunc("GET /api/orders/query", a.handleOrderQuery)
	a.mux.HandleFunc("GET /api/orders/query/detail", a.handleOrderQueryDetail)

	// 兑换 / 取件
	a.mux.HandleFunc("POST /api/redeem", a.handleRedeem)
	a.mux.HandleFunc("POST /api/pickup", a.handlePickup)
	a.mux.HandleFunc("POST /api/mailbox/list", a.handleMailboxList)
	a.mux.HandleFunc("POST /api/mailbox/detail", a.handleMailboxDetail)
	a.mux.HandleFunc("GET /api/mailbox/cards", a.handleMailboxCards)

	// 加密充值
	a.mux.HandleFunc("GET /api/crypto/config", a.handleCryptoConfig)
	a.mux.HandleFunc("POST /api/crypto/order", a.handleCryptoCreate)
	a.mux.HandleFunc("GET /api/crypto/order", a.handleCryptoGet)
	a.mux.HandleFunc("DELETE /api/crypto/order", a.handleCryptoCancel)
	a.mux.HandleFunc("GET /api/crypto/cron", a.handleCryptoCron)

	// 游客直接购买商品
	a.mux.HandleFunc("POST /api/guest/order", a.handleGuestOrderCreate)
	a.mux.HandleFunc("GET /api/guest/order", a.handleGuestOrderGet)

	// 支付宝经营码充值
	a.mux.HandleFunc("GET /api/alipay/config", a.handleAlipayConfig)
	a.mux.HandleFunc("POST /api/alipay/order", a.handleAlipayCreate)
	a.mux.HandleFunc("GET /api/alipay/orders", a.handleAlipayList)
	a.mux.HandleFunc("GET /api/alipay/order", a.handleAlipayGet)
	a.mux.HandleFunc("POST /api/alipay/order/check", a.handleAlipayCheck)
	a.mux.HandleFunc("DELETE /api/alipay/order", a.handleAlipayClose)
	a.mux.HandleFunc("GET /api/alipay/checkout/{token}", a.handleAlipayCheckout)

	// 后台
	a.mux.HandleFunc("GET /api/admin/stats", a.handleAdminStats)
	a.mux.HandleFunc("GET /api/admin/upstream/status", a.handleAdminUpstreamStatus)
	a.mux.HandleFunc("GET /api/admin/products", a.handleAdminProducts)
	a.mux.HandleFunc("POST /api/admin/products", a.handleAdminProductsPost)
	a.mux.HandleFunc("GET /api/admin/product-filters", a.handleAdminProductFilters)
	a.mux.HandleFunc("GET /api/admin/product-info", a.handleAdminProductInfo)
	a.mux.HandleFunc("POST /api/admin/product-info", a.handleAdminProductInfoPost)
	a.mux.HandleFunc("GET /api/admin/catalog/tree", a.handleAdminCatalogTree)
	a.mux.HandleFunc("GET /api/admin/catalog/products", a.handleAdminCatalogProducts)
	a.mux.HandleFunc("POST /api/admin/catalog/import", a.handleAdminCatalogImport)
	a.mux.HandleFunc("GET /api/admin/pricing", a.handleAdminPricing)
	a.mux.HandleFunc("POST /api/admin/pricing", a.handleAdminPricingPost)
	a.mux.HandleFunc("GET /api/admin/levels", a.handleAdminLevels)
	a.mux.HandleFunc("POST /api/admin/levels", a.handleAdminLevelsPost)
	a.mux.HandleFunc("GET /api/admin/users", a.handleAdminUsers)
	a.mux.HandleFunc("POST /api/admin/users", a.handleAdminUsersPost)
	a.mux.HandleFunc("GET /api/admin/ledger", a.handleAdminLedger)
	a.mux.HandleFunc("GET /api/admin/audit", a.handleAdminAudit)
	a.mux.HandleFunc("GET /api/admin/announcements", a.handleAdminAnnouncements)
	a.mux.HandleFunc("POST /api/admin/announcements", a.handleAdminAnnouncementsPost)
	a.mux.HandleFunc("GET /api/admin/redeem", a.handleAdminRedeem)
	a.mux.HandleFunc("POST /api/admin/redeem", a.handleAdminRedeemPost)
	a.mux.HandleFunc("GET /api/admin/settings", a.handleAdminSettings)
	a.mux.HandleFunc("POST /api/admin/settings", a.handleAdminSettingsPost)
	a.mux.HandleFunc("POST /api/admin/settings/cookie", a.handleAdminCookieRefresh)
	a.mux.HandleFunc("POST /api/admin/settings/test", a.handleAdminSettingsTest)
	a.mux.HandleFunc("GET /api/admin/smtp", a.handleAdminSMTPGet)
	a.mux.HandleFunc("POST /api/admin/smtp", a.handleAdminSMTPPost)
	a.mux.HandleFunc("GET /api/admin/crypto", a.handleAdminCrypto)
	a.mux.HandleFunc("POST /api/admin/crypto", a.handleAdminCryptoPost)
	a.mux.HandleFunc("GET /api/admin/alipay", a.handleAdminAlipay)
	a.mux.HandleFunc("POST /api/admin/alipay", a.handleAdminAlipayPost)
	a.mux.HandleFunc("POST /api/sync", a.handleSync)

	// 健康检查
	a.mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		ok(w, map[string]any{"service": "hstore-backend"})
	})
}
