package identity

import (
	"0xbot.org/api/internal/platform/postgres"
	"0xbot.org/api/internal/platform/token"
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func TestOAuthStateIntegration(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set TEST_DATABASE_URL to a dedicated database ending in _test")
	}
	ctx := context.Background()
	db, err := postgres.Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	var database string
	if err := db.QueryRow(ctx, "SELECT current_database()").Scan(&database); err != nil || !strings.HasSuffix(database, "_test") {
		t.Fatal("dedicated _test database required")
	}
	s := Store{DB: db}
	newState := func(t *testing.T) (string, string, OAuthState) {
		t.Helper()
		state, browser := token.New(), token.New()
		v := OAuthState{BrowserHash: token.Hash(browser), Verifier: token.New(), Origin: "https://app.example.test"}
		if err := s.SaveOAuth(ctx, state, v); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			_, err := db.Exec(ctx, "DELETE FROM oauth_states WHERE state_hash=$1", token.Hash(state))
			if err != nil {
				t.Error(err)
			}
		})
		return state, browser, v
	}
	t.Run("browser_binding_and_replay", func(t *testing.T) {
		state, browser, expected := newState(t)
		if _, err := s.ConsumeOAuth(ctx, state, token.New()); !errors.Is(err, ErrAuth) {
			t.Fatal("wrong browser accepted")
		}
		if _, err := s.ConsumeOAuth(ctx, token.New(), browser); !errors.Is(err, ErrAuth) {
			t.Fatal("wrong state accepted")
		}
		got, err := s.ConsumeOAuth(ctx, state, browser)
		if err != nil || got.Verifier != expected.Verifier || got.Origin != expected.Origin || got.BrowserHash != expected.BrowserHash {
			t.Fatalf("state changed: %+v, %v", got, err)
		}
		if _, err := s.ConsumeOAuth(ctx, state, browser); !errors.Is(err, ErrAuth) {
			t.Fatal("state replay accepted")
		}
	})
	t.Run("expired", func(t *testing.T) {
		state, browser, _ := newState(t)
		if _, err := db.Exec(ctx, "UPDATE oauth_states SET expires_at=now()-interval '1 second' WHERE state_hash=$1", token.Hash(state)); err != nil {
			t.Fatal(err)
		}
		if _, err := s.ConsumeOAuth(ctx, state, browser); !errors.Is(err, ErrAuth) {
			t.Fatal("expired state accepted")
		}
	})
	t.Run("concurrent_single_consumer", func(t *testing.T) {
		state, browser, _ := newState(t)
		var successes atomic.Int32
		var group sync.WaitGroup
		start := make(chan struct{})
		for i := 0; i < 8; i++ {
			group.Add(1)
			go func() {
				defer group.Done()
				<-start
				if _, err := s.ConsumeOAuth(ctx, state, browser); err == nil {
					successes.Add(1)
				} else if !errors.Is(err, ErrAuth) {
					t.Error(err)
				}
			}()
		}
		close(start)
		group.Wait()
		if successes.Load() != 1 {
			t.Fatalf("accepted %d consumers, want 1", successes.Load())
		}
	})
}
