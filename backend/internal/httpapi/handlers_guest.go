package httpapi

import (
	"net/http"
	"strings"

	"hstore/backend/internal/service"
)

func (a *API) handleGuestOrderCreate(w http.ResponseWriter, r *http.Request) {
	if !allowGuestCreate(clientIP(r)) {
		fail(w, http.StatusTooManyRequests, "操作过于频繁，请稍后再试")
		return
	}
	var body struct {
		GoodsID int                 `json:"goodsId"`
		Count   int                 `json:"count"`
		Attach  []map[string]string `json:"attach"`
		Channel string              `json:"channel"`
		Chain   string              `json:"chain"`
		Contact string              `json:"contact"`
	}
	if err := readJSON(r, &body); err != nil {
		fail(w, http.StatusBadRequest, "参数错误")
		return
	}
	gp, _, _, err := service.CreateGuestPurchase(body.GoodsID, body.Count, body.Attach, body.Channel, body.Chain, body.Contact)
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	view, err := service.GetGuestOrder(r.Context(), gp.Token, false)
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	ok(w, map[string]any{"guest": view})
}

func (a *API) handleGuestOrderGet(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimSpace(r.URL.Query().Get("token"))
	if token == "" {
		fail(w, http.StatusBadRequest, "缺少游客订单凭证")
		return
	}
	view, err := service.GetGuestOrder(r.Context(), token, true)
	if err != nil {
		fail(w, http.StatusNotFound, err.Error())
		return
	}
	ok(w, map[string]any{"guest": view})
}
