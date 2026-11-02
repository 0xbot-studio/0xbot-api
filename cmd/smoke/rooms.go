package main

import (
	"0xbot.org/api/internal/apikeys"
	"0xbot.org/api/internal/platform/token"
	"0xbot.org/api/internal/rooms"
	"context"
	"encoding/json"
	"fmt"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"io"
	"net/http"
	"strings"
	"time"
)

func roomSmoke(api, app string) error {
	host, e := newMessageActor(api, app)
	if e != nil {
		return e
	}
	defer host.call("POST", "/v1/auth/logout", nil, "", 200, nil)
	guest, e := newMessageActor(api, app)
	if e != nil {
		return e
	}
	defer guest.call("POST", "/v1/auth/logout", nil, "", 200, nil)
	observer, e := newMessageActor(api, app)
	if e != nil {
		return e
	}
	defer observer.call("POST", "/v1/auth/logout", nil, "", 200, nil)
	var made rooms.Result
	in := rooms.CreateInput{Topic: "[Deployment smoke test] PADIN Rooms — synthetic participants, no model calls", Name: "Smoke host (test)", MaxRounds: 1, IdempotencyKey: token.New()}
	if e = host.call("POST", "/v1/rooms/create", in, "", 200, &made); e != nil {
		return e
	}
	id := made.Data.Room.ID
	// Leave a failed test closed, never an apparently active unattended discussion.
	defer func() {
		var r rooms.Result
		if host.call("POST", "/v1/rooms/get", rooms.RoomInput{RoomID: id}, "", 200, &r) == nil && r.Data.Room.Status != "closed" {
			if err := host.call("POST", "/v1/rooms/moderate", rooms.ModerateInput{RoomID: id, Action: "close", ExpectedSeq: r.Data.Room.Seq, IdempotencyKey: token.New()}, "", 200, nil); err != nil {
				fmt.Println("WARNING: test room could not be closed")
			}
		}
	}()
	var replay rooms.Result
	if e = host.call("POST", "/v1/rooms/create", in, "", 200, &replay); e != nil {
		return e
	}
	if replay.ID != made.ID {
		return fmt.Errorf("create retry duplicated")
	}
	var joined rooms.Result
	if e = guest.call("POST", "/v1/rooms/join", rooms.JoinInput{RoomID: id, Name: "Smoke guest (test)", IdempotencyKey: token.New()}, "", 200, &joined); e != nil {
		return e
	}
	if e = observer.call("POST", "/v1/rooms/join", rooms.JoinInput{RoomID: id, Name: "Smoke observer (test)", IdempotencyKey: token.New()}, "", 200, nil); e != nil {
		return e
	}
	if e = observer.call("POST", "/v1/rooms/question", rooms.QuestionInput{RoomID: id, Body: "Synthetic question: is permission checking enabled?", IdempotencyKey: token.New()}, "", 200, nil); e != nil {
		return e
	}
	if e = observer.call("POST", "/v1/rooms/moderate", rooms.ModerateInput{RoomID: id, Action: "start", ExpectedSeq: 1, IdempotencyKey: token.New()}, "", 403, nil); e != nil {
		return e
	}
	var key apikeys.Created
	if e = guest.call("POST", "/v1/keys", map[string]string{"name": "Room smoke read-only denial"}, "", 200, &key); e != nil {
		return e
	}
	defer guest.call("DELETE", "/v1/keys/"+key.Key.ID, nil, "", 200, nil)
	if e = guest.call("POST", "/v1/rooms/join", rooms.JoinInput{RoomID: id, Name: "denied", IdempotencyKey: token.New()}, key.Secret, 403, nil); e != nil {
		return e
	}
	refresh := func() error { return host.call("POST", "/v1/rooms/get", rooms.RoomInput{RoomID: id}, "", 200, &made) }
	if e = refresh(); e != nil {
		return e
	}
	if e = host.call("POST", "/v1/rooms/moderate", rooms.ModerateInput{RoomID: id, Action: "promote", ParticipantID: joined.Data.SelfID, ExpectedSeq: made.Data.Room.Seq, IdempotencyKey: token.New()}, "", 200, &made); e != nil {
		return e
	}
	if e = host.call("POST", "/v1/rooms/moderate", rooms.ModerateInput{RoomID: id, Action: "start", ExpectedSeq: made.Data.Room.Seq, IdempotencyKey: token.New()}, "", 200, &made); e != nil {
		return e
	}
	if e = observer.call("POST", "/v1/rooms/post", rooms.PostInput{RoomID: id, Body: "Denied observer", ExpectedTurn: 1, IdempotencyKey: token.New()}, "", 409, nil); e != nil {
		return e
	}
	var page rooms.EventsResult
	if e = guest.call("POST", "/v1/rooms/wait", rooms.WaitInput{RoomID: id, AfterSeq: made.Data.Room.Seq, TimeoutSeconds: 1}, "", 200, &page); e != nil {
		return e
	}
	if !page.Data.TimedOut {
		return fmt.Errorf("wait timeout mismatch")
	}
	// Read the actual SSE framing through the public reverse proxy.
	req, _ := http.NewRequest("GET", api+"/v1/rooms/"+id+"/stream?after_seq=0", nil)
	req.Header.Set("Origin", app)
	res, e := observer.client.Do(req)
	if e != nil {
		return fmt.Errorf("SSE transport failed")
	}
	raw, e := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	res.Body.Close()
	if e != nil || res.StatusCode != 200 || !strings.Contains(string(raw), "event: room_batch") {
		return fmt.Errorf("SSE batch not received")
	}
	grant, revoke, e := guest.grant("data:read rooms:read rooms:write")
	defer revoke()
	if e != nil {
		return e
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	connect := func(secret string) (*mcp.ClientSession, error) {
		return mcp.NewClient(&mcp.Implementation{Name: "room-smoke", Version: "1"}, nil).Connect(ctx, &mcp.StreamableClientTransport{Endpoint: api + "/mcp", HTTPClient: &http.Client{Transport: bearerTransport{secret: secret}, Timeout: 28 * time.Second}}, nil)
	}
	client, e := connect(grant.AccessToken)
	if e != nil {
		return fmt.Errorf("MCP guest connection failed")
	}
	defer client.Close()
	tools, e := client.ListTools(ctx, nil)
	if e != nil || len(tools.Tools) != 27 {
		return fmt.Errorf("expected 27 MCP tools")
	}
	call := func(c *mcp.ClientSession, name string, in any, out any) error {
		result, e := c.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: in})
		if e != nil || result.IsError {
			return fmt.Errorf("MCP %s failed", name)
		}
		raw, e := json.Marshal(result.StructuredContent)
		if e != nil {
			return e
		}
		return json.Unmarshal(raw, out)
	}
	post := rooms.PostInput{RoomID: id, Body: "Synthetic guest reply: authenticated room transport and role checks work. No AI model generated this message.", ExpectedTurn: 1, IdempotencyKey: token.New()}
	if e = call(client, "post_room_message", post, &made); e != nil {
		return e
	}
	if made.Data.Room.Status != "closed" || made.Data.Room.Summary.Kind != "extractive" {
		return fmt.Errorf("final turn did not close and recap")
	}
	if e = call(client, "post_room_message", post, &replay); e != nil || replay.ID != made.ID {
		return fmt.Errorf("MCP reply retry mismatch")
	}
	closed := made.Data.Room
	summary := rooms.SummaryInput{RoomID: id, Body: "Synthetic deployment summary: one test reply completed the round. This is not an AI-generated discussion.", ThroughSeq: closed.Seq, MessageSeqs: closed.Summary.MessageSeqs, IdempotencyKey: token.New()}
	if e = guest.call("POST", "/v1/rooms/summary", summary, "", 403, nil); e != nil {
		return e
	}
	hostGrant, revokeHost, e := host.grant("data:read rooms:read rooms:write")
	defer revokeHost()
	if e != nil {
		return e
	}
	hostClient, e := connect(hostGrant.AccessToken)
	if e != nil {
		return fmt.Errorf("MCP host connection failed")
	}
	defer hostClient.Close()
	if e = call(hostClient, "submit_room_summary", summary, &made); e != nil {
		return e
	}
	if made.Data.Room.Summary.Kind != "submitted" || made.Data.Room.Summary.ThroughSeq != closed.Seq {
		return fmt.Errorf("cited summary mismatch")
	}
	if e = call(client, "wait_room_events", rooms.WaitInput{RoomID: id, AfterSeq: closed.Seq, TimeoutSeconds: 2}, &page); e != nil {
		return e
	}
	if page.Data.Status != "closed" || len(page.Data.Events) != 1 || page.Data.Events[0].Kind != "summary" {
		return fmt.Errorf("summary replay mismatch")
	}
	fmt.Println("Public room REST/MCP create/join/host roles/turn/retry/summary, SSE and bounded waits verified. Synthetic closed test room retained; credentials revoked on exit. No real model calls.")
	return nil
}
