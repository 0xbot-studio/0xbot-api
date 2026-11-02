package httpapi

import (
	"0xbot.org/api/internal/nft"
	"net/http"
)

func (a *API) nftDetail(w http.ResponseWriter, r *http.Request) {
	var in nft.TokenRequest
	if !decode(w, r, &in) {
		return
	}
	out, err := a.Service.NFTDetail(r.Context(), WorkspaceFromContext(r.Context()), in)
	respond(w, r, out, err)
}
func (a *API) nftOwner(w http.ResponseWriter, r *http.Request) {
	var in nft.TokenRequest
	if !decode(w, r, &in) {
		return
	}
	out, err := a.Service.NFTOwner(r.Context(), WorkspaceFromContext(r.Context()), in)
	respond(w, r, out, err)
}
func (a *API) nftBalance(w http.ResponseWriter, r *http.Request) {
	var in nft.BalanceRequest
	if !decode(w, r, &in) {
		return
	}
	out, err := a.Service.NFTBalance(r.Context(), WorkspaceFromContext(r.Context()), in)
	respond(w, r, out, err)
}
