package main

import "testing"

func TestParseTarget(t *testing.T) {
	if _, err := parseTarget("127.0.0.1:7777"); err != nil {
		t.Fatalf("valid target rejected: %v", err)
	}
	bad := []string{"", "127.0.0.1", "127.0.0.1:", ":7777", "127.0.0.1:0", "127.0.0.1:70000", "127.0.0.1:abc"}
	for _, s := range bad {
		if _, err := parseTarget(s); err == nil {
			t.Errorf("target %q should be rejected", s)
		}
	}
}

func TestParseBotCount(t *testing.T) {
	if n, err := parseBotCount("50"); err != nil || n != 50 {
		t.Fatalf("50 -> %d, %v", n, err)
	}
	for _, s := range []string{"0", "-1", "1001", "abc", ""} {
		if _, err := parseBotCount(s); err == nil {
			t.Errorf("bot count %q should be rejected", s)
		}
	}
	if _, err := parseBotCount("1000"); err != nil {
		t.Errorf("1000 should be allowed: %v", err)
	}
}

func TestParseDuration(t *testing.T) {
	if d, err := parseDuration("3600"); err != nil || d.Seconds() != 3600 {
		t.Fatalf("3600 -> %v, %v", d, err)
	}
	for _, s := range []string{"0", "-5", "abc", ""} {
		if _, err := parseDuration(s); err == nil {
			t.Errorf("duration %q should be rejected", s)
		}
	}
}
