package main

import (
	"strings"
	"testing"
	"time"

	devserver "den-services/devserver-broker"
)

func TestNewManagerUsesBuiltInDefaultsWhenConfigPathIsEmpty(t *testing.T) {
	if _, err := newManager(""); err != nil {
		t.Fatalf("newManager(\"\") error = %v", err)
	}
}

func TestFormatSessionPacketExposesLaunchFreshness(t *testing.T) {
	session := devserver.SessionState{
		Project:     "alpha",
		Status:      "stale",
		LocalURL:    "http://127.0.0.1:5173/",
		StatePath:   "/tmp/current.json",
		SessionDir:  "/tmp/session",
		ReuseSource: "restarted_stale",
		StartedAt:   time.Date(2026, time.July, 25, 17, 0, 0, 0, time.UTC),
		PID:         123,
		LaunchFingerprint: devserver.LaunchFingerprint{
			Value:    "launch-value",
			RepoHead: "launch-head",
		},
		CurrentFingerprint: devserver.LaunchFingerprint{Value: "current-value"},
		Stale:              true,
		StaleReason:        "repo HEAD changed",
	}
	packet := formatSessionPacket(session)
	for _, marker := range []string{
		"alpha stale",
		"launch source: restarted_stale",
		"started: 2026-07-25T17:00:00Z",
		"launch fingerprint:  launch-value",
		"current fingerprint: current-value",
		"launch repo HEAD:    launch-head",
		"stale: true (repo HEAD changed)",
	} {
		if !strings.Contains(packet, marker) {
			t.Fatalf("packet missing %q:\n%s", marker, packet)
		}
	}
}
