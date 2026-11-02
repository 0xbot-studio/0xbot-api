package httpapi

import (
	"0xbot.org/api/internal/prices"
	"net/http"
)

func (a *API) assetPrice(w http.ResponseWriter, r *http.Request) {
	var in prices.AssetRequest
	if !decode(w, r, &in) {
		return
	}
	v, e := a.Service.AssetPrice(r.Context(), WorkspaceFromContext(r.Context()), in)
	respond(w, r, v, e)
}
func (a *API) tokenPrice(w http.ResponseWriter, r *http.Request) {
	var in prices.TokenRequest
	if !decode(w, r, &in) {
		return
	}
	v, e := a.Service.TokenPrice(r.Context(), WorkspaceFromContext(r.Context()), in)
	respond(w, r, v, e)
}
