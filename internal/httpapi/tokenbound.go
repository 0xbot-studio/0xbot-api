package httpapi

import (
	"0xbot.org/api/internal/tokenbound"
	"net/http"
)

func (a *API) resolveTokenBoundAccount(w http.ResponseWriter, r *http.Request) {
	var in tokenbound.ResolveRequest
	if !decode(w, r, &in) {
		return
	}
	out, err := a.Service.ResolveTokenBoundAccount(r.Context(), WorkspaceFromContext(r.Context()), in)
	respond(w, r, out, err)
}
func (a *API) getTokenBoundAccount(w http.ResponseWriter, r *http.Request) {
	var in tokenbound.GetRequest
	if !decode(w, r, &in) {
		return
	}
	out, err := a.Service.GetTokenBoundAccount(r.Context(), WorkspaceFromContext(r.Context()), in)
	respond(w, r, out, err)
}
