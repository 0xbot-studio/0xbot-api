package messaging

import (
	"strings"
	"testing"
)

func TestInputs(t *testing.T) {
	valid := SendInput{From: "0x" + strings.Repeat("a", 40), To: "0x" + strings.Repeat("b", 40), Body: "hello", IdempotencyKey: "1234567890123456"}
	for _, change := range []func(*SendInput){func(in *SendInput) { in.From = "bad" }, func(in *SendInput) { in.To = "0x" + strings.Repeat("0", 40) }, func(in *SendInput) { in.Body = "  " }, func(in *SendInput) { in.Body = strings.Repeat("中", 1366) }, func(in *SendInput) { in.Body = "a\x00b" }, func(in *SendInput) { in.Body = string([]byte{255}) }, func(in *SendInput) { in.IdempotencyKey = "short" }, func(in *SendInput) { in.Provider = "xmtp" }} {
		in := valid
		change(&in)
		if in.Validate() == nil {
			t.Fatal("invalid input accepted")
		}
	}
	if valid.Validate() != nil || valid.Provider != ProviderID {
		t.Fatal("valid request rejected")
	}
	for _, n := range []int{-1, 51} {
		in := ListInput{Address: valid.To, Limit: n}
		if in.Validate() == nil {
			t.Fatal("invalid limit")
		}
	}
	in := ListInput{Address: valid.To}
	if in.Validate() != nil || in.Limit != 20 {
		t.Fatal("default pagination")
	}
}
