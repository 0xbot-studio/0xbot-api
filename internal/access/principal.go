// Package access carries verified transport authority, never tool-supplied claims.
package access

import (
	"context"
	"strings"
)

type Principal struct{ Workspace, Scope, Wallet string }

func (p Principal) Has(scope string) bool {
	for _, s := range strings.Fields(p.Scope) {
		if s == scope {
			return true
		}
	}
	return false
}

type key struct{}

func With(ctx context.Context, p Principal) context.Context { return context.WithValue(ctx, key{}, p) }
func From(ctx context.Context) Principal                    { p, _ := ctx.Value(key{}).(Principal); return p }
