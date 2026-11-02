package mcpoauth

import "testing"

func TestScopeNormalization(t *testing.T) {
	for _, raw := range []string{"payments:write", "messages:read", "data:read messages:admin", " "} {
		if _, e := normalizeScope(raw); e == nil {
			t.Fatal("invalid scope accepted", raw)
		}
	}
	for raw, want := range map[string]string{"": Scope, "data:read": Scope, "messages:send data:read messages:read": "data:read messages:read messages:send", "data:read messages:send messages:send": "data:read messages:send"} {
		if got, e := normalizeScope(raw); e != nil || got != want {
			t.Fatal(raw, got, e)
		}
	}
}
