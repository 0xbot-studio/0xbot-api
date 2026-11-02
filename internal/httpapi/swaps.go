package httpapi

import (
	"0xbot.org/api/internal/data"
	"0xbot.org/api/internal/swaps"
	"context"
	"net/http"
)

func (a *API) swapTokens(w http.ResponseWriter, r *http.Request) {
	var in swaps.TokensInput
	if !decode(w, r, &in) {
		return
	}
	s, ok := a.Service.(interface {
		SwapTokens(context.Context, string, swaps.TokensInput) (swaps.TokensResult, error)
	})
	if !ok {
		respond(w, r, nil, data.ErrUnavailable)
		return
	}
	out, e := s.SwapTokens(r.Context(), WorkspaceFromContext(r.Context()), in)
	respond(w, r, out, e)
}
func (a *API) swapQuote(w http.ResponseWriter, r *http.Request) {
	var in swaps.QuoteInput
	if !decode(w, r, &in) {
		return
	}
	s, ok := a.Service.(interface {
		SwapQuote(context.Context, string, swaps.QuoteInput) (swaps.QuoteResult, error)
	})
	if !ok {
		respond(w, r, nil, data.ErrUnavailable)
		return
	}
	out, e := s.SwapQuote(r.Context(), WorkspaceFromContext(r.Context()), in)
	respond(w, r, out, e)
}
func (a *API) swapStatus(w http.ResponseWriter, r *http.Request) {
	var in swaps.StatusInput
	if !decode(w, r, &in) {
		return
	}
	s, ok := a.Service.(interface {
		SwapStatus(context.Context, string, swaps.StatusInput) (swaps.StatusResult, error)
	})
	if !ok {
		respond(w, r, nil, data.ErrUnavailable)
		return
	}
	out, e := s.SwapStatus(r.Context(), WorkspaceFromContext(r.Context()), in)
	respond(w, r, out, e)
}
