package httpapi

import (
	"0xbot.org/api/internal/access"
	"0xbot.org/api/internal/data"
	"0xbot.org/api/internal/identity"
	"0xbot.org/api/internal/rooms"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/go-chi/chi/v5"
	"net/http"
	"strconv"
)

func roomHandler[In, Out any](fn func(context.Context, In) (Out, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in In
		if !decode(w, r, &in) {
			return
		}
		out, e := fn(r.Context(), in)
		roomRespond(w, r, out, e)
	}
}
func roomRespond(w http.ResponseWriter, r *http.Request, out any, e error) {
	switch {
	case errors.Is(e, rooms.ErrPermission):
		fail(w, r, 403, "room_permission_required")
	case errors.Is(e, rooms.ErrNotFound):
		fail(w, r, 404, "room_not_found")
	case errors.Is(e, rooms.ErrConflict):
		fail(w, r, 409, "room_conflict")
	case errors.Is(e, rooms.ErrTurn):
		fail(w, r, 409, "not_your_turn")
	case e != nil && !errors.Is(e, data.ErrInput) && !errors.Is(e, data.ErrRateLimited):
		fail(w, r, 503, "room_service_unavailable")
	default:
		respond(w, r, out, e)
	}
}
func (a *API) roomRoutes(r chi.Router) {
	s := a.Rooms
	r.Group(func(r chi.Router) {
		r.Use(a.admin, roomAdministrator)
		r.Post("/v1/admin/rooms/create", func(w http.ResponseWriter, r *http.Request) {
			var in rooms.StudioCreateInput
			if !decodeLimit(w, r, &in, 128<<10) {
				return
			}
			out, e := s.CreateStudio(r.Context(), in)
			roomRespond(w, r, out, e)
		})
		r.Post("/v1/admin/rooms/list", roomHandler(s.List))
		r.Post("/v1/admin/rooms/get", roomHandler(s.Studio))
		r.Post("/v1/admin/rooms/moderate", roomHandler(s.Moderate))
		r.Post("/v1/admin/rooms/summary", roomHandler(s.SubmitSummary))
	})
	r.With(a.access).Post("/v1/rooms/create", roomHandler(s.Create))
	r.With(a.access).Post("/v1/rooms/list", roomHandler(s.List))
	r.With(a.access).Post("/v1/rooms/get", roomHandler(s.Get))
	r.With(a.access).Post("/v1/rooms/join", roomHandler(s.Join))
	r.With(a.access).Post("/v1/rooms/moderate", roomHandler(s.Moderate))
	r.With(a.access).Post("/v1/rooms/post", roomHandler(s.Post))
	r.With(a.access).Post("/v1/rooms/question", roomHandler(s.Question))
	r.With(a.access).Post("/v1/rooms/summary", roomHandler(s.SubmitSummary))
	r.With(a.access).Post("/v1/rooms/events", roomHandler(s.Events))
	r.With(a.access).Post("/v1/rooms/wait", roomHandler(s.Wait))
	// A short authenticated SSE batch. Reconnect using the last delivered sequence.
	// No tokens in URLs and no unbounded goroutines or idle response writers.
	r.With(a.access).Get("/v1/rooms/{id}/stream", func(w http.ResponseWriter, r *http.Request) {
		seq, e := strconv.ParseInt(r.URL.Query().Get("after_seq"), 10, 64)
		if e != nil {
			fail(w, r, 400, "invalid_request")
			return
		}
		if r.URL.Query().Get("live") == "true" {
			a.roomLive(w, r, seq)
			return
		}
		out, e := s.Wait(r.Context(), rooms.WaitInput{RoomID: chi.URLParam(r, "id"), AfterSeq: seq, TimeoutSeconds: 20})
		if e != nil {
			roomRespond(w, r, nil, e)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("X-Accel-Buffering", "no")
		raw, _ := json.Marshal(out)
		fmt.Fprintf(w, "id: %d\nevent: room_batch\ndata: %s\n\n", out.Data.NextSeq, raw)
	})
}

// Browser-only renewable SSE; the legacy batch and MCP wait retain their contract.
func (a *API) roomLive(w http.ResponseWriter, r *http.Request, seq int64) {
	started := false
	controller := http.NewResponseController(w)
	err := a.Rooms.Watch(r.Context(), rooms.EventsInput{RoomID: chi.URLParam(r, "id"), AfterSeq: seq}, func(out *rooms.Result) error {
		session, e := a.session(r)
		if e != nil || session.Audience != "app" || session.User.WorkspaceID != access.From(r.Context()).Workspace {
			return rooms.ErrPermission
		}
		if !started {
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("X-Accel-Buffering", "no")
			w.Header().Set("Cache-Control", "no-store, no-transform")
			started = true
		}
		if out == nil {
			_, e = fmt.Fprint(w, ": heartbeat\n\n")
		} else {
			var raw []byte
			raw, e = json.Marshal(out)
			if e == nil {
				_, e = fmt.Fprintf(w, "id: %d\nevent: room_snapshot\ndata: %s\n\n", out.Data.Room.Seq, raw)
			}
		}
		if e != nil {
			return e
		}
		return controller.Flush()
	})
	if !started {
		if err != nil {
			roomRespond(w, r, nil, err)
		}
		return
	}
	if r.Context().Err() != nil {
		return
	}
	if err != nil {
		code := "room_service_unavailable"
		if errors.Is(err, rooms.ErrPermission) {
			code = "authentication_required"
		}
		fmt.Fprintf(w, "event: room_error\ndata: {\"code\":%q}\n\n", code)
	} else {
		fmt.Fprint(w, "event: room_reconnect\ndata: {}\n\n")
	}
	_ = controller.Flush()
}

// Only used after a.admin validates the independent admin session and recent MFA.
func roomAdministrator(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s := r.Context().Value(sessionKey).(identity.Session)
		ctx := access.With(r.Context(), access.Principal{Workspace: s.User.WorkspaceID, Scope: "rooms:read rooms:write"})
		next.ServeHTTP(w, r.WithContext(rooms.WithAdministrator(ctx, s.User.ID)))
	})
}
