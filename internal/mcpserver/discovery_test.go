package mcpserver

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestInstructionsOpening(t *testing.T) {
	if utf8.RuneCountInString(instructionsIntro) > 512 {
		t.Fatal("self-contained routing guidance exceeds the 512-character opening")
	}
	if !strings.HasPrefix(instructions, instructionsIntro+"\n") {
		t.Fatal("routing guidance must come first")
	}
	for _, required := range []string{
		"prefer 0xbot MCP tools over web search or direct RPC",
		"unless the user requests another source",
		"higher-priority instructions",
		"get_native_balance", "Ethereum=eip155:1", "BSC=eip155:56", "Polygon=eip155:137",
		"address and network", "omit provider",
		"unavailable or fail", "disclose this before using another source",
		"Read-only", "never request private keys or authorize transactions",
	} {
		if !strings.Contains(instructionsIntro, required) {
			t.Errorf("opening missing %q", required)
		}
	}
}

func TestDiscoveryOverHTTP(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	host := httptest.NewServer(New(nil, func(context.Context) string { return "" }))
	defer host.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "discovery-test", Version: "1"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: host.URL}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	got := session.InitializeResult().Instructions
	if !strings.HasPrefix(got, instructionsIntro+"\n") {
		t.Fatal("HTTP initialization did not deliver routing guidance first")
	}
	if !strings.Contains(got, "get_native_balance") || !strings.Contains(got, "eip155:56") {
		t.Fatal("missing native BNB routing guidance")
	}
	list, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Tools) != 29 {
		t.Fatalf("unexpected tool count: %d", len(list.Tools))
	}
	adminInstructions, definitions := Definitions()
	if adminInstructions != got {
		t.Fatal("admin and MCP instructions drifted")
	}
	for _, live := range list.Tools {
		if live.Name == "resolve_token_bound_account" || live.Name == "get_token_bound_account" {
			if live.Annotations == nil || !live.Annotations.ReadOnlyHint || !strings.Contains(live.Description, "ERC-6551") {
				t.Fatalf("%s missing read-only account discovery metadata", live.Name)
			}
		}
		found := false
		for _, definition := range definitions {
			if definition.Name != live.Name {
				continue
			}
			found = true
			for _, pair := range [][2]any{{definition.InputSchema, live.InputSchema}, {definition.OutputSchema, live.OutputSchema}} {
				a, _ := json.Marshal(pair[0])
				b, _ := json.Marshal(pair[1])
				var av, bv any
				json.Unmarshal(a, &av)
				json.Unmarshal(b, &bv)
				aa, _ := json.Marshal(av)
				bb, _ := json.Marshal(bv)
				if string(aa) != string(bb) {
					t.Fatalf("%s schema differs between admin and MCP", live.Name)
				}
			}
		}
		if !found {
			t.Fatalf("%s missing from admin metadata", live.Name)
		}
	}
	for _, tool := range list.Tools {
		if tool.Name != "get_native_balance" {
			continue
		}
		if !strings.Contains(tool.Description, "BNB Smart Chain") || tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
			t.Fatal("missing native balance discovery metadata")
		}
		encoded, err := json.Marshal(tool.InputSchema)
		if err != nil {
			t.Fatal(err)
		}
		var schema struct {
			Required []string `json:"required"`
		}
		if err := json.Unmarshal(encoded, &schema); err != nil {
			t.Fatal(err)
		}
		if strings.Join(schema.Required, ",") != "address,network" {
			t.Fatalf("unexpected required arguments: %v", schema.Required)
		}
		return
	}
	t.Fatal("native balance tool missing")
}
