// Generate REST contracts from the executable capability schemas, then generate clients from OpenAPI.
package main

import (
	"0xbot.org/api/internal/capabilities"
	"0xbot.org/api/internal/contract"
	"0xbot.org/api/internal/mcpserver"
	"encoding/json"
	"os"
	"strings"
)

func main() {
	var doc map[string]any
	if e := json.Unmarshal(contract.Spec, &doc); e != nil {
		panic(e)
	}
	schemas := doc["components"].(map[string]any)["schemas"].(map[string]any)
	paths := doc["paths"].(map[string]any)
	_, tools := mcpserver.Definitions()
	for _, d := range capabilities.All() {
		if !strings.HasPrefix(d.ID, "swaps.") {
			continue
		}
		for _, t := range tools {
			if t.Name == d.Tool {
				schemas[d.Input] = t.InputSchema
				schemas[d.Output] = t.OutputSchema
			}
		}
		paths[d.Path] = map[string]any{"post": map[string]any{"operationId": d.Tool, "summary": d.Description["en"], "description": strings.Join(d.Rules["en"], " "), "security": []any{map[string]any{"session": []string{}}, map[string]any{"apiKey": []string{}}}, "requestBody": map[string]any{"required": true, "content": map[string]any{"application/json": map[string]any{"schema": map[string]string{"$ref": "#/components/schemas/" + d.Input}}}}, "responses": map[string]any{"200": map[string]any{"description": "Read-only success, not transaction execution", "content": map[string]any{"application/json": map[string]any{"schema": map[string]string{"$ref": "#/components/schemas/" + d.Output}}}}, "default": map[string]any{"description": "Invalid input, unavailable provider or rate limit", "content": map[string]any{"application/json": map[string]any{"schema": map[string]string{"$ref": "#/components/schemas/Error"}}}}}}}
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if e := enc.Encode(doc); e != nil {
		panic(e)
	}
}
