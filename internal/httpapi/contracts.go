package httpapi

import (
	"0xbot.org/api/internal/capabilities"
	"0xbot.org/api/internal/data"
	"0xbot.org/api/internal/mcpserver"
	"encoding/json"
	"net/http"
)

// Contract metadata is code-owned; this endpoint cannot modify runtime schemas.
func (a *API) contracts(w http.ResponseWriter, r *http.Request) {
	var spec struct {
		Components struct {
			Schemas map[string]json.RawMessage `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal(a.Spec, &spec); err != nil {
		fail(w, r, 500, "internal_error")
		return
	}
	settings, err := a.Settings.List(r.Context())
	if err != nil {
		respond(w, r, nil, err)
		return
	}
	providers := []map[string]any{}
	for _, s := range settings {
		if !a.Router.Knows(s.ID) {
			continue
		}
		meta := a.Router.Describe(s.ID)
		providers = append(providers, map[string]any{"id": s.ID, "name": meta.Name, "network": meta.Network, "capabilities": meta.Capabilities, "enabled": s.Enabled, "priority": s.Priority, "health": a.Router.Health(s.ID)})
	}
	instructions, tools := mcpserver.Definitions()
	// Only schema names referenced by released contracts are exposed.
	schemas := map[string]json.RawMessage{"Error": spec.Components.Schemas["Error"]}
	for _, c := range capabilities.All() {
		for _, name := range []string{c.Input, c.Output} {
			schemas[name] = spec.Components.Schemas[name]
		}
	}
	write(w, 200, map[string]any{"version": "1.0.0", "capabilities": capabilities.All(), "schemas": schemas, "providers": providers, "tools": tools, "instructions": instructions, "networks": data.NetworkIDs(), "tokens": data.Tokens()})
}
