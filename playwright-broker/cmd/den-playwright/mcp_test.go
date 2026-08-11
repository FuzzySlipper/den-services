package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestPlaytestMCPToolsAdvertisePermissiveEscapeHatches(t *testing.T) {
	tools := playtestTools()
	data, err := json.Marshal(tools)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	text := string(data)
	for _, expected := range []string{"playtest_start", "playtest_observe", "playtest_act", "playtest_inspect", "playtest_finish", "eval", "raw CDP", `"additionalProperties":true`} {
		if !strings.Contains(text, expected) {
			t.Fatalf("tools do not contain %q: %s", expected, text)
		}
	}
	for _, forbidden := range []string{"allowlist", "permission denied", "forbidden operation", "unauthorized"} {
		if strings.Contains(strings.ToLower(text), forbidden) {
			t.Fatalf("tools contain restrictive phrase %q: %s", forbidden, text)
		}
	}
}
