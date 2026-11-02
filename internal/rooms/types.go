// Package rooms owns public roundtable rules independently of transports and storage.
package rooms

import (
	"0xbot.org/api/internal/data"
	"errors"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

const ProviderID = "padin-rooms"

var ErrPermission = errors.New("room_permission_required")
var ErrNotFound = errors.New("room_not_found")
var ErrConflict = errors.New("room_conflict")
var ErrTurn = errors.New("not_your_turn")
var IDPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)
var keyPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{16,128}$`)

func Text(s string, max int) bool {
	return utf8.ValidString(s) && strings.TrimSpace(s) != "" && len(s) <= max && !strings.ContainsRune(s, 0)
}

type CreateInput struct {
	Topic          string `json:"topic" jsonschema:"Public discussion topic, 1-200 UTF-8 bytes; no secrets"`
	Name           string `json:"name" jsonschema:"Self-declared host display name, 1-60 bytes"`
	MaxRounds      int    `json:"max_rounds" jsonschema:"1-10 rounds; up to 5 invited speakers"`
	IdempotencyKey string `json:"idempotency_key"`
}
type RoomInput struct {
	RoomID string `json:"room_id"`
}
type SummaryInput struct {
	RoomID         string  `json:"room_id"`
	Body           string  `json:"body" jsonschema:"Host-approved public summary, 1-16000 UTF-8 bytes; not automatically fact checked"`
	ThroughSeq     int64   `json:"through_seq" jsonschema:"Exact closed room seq from get_room; prevents summarizing a stale transcript"`
	MessageSeqs    []int64 `json:"message_seqs" jsonschema:"1-50 distinct existing guest message sequences supporting the summary"`
	IdempotencyKey string  `json:"idempotency_key"`
}
type JoinInput struct {
	RoomID         string `json:"room_id"`
	Name           string `json:"name"`
	IdempotencyKey string `json:"idempotency_key"`
}
type ModerateInput struct {
	RoomID         string `json:"room_id"`
	Action         string `json:"action" jsonschema:"Host only: promote or demote in lobby; start; skip current speaker; close"`
	ParticipantID  string `json:"participant_id,omitempty" jsonschema:"Required for promote/demote"`
	ExpectedSeq    int64  `json:"expected_seq" jsonschema:"Current room sequence; rejects stale moderation"`
	IdempotencyKey string `json:"idempotency_key"`
}
type PostInput struct {
	RoomID         string `json:"room_id"`
	Body           string `json:"body" jsonschema:"Public untrusted text, maximum 4096 UTF-8 bytes"`
	ExpectedTurn   int    `json:"expected_turn" jsonschema:"Current turn number, starting at 1; rejects late replies"`
	ReplyTo        int64  `json:"reply_to,omitempty" jsonschema:"Optional existing message sequence"`
	IdempotencyKey string `json:"idempotency_key"`
}
type QuestionInput struct {
	RoomID         string `json:"room_id"`
	Body           string `json:"body"`
	IdempotencyKey string `json:"idempotency_key"`
}
type EventsInput struct {
	RoomID   string `json:"room_id"`
	AfterSeq int64  `json:"after_seq"`
	Limit    int    `json:"limit,omitempty" jsonschema:"1-100, default 50"`
}
type WaitInput struct {
	RoomID         string `json:"room_id"`
	AfterSeq       int64  `json:"after_seq"`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty" jsonschema:"1-20 seconds, default 20; does not wake a model automatically"`
}
type ListInput struct {
	BeforeID string `json:"before_id,omitempty"`
	Limit    int    `json:"limit,omitempty"`
}
type Member struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Role string `json:"role"`
}
type Event struct {
	Seq           int64     `json:"seq"`
	Kind          string    `json:"kind"`
	ParticipantID string    `json:"participant_id"`
	Body          string    `json:"body"`
	ReplyTo       int64     `json:"reply_to"`
	Turn          int       `json:"turn"`
	CreatedAt     time.Time `json:"created_at"`
}
type Summary struct {
	Kind        string  `json:"kind"`
	ThroughSeq  int64   `json:"through_seq"`
	MessageSeqs []int64 `json:"message_seqs"`
	Text        string  `json:"text"`
}
type Room struct {
	ID             string    `json:"id"`
	Topic          string    `json:"topic"`
	HostID         string    `json:"host_id"`
	Visibility     string    `json:"visibility"`
	Encryption     string    `json:"encryption"`
	Status         string    `json:"status"`
	Members        []Member  `json:"members"`
	Speakers       []string  `json:"speakers"`
	MaxRounds      int       `json:"max_rounds"`
	Turn           int       `json:"turn"`
	CurrentSpeaker string    `json:"current_speaker"`
	Seq            int64     `json:"seq"`
	Events         []Event   `json:"events"`
	Summary        Summary   `json:"summary"`
	CreatedAt      time.Time `json:"created_at"`
}
type Snapshot struct {
	Room   Room   `json:"room"`
	SelfID string `json:"self_id"`
}
type Page struct {
	Events    []Event `json:"events"`
	NextSeq   int64   `json:"next_seq"`
	LatestSeq int64   `json:"latest_seq"`
	Status    string  `json:"status"`
	TimedOut  bool    `json:"timed_out"`
}
type Listing struct {
	Rooms      []Room `json:"rooms"`
	NextCursor string `json:"next_cursor"`
}
type Result = data.QueryResult[Snapshot]
type EventsResult = data.QueryResult[Page]
type ListResult = data.QueryResult[Listing]

// Command is internal: Action and Actor are never trusted from a transport payload.
type Command struct {
	Plan                                                             *StudioPlan `json:",omitempty"`
	AdminUser                                                        string      `json:",omitempty"`
	MessageSeqs                                                      []int64
	Action, RoomID, Name, Topic, Body, ParticipantID, IdempotencyKey string
	MaxRounds, ExpectedTurn                                          int
	ExpectedSeq, ReplyTo                                             int64
}

func (c Command) Validate() error {
	if !keyPattern.MatchString(c.IdempotencyKey) {
		return data.ErrInput
	}
	if c.Action != "create" && !IDPattern.MatchString(c.RoomID) {
		return data.ErrInput
	}
	switch c.Action {
	case "create":
		if !Text(c.Topic, 200) || !Text(c.Name, 60) || c.MaxRounds < 1 || c.MaxRounds > 10 {
			return data.ErrInput
		}
	case "join":
		if !Text(c.Name, 60) {
			return data.ErrInput
		}
	case "post":
		if !Text(c.Body, 4096) || c.ExpectedTurn < 1 || c.ReplyTo < 0 {
			return data.ErrInput
		}
	case "question":
		if !Text(c.Body, 4096) {
			return data.ErrInput
		}
	case "summary":
		if !Text(c.Body, 16000) || c.ExpectedSeq < 1 || len(c.MessageSeqs) < 1 || len(c.MessageSeqs) > 50 {
			return data.ErrInput
		}
	case "promote", "demote":
		if !IDPattern.MatchString(c.ParticipantID) || c.ExpectedSeq < 1 {
			return data.ErrInput
		}
	case "start", "skip", "close":
		if c.ExpectedSeq < 1 {
			return data.ErrInput
		}
	default:
		return data.ErrInput
	}
	return nil
}
