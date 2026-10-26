package model

// ===== 上游（天启开放平台） =====

type RemoteCategory struct {
	ID       int              `json:"id"`
	Name     string           `json:"name"`
	Img      string           `json:"img,omitempty"`
	Children []RemoteCategory `json:"children,omitempty"`
}

type RemoteProduct struct {
	GoodsID      int     `json:"goodsId"`
	Name         string  `json:"name"`
	FaceValue    float64 `json:"faceValue"`
	SalesPrice   float64 `json:"salesPrice"`
	GoodsType    int     `json:"goodsType"`
	WarrantyDays int     `json:"warrantyDays,omitempty"`
	WarrantyTime int     `json:"warrantyTime,omitempty"`
	WarrantyUnit int     `json:"warrantyUnit,omitempty"`
	Status       int     `json:"status"`
	Multiple     int     `json:"multiple"`
	IsRepeat     int     `json:"isRepeat"`
	IsRefOrder   int     `json:"isRefOrder"`
	IsRefMoney   int     `json:"isRefMoney"`
	SkuType      int     `json:"skuType"`
	ImgURL       string  `json:"imgUrl"`
	DirIDs       []int   `json:"dirIds"`
	StockCount   int     `json:"stockCount"`
}

// GoodsDetail 是商品详情（字段比列表多库存/购买限制）。
type GoodsDetail struct {
	RemoteProduct
	MinQuantity int `json:"minQuantity"`
	MaxQuantity int `json:"maxQuantity"`
}

type CreateOrderResult struct {
	OrderNumber string `json:"orderNumber"`
	Cards       []Card `json:"cards"`
}

type OrderDetail struct {
	OrderNumber string  `json:"orderNumber"`
	OuterNumber string  `json:"outerNumber"`
	Count       int     `json:"count"`
	Money       float64 `json:"money"`
	Status      int     `json:"status"`
	Result      string  `json:"result"`
	Cards       []Card  `json:"cards"`
}

// ===== 商品 / 分类 =====

type Product struct {
	GoodsID        int      `json:"goodsId"`
	Name           string   `json:"name"`
	FaceValue      float64  `json:"faceValue"`
	CostPrice      float64  `json:"costPrice"`
	FinalPrice     float64  `json:"finalPrice"`
	SalesPrice     float64  `json:"salesPrice"`
	GoodsType      int      `json:"goodsType"`
	WarrantyDays   int      `json:"warrantyDays"`
	Status         int      `json:"status"`
	Multiple       int      `json:"multiple"`
	IsRepeat       int      `json:"isRepeat"`
	IsRefOrder     int      `json:"isRefOrder"`
	IsRefMoney     int      `json:"isRefMoney"`
	SkuType        int      `json:"skuType"`
	ImgURL         string   `json:"imgUrl"`
	DirIDs         []int    `json:"dirIds"`
	Platform       string   `json:"platform"`
	Enabled        int      `json:"enabled"`
	ManualPrice    *float64 `json:"manualPrice"`
	PriceLocked    int      `json:"priceLocked"`
	StockCount     int      `json:"stockCount"`
	RootCategoryID *int     `json:"rootCategoryId"`
	CustomName     *string  `json:"customName"`
	CustomDesc     *string  `json:"customDesc"`
	CustomImg      *string  `json:"customImg"`
	UpdatedAt      int64    `json:"updatedAt"`
}

type AdminProduct struct {
	Product
	RootName string `json:"rootName"`
	SubName  string `json:"subName"`
}

type Category struct {
	ID        int    `json:"id"`
	Name      string `json:"name"`
	Img       string `json:"img"`
	ParentID  *int   `json:"parentId"`
	Platform  string `json:"platform"`
	UpdatedAt int64  `json:"updatedAt"`
}

type CategoryNode struct {
	Key          string         `json:"key"`
	ID           int            `json:"id"`
	Title        string         `json:"title"`
	ProductCount int            `json:"productCount"`
	Children     []CategoryNode `json:"children,omitempty"`
}

type StoreSubCategory struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	Img  string `json:"img"`
}

type PlatformStats struct {
	Total    int      `json:"total"`
	OnSale   int      `json:"onSale"`
	MinPrice *float64 `json:"minPrice"`
	MaxPrice *float64 `json:"maxPrice"`
}

type StoreBoard struct {
	ID       int                `json:"id"`
	Name     string             `json:"name"`
	Img      string             `json:"img"`
	Total    int                `json:"total"`
	MinPrice *float64           `json:"minPrice"`
	MaxPrice *float64           `json:"maxPrice"`
	Children []StoreSubCategory `json:"children"`
}

// ===== 订单 / 卡券 =====

type Card struct {
	CardID        string `json:"cardId"`
	CardNo        string `json:"cardNo"`
	CardPwd       string `json:"cardPwd"`
	Status        int    `json:"status"`
	CancelRequest int    `json:"cancelRequest"`
	Result        string `json:"result"`
}

type LocalOrder struct {
	OrderNumber string  `json:"orderNumber"`
	OuterNumber string  `json:"outerNumber"`
	GoodsID     int     `json:"goodsId"`
	GoodsName   string  `json:"goodsName"`
	Count       int     `json:"count"`
	Money       float64 `json:"money"`
	Status      int     `json:"status"`
	Result      string  `json:"result"`
	Platform    string  `json:"platform"`
	CreatedAt   int64   `json:"createdAt"`
	UpdatedAt   int64   `json:"updatedAt"`
	UserUID     *int    `json:"userUid"`
	Username    string  `json:"username"`
	SaleMoney   float64 `json:"saleMoney"`
	CostMoney   float64 `json:"costMoney"`
	Source      string  `json:"source"`
	Settled     int     `json:"settled"`
	Cards       []Card  `json:"cards"`
}

