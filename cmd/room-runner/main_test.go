package main

import (
	"0xbot.org/api/internal/rooms"
	"context"
	"os"
	"path/filepath"
	"testing"
)

// Only this test executable acts as the driver; production never installs a fake model.
func TestDriverProcess(t *testing.T) {
	if os.Getenv("OXBOT_TEST_ROOM_DRIVER") != "1" {
		return
	}
	if os.Getenv("OXBOT_ROOM_ACCESS_TOKEN") != "" {
		os.Exit(2)
	}
	switch os.Getenv("OXBOT_TEST_DRIVER_MODE") {
	case "bad":
		os.Stdout.WriteString("not-json")
	case "large":
		os.Stdout.Write(make([]byte, 70000))
	default:
		os.Stdout.WriteString(`{"body":"fixture reply"}`)
	}
	os.Exit(0)
}
func TestDriverBoundary(t *testing.T) {
	exe, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	if !filepath.IsAbs(exe) {
		t.Fatal("test executable")
	}
	t.Setenv("OXBOT_TEST_ROOM_DRIVER", "1")
	t.Setenv("OXBOT_ROOM_ACCESS_TOKEN", "must-not-reach-driver")
	for _, mode := range []string{"ok", "bad", "large"} {
		t.Setenv("OXBOT_TEST_DRIVER_MODE", mode)
		body, e := generate(context.Background(), exe, rooms.Room{Topic: "untrusted"})
		if mode == "ok" {
			if e != nil || body != "fixture reply" {
				t.Fatal(e)
			}
		} else if e == nil {
			t.Fatal("invalid driver accepted")
		}
	}
}
