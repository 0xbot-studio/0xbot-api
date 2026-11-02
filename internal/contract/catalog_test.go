package contract_test

import (
	"0xbot.org/api/internal/capabilities"
	"0xbot.org/api/internal/contract"
	"0xbot.org/api/internal/mcpserver"
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// The transports may differ in annotations/constraints, but not field names,
// types or requiredness. Check recursively, including references and arrays.
func TestContractTransportParity(t *testing.T) {
	var root map[string]any
	if err := json.Unmarshal(contract.Spec, &root); err != nil {
		t.Fatal(err)
	}
	var resolve func(any) map[string]any
	resolve = func(value any) map[string]any {
		m, ok := value.(map[string]any)
		if !ok {
			t.Fatalf("not a schema object: %T", value)
		}
		if ref, ok := m["$ref"].(string); ok {
			var target any = root
			for _, part := range strings.Split(strings.TrimPrefix(ref, "#/"), "/") {
				target = target.(map[string]any)[part]
			}
			return resolve(target)
		}
		return m
	}
	var shape func(any) any
	shape = func(value any) any {
		m := resolve(value)
		out := map[string]any{"type": m["type"]}
		if props, ok := m["properties"].(map[string]any); ok {
			p := map[string]any{}
			for k, v := range props {
				p[k] = shape(v)
			}
			out["properties"] = p
		}
		if required, ok := m["required"].([]any); ok {
			names := []string{}
			for _, v := range required {
				names = append(names, v.(string))
			}
			sort.Strings(names)
			out["required"] = names
		}
		if items, ok := m["items"]; ok {
			out["items"] = shape(items)
		}
		return out
	}
	_, tools := mcpserver.Definitions()
	schemas := root["components"].(map[string]any)["schemas"].(map[string]any)
	seen := map[string]bool{}
	for _, d := range capabilities.All() {
		if seen[d.ID] {
			t.Fatal("duplicate capability")
		}
		seen[d.ID] = true
		if root["paths"].(map[string]any)[d.Path] == nil {
			t.Fatal("missing REST mapping")
		}
		found := false
		for _, tool := range tools {
			if tool.Name != d.Tool {
				continue
			}
			found = true
			for _, pair := range []struct {
				name   string
				schema any
			}{{d.Input, tool.InputSchema}, {d.Output, tool.OutputSchema}} {
				raw, err := json.Marshal(pair.schema)
				if err != nil {
					t.Fatal(err)
				}
				var m any
				if err = json.Unmarshal(raw, &m); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(shape(schemas[pair.name]), shape(m)) {
					t.Fatalf("%s: REST and MCP structural contract mismatch\nREST: %#v\nMCP: %#v", pair.name, shape(schemas[pair.name]), shape(m))
				}
			}
		}
		if !found {
			t.Fatal("missing MCP mapping")
		}
	}
}