// ===== 用户 / 资产 =====

type User struct {
	UID         int     `json:"uid"`
	Username    string  `json:"username"`
	Nickname    string  `json:"nickname"`
	Phone       string  `json:"phone"`
	Email       string  `json:"email"`
	Role        string  `json:"role"`
	Status      string  `json:"status"`
	Balance     float64 `json:"balance"`
	TotalSpent  float64 `json:"totalSpent"`
	LevelID     *int    `json:"levelId"`
	LevelName   string  `json:"levelName"`
	LevelLocked int     `json:"levelLocked"`
	CreatedAt   int64   `json:"createdAt"`
	UpdatedAt   int64   `json:"updatedAt"`
}

type Level struct {
	ID        int     `json:"id"`
	Name      string  `json:"name"`
	Threshold float64 `json:"threshold"`
	Discount  float64 `json:"discount"`
	Sort      int     `json:"sort"`
}

type LedgerEntry struct {
	ID            int     `json:"id"`
	UID           int     `json:"uid"`
	Username      string  `json:"username"`
	Type          string  `json:"type"`
	Amount        float64 `json:"amount"`
	BalanceBefore float64 `json:"balanceBefore"`
	BalanceAfter  float64 `json:"balanceAfter"`
	OrderNumber   string  `json:"orderNumber"`
	Code          string  `json:"code"`
	OperatorID    *int    `json:"operatorId"`
	Remark        string  `json:"remark"`
	CreatedAt     int64   `json:"createdAt"`
}

type AuditLog struct {
	ID         int    `json:"id"`
	OperatorID *int   `json:"operatorId"`
	Action     string `json:"action"`
	TargetType string `json:"targetType"`
	TargetID   string `json:"targetId"`
	Before     string `json:"before"`
	After      string `json:"after"`
	Remark     string `json:"remark"`
	CreatedAt  int64  `json:"createdAt"`
}

// ===== 定价 =====

type PricingRule struct {
	ID          int     `json:"id"`
	Name        string  `json:"name"`
	Scope       string  `json:"scope"`
	ScopeID     string  `json:"scopeId"`
	FormulaType string  `json:"formulaType"`
	Amount      float64 `json:"amount"`
	Percent     float64 `json:"percent"`
	Multiplier  float64 `json:"multiplier"`
	Fixed       float64 `json:"fixed"`
	Rounding    string  `json:"rounding"`
	Priority    int     `json:"priority"`
	Enabled     int     `json:"enabled"`
	CreatedAt   int64   `json:"createdAt"`
	UpdatedAt   int64   `json:"updatedAt"`
}

// ===== 公告 =====

type Announcement struct {
	ID        int    `json:"id"`
	Type      string `json:"type"`
	Title     string `json:"title"`
	Content   string `json:"content"`
	Enabled   int    `json:"enabled"`
	Sort      int    `json:"sort"`
	CreatedAt int64  `json:"createdAt"`
	UpdatedAt int64  `json:"updatedAt"`
}

// ===== 兑换码 =====

type RedeemCode struct {
	ID          int     `json:"id"`
	Code        string  `json:"code"`
	FaceValue   float64 `json:"faceValue"`
	GoodsID     *int    `json:"goodsId"`
	GoodsName   string  `json:"goodsName"`
	GoodsCount  int     `json:"goodsCount"`
	OrderNumber string  `json:"orderNumber"`
	Status      string  `json:"status"`
	UsedBy      *int    `json:"usedBy"`
	UsedAt      *int64  `json:"usedAt"`
	StartAt     *int64  `json:"startAt"`
	EndAt       *int64  `json:"endAt"`
	CreatedAt   int64   `json:"createdAt"`
}

// ===== 加密充值 =====

type PoolSlot struct {
	ID             int    `json:"id"`
	SubUID         string `json:"subUid"`
	Username       string `json:"username"`
	Address        string `json:"address"`
	AddressTrx     string `json:"addressTrx"`
	AddressBsc     string `json:"addressBsc"`
	Chain          string `json:"chain"`
	Status         string `json:"status"`
	CurrentOrderID *int   `json:"currentOrderId"`
	ReservedUntil  *int64 `json:"reservedUntil"`
	UpdatedAt      int64  `json:"updatedAt"`
}

type CryptoOrder struct {
	ID            int      `json:"id"`
	UID           int      `json:"uid"`
	PoolID        int      `json:"poolId"`
	SubUID        string   `json:"subUid"`
	Address       string   `json:"address"`
	Chain         string   `json:"chain"`
	ChainLabel    string   `json:"chainLabel"`
	RequestedCny  float64  `json:"requestedCny"`
	RequestedUsdt string   `json:"requestedUsdt"`
	UsdtAmount    string   `json:"usdtAmount"`
	Rate          float64  `json:"rate"`
	CreditCny     float64  `json:"creditCny"`
	Status        string   `json:"status"`
	Txid          *string  `json:"txid"`
	ReceivedUsdt  *string  `json:"receivedUsdt"`
	Credited      *float64 `json:"credited"`
	ExpiresAt     int64    `json:"expiresAt"`
	CreatedAt     int64    `json:"createdAt"`
	PaidAt        *int64   `json:"paidAt"`
}

// MailboxItem 是「可取出邮箱」的卡券摘要（取件页列表用）。
type MailboxItem struct {
	OrderNumber string `json:"orderNumber"`
	CardID      string `json:"cardId"`
	Email       string `json:"email"`
	GoodsName   string `json:"goodsName"`
	CreatedAt   int64  `json:"createdAt"`
	Status      int    `json:"status"`
}
