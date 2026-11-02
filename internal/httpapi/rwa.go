package httpapi

import (
	"0xbot.org/api/internal/data"
	"0xbot.org/api/internal/prices"
	"net/http"
)

func (a *API) rwaTokenBalance(w http.ResponseWriter, r *http.Request) {
	var in data.EquityTokenRequest
	if !decode(w, r, &in) {
		return
	}
	out, e := a.Service.RWATokenBalance(r.Context(), WorkspaceFromContext(r.Context()), in)
	respond(w, r, out, e)
}
func (a *API) equityPrice(w http.ResponseWriter, r *http.Request) {
	var in prices.EquityRequest
	if !decode(w, r, &in) {
		return
	}
	out, e := a.Service.EquityPrice(r.Context(), WorkspaceFromContext(r.Context()), in)
	respond(w, r, out, e)
}
