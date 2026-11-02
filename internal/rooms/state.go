package rooms

import (
	"fmt"
	"strings"
	"time"
)

func NewRoom(id, actor string, c Command, now time.Time) Room {
	r := Room{ID: id, Topic: c.Topic, HostID: actor, Visibility: "public", Encryption: "none", Status: "lobby", Members: []Member{{actor, c.Name, "host"}}, Speakers: []string{}, MaxRounds: c.MaxRounds, Events: []Event{}, Summary: Summary{Kind: "pending", MessageSeqs: []int64{}}, CreatedAt: now}
	r.append("created", actor, "", 0, now)
	return r
}
func (r *Room) append(kind, actor, body string, reply int64, now time.Time) {
	r.Seq++
	r.Events = append(r.Events, Event{r.Seq, kind, actor, body, reply, r.Turn, now})
}
func (r *Room) member(actor string) int {
	for i, m := range r.Members {
		if m.ID == actor {
			return i
		}
	}
	return -1
}

// Apply is serialized by the provider. No transport, clock, model or database dependency.
func (r *Room) Apply(actor string, c Command, now time.Time) error {
	if c.Action == "summary" {
		if actor != r.HostID {
			return ErrPermission
		}
		if r.Status != "closed" || r.Summary.Kind != "extractive" || c.ExpectedSeq != r.Seq {
			return ErrConflict
		}
		seen := map[int64]bool{}
		for _, seq := range c.MessageSeqs {
			if seen[seq] {
				return ErrConflict
			}
			valid := false
			for _, e := range r.Events {
				if e.Seq == seq && e.Kind == "message" {
					valid = true
				}
			}
			if !valid {
				return ErrConflict
			}
			seen[seq] = true
		}
		r.Summary = Summary{Kind: "submitted", ThroughSeq: r.Seq, MessageSeqs: append([]int64{}, c.MessageSeqs...), Text: c.Body}
		r.append("summary", actor, "", 0, now)
		return nil
	}
	if r.Status == "closed" {
		return ErrConflict
	}
	i := r.member(actor)
	switch c.Action {
	case "join":
		if i >= 0 {
			return nil
		}
		if len(r.Members) >= 100 || r.Seq >= 200 {
			return ErrConflict
		}
		r.Members = append(r.Members, Member{actor, c.Name, "observer"})
		r.append("joined", actor, "", 0, now)
	case "question":
		if i < 0 {
			return ErrPermission
		}
		count := 0
		for _, e := range r.Events {
			if e.Kind == "question" {
				count++
			}
		}
		if count >= 30 || r.Seq >= 230 {
			return ErrConflict
		}
		r.append("question", actor, c.Body, 0, now)
	case "post":
		if r.Status != "live" || r.CurrentSpeaker != actor {
			return ErrTurn
		}
		if c.ExpectedTurn != r.Turn {
			return ErrConflict
		}
		if c.ReplyTo > 0 {
			found := false
			for _, e := range r.Events {
				if e.Seq == c.ReplyTo && (e.Kind == "message" || e.Kind == "question") {
					found = true
				}
			}
			if !found {
				return ErrConflict
			}
		}
		r.append("message", actor, c.Body, c.ReplyTo, now)
		r.advance(now)
	default:
		if actor != r.HostID {
			return ErrPermission
		}
		if c.ExpectedSeq != r.Seq {
			return ErrConflict
		}
		switch c.Action {
		case "promote", "demote":
			if r.Status != "lobby" || r.Seq >= 200 {
				return ErrConflict
			}
			j := r.member(c.ParticipantID)
			if j < 0 || c.ParticipantID == r.HostID {
				return ErrConflict
			}
			if c.Action == "promote" {
				if r.Members[j].Role == "speaker" || len(r.Speakers) >= 5 {
					return ErrConflict
				}
				r.Members[j].Role = "speaker"
				r.Speakers = append(r.Speakers, c.ParticipantID)
			} else {
				if r.Members[j].Role != "speaker" {
					return ErrConflict
				}
				r.Members[j].Role = "observer"
				for k, id := range r.Speakers {
					if id == c.ParticipantID {
						r.Speakers = append(r.Speakers[:k], r.Speakers[k+1:]...)
						break
					}
				}
			}
			r.append(c.Action, c.ParticipantID, "", 0, now)
		case "start":
			if r.Status != "lobby" || len(r.Speakers) == 0 {
				return ErrConflict
			}
			r.Status = "live"
			r.Turn = 1
			r.CurrentSpeaker = r.Speakers[0]
			r.append("started", actor, "", 0, now)
		case "skip":
			if r.Status != "live" {
				return ErrConflict
			}
			r.append("skipped", r.CurrentSpeaker, "", 0, now)
			r.advance(now)
		case "close":
			r.close(now)
		default:
			return ErrConflict
		}
	}
	return nil
}
func (r *Room) advance(now time.Time) {
	if r.Turn >= r.MaxRounds*len(r.Speakers) {
		r.close(now)
		return
	}
	r.Turn++
	r.CurrentSpeaker = r.Speakers[(r.Turn-1)%len(r.Speakers)]
	r.append("turn", r.CurrentSpeaker, "", 0, now)
}
func (r *Room) close(now time.Time) {
	r.Status = "closed"
	r.CurrentSpeaker = ""
	s := Summary{Kind: "extractive", ThroughSeq: r.Seq, MessageSeqs: []int64{}}
	lines := []string{"Extractive recap — quoted excerpts, not model-generated consensus or fact checking."}
	for _, e := range r.Events {
		if e.Kind == "message" {
			s.MessageSeqs = append(s.MessageSeqs, e.Seq)
			excerpt := []rune(e.Body)
			if len(excerpt) > 240 {
				excerpt = append(excerpt[:240], '…')
			}
			lines = append(lines, fmt.Sprintf("[#%d] %s", e.Seq, string(excerpt)))
		}
	}
	s.Text = strings.Join(lines, "\n")
	r.Summary = s
	r.append("closed", r.HostID, "", 0, now)
}
func (r Room) Page(after int64, limit int) Page {
	p := Page{Events: []Event{}, NextSeq: after, LatestSeq: r.Seq, Status: r.Status}
	for _, e := range r.Events {
		if e.Seq > after && len(p.Events) < limit {
			p.Events = append(p.Events, e)
			p.NextSeq = e.Seq
		}
	}
	return p
}
