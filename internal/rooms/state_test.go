package rooms

import (
	"strings"
	"testing"
	"time"
)

func TestRoundtableState(t *testing.T) {
	now := time.Now()
	r := NewRoom("room", "host", Command{Topic: "topic", Name: "Host", MaxRounds: 1}, now)
	if e := r.Apply("observer", Command{Action: "post", Body: "spoof", ExpectedTurn: 1}, now); e != ErrTurn {
		t.Fatal(e)
	}
	for _, id := range []string{"a", "b", "viewer"} {
		if e := r.Apply(id, Command{Action: "join", Name: id}, now); e != nil {
			t.Fatal(e)
		}
	}
	if e := r.Apply("viewer", Command{Action: "promote", ParticipantID: "viewer", ExpectedSeq: r.Seq}, now); e != ErrPermission {
		t.Fatal(e)
	}
	for _, id := range []string{"a", "b"} {
		if e := r.Apply("host", Command{Action: "promote", ParticipantID: id, ExpectedSeq: r.Seq}, now); e != nil {
			t.Fatal(e)
		}
	}
	if e := r.Apply("host", Command{Action: "start", ExpectedSeq: r.Seq - 1}, now); e != ErrConflict {
		t.Fatal(e)
	}
	if e := r.Apply("host", Command{Action: "start", ExpectedSeq: r.Seq}, now); e != nil {
		t.Fatal(e)
	}
	if r.CurrentSpeaker != "a" {
		t.Fatal(r)
	}
	if e := r.Apply("b", Command{Action: "post", ExpectedTurn: 1, Body: "early"}, now); e != ErrTurn {
		t.Fatal(e)
	}
	if e := r.Apply("a", Command{Action: "post", ExpectedTurn: 2, Body: "stale"}, now); e != ErrConflict {
		t.Fatal(e)
	}
	if e := r.Apply("viewer", Command{Action: "question", Body: "question"}, now); e != nil {
		t.Fatal(e)
	}
	if e := r.Apply("a", Command{Action: "post", ExpectedTurn: 1, Body: "first"}, now); e != nil {
		t.Fatal(e)
	}
	if e := r.Apply("b", Command{Action: "post", ExpectedTurn: 2, Body: "second"}, now); e != nil {
		t.Fatal(e)
	}
	if r.Status != "closed" || r.CurrentSpeaker != "" || len(r.Summary.MessageSeqs) != 2 || r.Summary.Kind != "extractive" || r.Summary.ThroughSeq != r.Seq-1 {
		t.Fatal(r)
	}
	if e := r.Apply("host", Command{Action: "skip", ExpectedSeq: r.Seq}, now); e != ErrConflict {
		t.Fatal(e)
	}
	p := r.Page(2, 3)
	if len(p.Events) != 3 || p.NextSeq != 5 {
		t.Fatal(p)
	}
}
func TestInputBounds(t *testing.T) {
	valid := Command{Action: "create", Topic: "topic", Name: "host", MaxRounds: 1, IdempotencyKey: strings.Repeat("a", 16)}
	if e := valid.Validate(); e != nil {
		t.Fatal(e)
	}
	for _, change := range []func(*Command){func(c *Command) { c.MaxRounds = 11 }, func(c *Command) { c.Topic = "\x00" }, func(c *Command) { c.Name = string([]byte{255}) }, func(c *Command) { c.IdempotencyKey = "short" }} {
		c := valid
		change(&c)
		if c.Validate() == nil {
			t.Fatal("invalid accepted")
		}
	}
}

func TestSubmittedSummary(t *testing.T) {
	r := Room{Status: "closed", HostID: "host", Seq: 3, Events: []Event{{Seq: 1, Kind: "message"}, {Seq: 2, Kind: "question"}, {Seq: 3, Kind: "closed"}}, Summary: Summary{Kind: "extractive"}}
	c := Command{Action: "summary", ExpectedSeq: 3, Body: "synthesis", MessageSeqs: []int64{1}}
	if e := r.Apply("other", c, time.Now()); e != ErrPermission {
		t.Fatal(e)
	}
	bad := c
	bad.ExpectedSeq = 2
	if e := r.Apply("host", bad, time.Now()); e != ErrConflict {
		t.Fatal(e)
	}
	for _, refs := range [][]int64{{999}, {2}, {1, 1}} {
		bad = c
		bad.MessageSeqs = refs
		if e := r.Apply("host", bad, time.Now()); e != ErrConflict {
			t.Fatal("invalid references", e)
		}
	}
	if e := r.Apply("host", c, time.Now()); e != nil {
		t.Fatal(e)
	}
	if r.Status != "closed" || r.Summary.Kind != "submitted" || r.Summary.ThroughSeq != 3 || r.Seq != 4 {
		t.Fatal(r)
	}
	c.ExpectedSeq = 4
	if e := r.Apply("host", c, time.Now()); e != ErrConflict {
		t.Fatal("silent summary overwrite", e)
	}
}
