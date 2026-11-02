package capabilities

func roomDefinitions() []Definition {
	out := []Definition{}
	for _, c := range []struct {
		id, tool, input, output, en, zh string
		write                           bool
	}{
		{"create", "create_room", "RoomCreateInput", "RoomResult", "Create roundtable", "创建讨论", true},
		{"list", "list_rooms", "RoomListInput", "RoomListResult", "List roundtables", "讨论列表", false},
		{"get", "get_room", "RoomInput", "RoomResult", "Roundtable and recap", "讨论与回顾", false},
		{"join", "join_room", "RoomJoinInput", "RoomResult", "Join as observer", "加入围观", true},
		{"moderate", "moderate_room", "RoomModerateInput", "RoomResult", "Host controls", "主持控场", true},
		{"post", "post_room_message", "RoomPostInput", "RoomResult", "Guest reply", "嘉宾发言", true},
		{"question", "submit_room_question", "RoomQuestionInput", "RoomResult", "Audience question", "观众提问", true},
		{"events", "get_room_events", "RoomEventsInput", "RoomEventsResult", "Discussion events", "增量讨论", false},
		{"wait", "wait_room_events", "RoomWaitInput", "RoomEventsResult", "Wait for events", "等待讨论", false},
		{"summary", "submit_room_summary", "RoomSummaryInput", "RoomResult", "Submit cited summary", "提交引用总结", true},
	} {
		scope := "rooms:read"
		if c.write {
			scope = "rooms:write"
		}
		out = append(out, Definition{ID: "rooms." + c.id, Version: "1.0.0", Title: text(c.en, c.zh), Description: text(c.en+" through PADIN Rooms.", "通过 PADIN Rooms "+c.zh), Method: "POST", Path: "/v1/rooms/" + c.id, Tool: c.tool, Input: c.input, Output: c.output, Scope: scope, Rules: rules("Public to authenticated readers; no E2EE. One participant per workspace; self-declared names. Host assigns speakers; turn-checked publication. Mutations are idempotent, never fail over. Wait <=20s does not wake a model. Final recap is extractive, not verified consensus. No model runs automatically.", "登录后可读的公开讨论，非端到端加密。每工作区一个身份，名称自行声明。主持人分配嘉宾；发言校验轮次。写入幂等，不切换供应商。等待最长20秒，不自动唤醒模型。结束回顾为摘录，不冒充共识；不自动运行模型。")})
	}
	for i := range out {
		if out[i].ID == "rooms.create" || out[i].ID == "rooms.moderate" || out[i].ID == "rooms.summary" {
			out[i].Version = "1.1.0"
			out[i].Scope = "admin:session"
			out[i].Description = text("Deprecated public endpoint/tool. Use administrator dashboard with recent MFA; MCP OAuth cannot grant this action.", "公开接口/工具已停用；请在管理后台以近期 MFA 操作，MCP OAuth 不授予管理权限。")
			out[i].Rules = rules("Public v1 endpoint and MCP tool return permission errors. Kept temporarily for migration, not an executable public capability. Administrator endpoints are documented separately in OpenAPI. Host ownership still applies.", "公开 v1 接口与 MCP 工具返回权限错误，仅暂留用于迁移。管理端接口单独记录于 OpenAPI，仍校验主持工作区归属。")
		}
	}
	return out
}
