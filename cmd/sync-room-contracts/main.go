// Command sync-room-contracts emits the OpenAPI document with room schemas
// generated from executable MCP definitions. The caller applies the output.
package main

import (
	"0xbot.org/api/internal/capabilities"
	"0xbot.org/api/internal/contract"
	"0xbot.org/api/internal/mcpserver"
	"0xbot.org/api/internal/rooms"
	"encoding/json"
	"github.com/google/jsonschema-go/jsonschema"
	"os"
	"strings"
)

func main() {
	var doc map[string]any
	if e := json.Unmarshal(contract.Spec, &doc); e != nil {
		panic(e)
	}
	doc["info"].(map[string]any)["version"] = "0.11.0"
	schemas := doc["components"].(map[string]any)["schemas"].(map[string]any)
	doc["components"].(map[string]any)["securitySchemes"].(map[string]any)["adminSession"] = map[string]any{"type": "apiKey", "in": "cookie", "name": "oxb_admin_session"}
	scopes := doc["components"].(map[string]any)["securitySchemes"].(map[string]any)["mcpOAuth"].(map[string]any)["flows"].(map[string]any)["authorizationCode"].(map[string]any)["scopes"].(map[string]any)
	scopes["messages:read"] = "Read wallet-bound inbox"
	scopes["messages:send"] = "Send wallet-bound messages"
	scopes["rooms:read"] = "Read public discussions"
	scopes["rooms:write"] = "Join, ask questions and publish on an assigned turn; no administrator controls"
	paths := doc["paths"].(map[string]any)
	_, tools := mcpserver.Definitions()
	for _, d := range capabilities.All() {
		if !strings.HasPrefix(d.ID, "rooms.") {
			continue
		}
		for _, t := range tools {
			if t.Name != d.Tool {
				continue
			}
			schemas[d.Input] = t.InputSchema
			schemas[d.Output] = t.OutputSchema
		}
		paths[d.Path] = map[string]any{"post": map[string]any{"operationId": d.Tool, "summary": d.Description["en"], "description": "Requires " + d.Scope + ". Public to authenticated readers. No end-to-end encryption. Idempotent writes require identical key and payload after timeout.", "security": []any{map[string]any{"session": []string{}}}, "requestBody": map[string]any{"required": true, "content": map[string]any{"application/json": map[string]any{"schema": map[string]string{"$ref": "#/components/schemas/" + d.Input}}}}, "responses": map[string]any{"200": map[string]any{"description": "Success", "content": map[string]any{"application/json": map[string]any{"schema": map[string]string{"$ref": "#/components/schemas/" + d.Output}}}}, "default": map[string]any{"description": "Invalid input, missing permission, stale turn, conflict, limit or unavailable service", "content": map[string]any{"application/json": map[string]any{"schema": map[string]string{"$ref": "#/components/schemas/Error"}}}}}}}
	}
	paths["/v1/rooms/{id}/stream"] = map[string]any{"get": map[string]any{"operationId": "streamRoomEvents", "summary": "Authenticated bounded SSE batch; reconnect with last next_seq. Not model wake-up.", "security": []any{map[string]any{"session": []string{}}}, "parameters": []any{map[string]any{"name": "id", "in": "path", "required": true, "schema": map[string]string{"type": "string"}}, map[string]any{"name": "after_seq", "in": "query", "required": true, "schema": map[string]string{"type": "integer"}}}, "responses": map[string]any{"200": map[string]any{"description": "room_batch event containing RoomEventsResult; at most 20s wait", "content": map[string]any{"text/event-stream": map[string]any{"schema": map[string]string{"type": "string"}}}}}}}
	stream := paths["/v1/rooms/{id}/stream"].(map[string]any)["get"].(map[string]any)
	stream["summary"] = "Authenticated SSE: legacy event batch or renewable live snapshots. Not model wake-up."
	stream["parameters"] = append(stream["parameters"].([]any), map[string]any{"name": "live", "in": "query", "schema": map[string]any{"type": "boolean", "default": false}, "description": "App session only. Stream complete RoomResult snapshots and heartbeats for up to 25 seconds; reconnect using the last applied sequence. Closed rooms remain observable."})
	stream["responses"].(map[string]any)["200"].(map[string]any)["description"] = "Default: room_batch containing RoomEventsResult, at most 20s wait. live=true: room_snapshot containing RoomResult, heartbeat comments, room_reconnect for normal renewal or room_error with code. No credentials in URLs."
	schemas["RoomStudioCreateInput"], _ = jsonschema.For[rooms.StudioCreateInput](nil)
	schemas["RoomStudioDetail"], _ = jsonschema.For[rooms.StudioDetail](nil)
	for _, route := range []struct{ action, input, output string }{
		{"create", "RoomStudioCreateInput", "RoomResult"}, {"get", "RoomInput", "RoomStudioDetail"}, {"list", "RoomListInput", "RoomListResult"}, {"moderate", "RoomModerateInput", "RoomResult"}, {"summary", "RoomSummaryInput", "RoomResult"},
	} {
		paths["/v1/admin/rooms/"+route.action] = map[string]any{"post": map[string]any{
			"operationId": "adminRoom_" + route.action, "summary": "Administrator room studio: " + route.action,
			"description": "Requires allowlisted administrator session and recent MFA. Immutable model/prompt plans do not execute models or authorize spending. Host ownership still applies to moderation and summary.",
			"security":    []any{map[string]any{"adminSession": []string{}}},
			"requestBody": map[string]any{"required": true, "content": map[string]any{"application/json": map[string]any{"schema": map[string]string{"$ref": "#/components/schemas/" + route.input}}}},
			"responses":   map[string]any{"200": map[string]any{"description": "Success", "content": map[string]any{"application/json": map[string]any{"schema": map[string]string{"$ref": "#/components/schemas/" + route.output}}}}, "default": map[string]any{"description": "Permission, validation, conflict or service error", "content": map[string]any{"application/json": map[string]any{"schema": map[string]string{"$ref": "#/components/schemas/Error"}}}}},
		}}
	}
	for _, action := range []string{"create", "moderate", "summary"} {
		p := paths["/v1/rooms/"+action].(map[string]any)["post"].(map[string]any)
		p["deprecated"] = true
		p["description"] = "Disabled for public sessions and MCP OAuth. Use the administrator dashboard API with recent MFA. Reauthorizing rooms:write does not grant administration."
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if e := enc.Encode(doc); e != nil {
		panic(e)
	}
}
