package board

import (
	"strings"
	"testing"
)

func TestStoreSQLSerializesCommentCreationWithPostPurge(t *testing.T) {
	if !strings.Contains(strings.ToLower(lockPostForCommentSQL), "for update") {
		t.Fatal("comment creation must lock the owning post before insert")
	}
	if strings.Contains(strings.ToLower(listCommentsSQL), "with recursive") {
		t.Fatal("direct-child listing must not recursively scan descendant subtrees")
	}
	if !strings.Contains(commentPathSQL, "parent.post_id = $2") {
		t.Fatal("comment paths must remain scoped to the target post")
	}
}
