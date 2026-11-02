package httpapi

import (
	"0xbot.org/api/internal/data"
	"0xbot.org/api/internal/identity"
	"0xbot.org/api/internal/nft"
	"0xbot.org/api/internal/prices"
	"0xbot.org/api/internal/tokenbound"
	"encoding/json"
	"errors"
	"github.com/go-chi/chi/v5/middleware"
	"io"
	"log/slog"
	"net/http"
	"strconv"
)

func write(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
func fail(w http.ResponseWriter, r *http.Request, status int, code string) {
	write(w, status, map[string]any{"error": map[string]string{"code": code, "request_id": middleware.GetReqID(r.Context())}})
}
func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	return decodeLimit(w, r, v, 32<<10)
}
func decodeLimit(w http.ResponseWriter, r *http.Request, v any, limit int64) bool {
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if e := d.Decode(v); e != nil {
		fail(w, r, 400, "invalid_request")
		return false
	}
	var rest any
	if e := d.Decode(&rest); e != io.EOF {
		fail(w, r, 400, "invalid_request")
		return false
	}
	return true
}
func respond(w http.ResponseWriter, r *http.Request, v any, e error) {
	if e == nil {
		write(w, 200, v)
		return
	}
	switch {
	case errors.Is(e, prices.ErrNotFound):
		fail(w, r, 404, "price_not_found")
	case errors.Is(e, prices.ErrStale):
		fail(w, r, 422, "price_stale")
	case errors.Is(e, prices.ErrConfidence):
		fail(w, r, 422, "price_low_confidence")
	case errors.Is(e, nft.ErrUnsupported):
		fail(w, r, 422, "unsupported_nft_standard")
	case errors.Is(e, nft.ErrReverted):
		fail(w, r, 422, "nft_query_reverted")
	case errors.Is(e, tokenbound.ErrNotAccount):
		fail(w, r, 422, "not_token_bound_account")
	case errors.Is(e, data.ErrRateLimited):
		w.Header().Set("Retry-After", "60")
		fail(w, r, 429, "rate_limited")
	case errors.Is(e, data.ErrInput):
		fail(w, r, 400, "invalid_request")
	case errors.Is(e, data.ErrProvider):
		fail(w, r, 400, "unsupported_provider")
	case errors.Is(e, data.ErrUnavailable):
		fail(w, r, 503, "provider_unavailable")
	case errors.Is(e, identity.ErrAuth):
		fail(w, r, 401, "authentication_required")
	case errors.Is(e, identity.ErrConflict):
		fail(w, r, 409, "identity_already_linked")
	default:
		slog.Error("request failed", "request_id", middleware.GetReqID(r.Context()), "error_type", "internal")
		fail(w, r, 500, "internal_error")
	}
}
func page(r *http.Request) (int, int) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	if limit < 1 || limit > 100 {
		limit = 25
	}
	if offset < 0 {
		offset = 0
	}
	if offset > 100000 {
		offset = 100000
	}
	return limit, offset
}
