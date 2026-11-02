package rooms

import (
	"context"
	"time"

	"0xbot.org/api/internal/access"
	"0xbot.org/api/internal/data"
)

// Watch sends complete, durable snapshots so reconnects cannot omit event gaps.
// A nil snapshot is a heartbeat. Closed rooms remain observable for late summaries.
// Transport callbacks must be cancellable and revalidate their session before sending.
func (s *Service) Watch(ctx context.Context, in EventsInput, send func(*Result) error) error {
	return s.watch(ctx, in, send, time.Second, 25*time.Second)
}

func (s *Service) watch(ctx context.Context, in EventsInput, send func(*Result) error, interval, lifetime time.Duration) error {
	if !IDPattern.MatchString(in.RoomID) || in.AfterSeq < 0 {
		return data.ErrInput
	}
	if e := s.authorize(ctx, "rooms:read"); e != nil {
		return e
	}
	release, e := s.acquireWait(ctx)
	if e != nil {
		return e
	}
	defer release()
	ctx, cancel := context.WithTimeout(ctx, lifetime)
	defer cancel()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	seq, first, ticks := in.AfterSeq, true, 0
	for {
		if s.Availability != nil {
			ok, e := s.Availability.Enabled(ctx, ProviderID)
			if e != nil {
				return e
			}
			if !ok {
				return data.ErrUnavailable
			}
		}
		r, e := s.Provider.Get(ctx, in.RoomID)
		if e != nil {
			return e
		}
		if seq > r.Seq {
			return data.ErrInput
		}
		if first || r.Seq > seq {
			out, e := readResult(ctx, s, "rooms.get", Snapshot{Room: r, SelfID: Actor(access.From(ctx).Workspace)})
			if e != nil {
				return e
			}
			if e = send(&out); e != nil {
				return e
			}
			seq, first = r.Seq, false
		} else if ticks%5 == 0 {
			if e = send(nil); e != nil {
				return e
			}
		}
		ticks++
		select {
		case <-ctx.Done():
			if ctx.Err() == context.DeadlineExceeded {
				return nil
			}
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
