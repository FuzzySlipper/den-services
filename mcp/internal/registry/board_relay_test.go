package registry

import "testing"

func TestBoardRelayOperationsAreCallableButHidden(t *testing.T) {
	registry, err := DefaultRegistry()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"sync_board_github", "set_board_github_visibility"} {
		tool, err := registry.Resolve(name)
		if err != nil {
			t.Fatalf("Resolve(%s): %v", name, err)
		}
		if !tool.Hidden || tool.Backend != "board-relay" {
			t.Fatalf("%s hidden=%v backend=%q", name, tool.Hidden, tool.Backend)
		}
	}
	for _, tool := range registry.Tools() {
		if tool.Name == "sync_board_github" || tool.Name == "set_board_github_visibility" {
			t.Fatalf("internal relay operation %s appeared in model discovery", tool.Name)
		}
	}
}
