package httpapi

import (
	"0xbot.org/api/internal/data"
	"0xbot.org/api/internal/messaging"
	"errors"
	"net/http"
)

func (a *API) sendMessage(w http.ResponseWriter, r *http.Request) {
	var in messaging.SendInput
	if !decode(w, r, &in) {
		return
	}
	out, e := a.Messages.Send(r.Context(), in)
	messageRespond(w, r, out, e)
}
func (a *API) listMessages(w http.ResponseWriter, r *http.Request) {
	var in messaging.ListInput
	if !decode(w, r, &in) {
		return
	}
	out, e := a.Messages.List(r.Context(), in)
	messageRespond(w, r, out, e)
}
func (a *API) getMessage(w http.ResponseWriter, r *http.Request) {
	var in messaging.GetInput
	if !decode(w, r, &in) {
		return
	}
	out, e := a.Messages.Get(r.Context(), in)
	messageRespond(w, r, out, e)
}
func messageRespond(w http.ResponseWriter, r *http.Request, out any, e error) {
	switch {
	case errors.Is(e, messaging.ErrPermission):
		fail(w, r, 403, "message_permission_required")
	case errors.Is(e, messaging.ErrNotFound):
		fail(w, r, 404, "message_not_found")
	case errors.Is(e, messaging.ErrConflict):
		fail(w, r, 409, "idempotency_conflict")
	case e != nil && !errors.Is(e, data.ErrInput) && !errors.Is(e, data.ErrRateLimited):
		fail(w, r, 503, "message_service_unavailable")
	default:
		respond(w, r, out, e)
	}
}
