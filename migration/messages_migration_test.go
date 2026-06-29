package migration

import (
	"strings"
	"testing"
)

func TestMessagesMigrationDiscovered(t *testing.T) {
	migrations, err := Discover(DefaultFS())
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	var found *Migration
	for i := range migrations {
		if migrations[i].Schema == "den_messages" && migrations[i].Version == 1 {
			found = &migrations[i]
			break
		}
	}
	if found == nil {
		t.Fatal("den_messages version 1 migration not discovered")
	}
	for _, want := range []string{
		"create table den_messages.messages",
		"create table den_messages.message_reads",
		"grant select, insert on den_messages.messages to den_messages_app",
		"messages_metadata_packet_kind_idx",
	} {
		if !strings.Contains(found.SQL, want) {
			t.Fatalf("messages migration missing %q", want)
		}
	}
}
