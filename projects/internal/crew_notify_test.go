package projects

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHTTPCrewNotifyRoundTripsThroughGetProject(t *testing.T) {
	server := testServer(t)
	call := func(method, path, body string) (int, map[string]any) {
		t.Helper()
		response := httptest.NewRecorder()
		server.Handler.ServeHTTP(response, jsonRequest(method, path, body))
		var decoded map[string]any
		_ = json.Unmarshal(response.Body.Bytes(), &decoded)
		return response.Code, decoded
	}
	if code, _ := call(http.MethodPost, "/v1/projects", `{"id":"rusty-dagger","name":"Rusty Dagger"}`); code != http.StatusCreated {
		t.Fatalf("create = %d", code)
	}
	if _, project := call(http.MethodGet, "/v1/projects/rusty-dagger", ""); project["crew_notify"] != nil {
		t.Fatalf("unset crew_notify is not omitted: %v", project["crew_notify"])
	}

	// An agent adds its own seat in one call; the policy defaults to no_wake.
	code, _ := call(http.MethodPost, "/v1/projects/rusty-dagger/crew-notify", `{"add":[{"address":"rusty-dagger.lead"}]}`)
	if code != http.StatusOK {
		t.Fatalf("add = %d", code)
	}
	call(http.MethodPost, "/v1/projects/rusty-dagger/crew-notify", `{"event":"on_done","add":[{"address":"rusty-dagger.deploy","policy":"next_turn"},{"address":"s/5b693970244d98db"}]}`)
	// Adding an address again replaces its policy instead of duplicating it.
	call(http.MethodPost, "/v1/projects/rusty-dagger/crew-notify", `{"add":[{"address":"rusty-dagger.lead","policy":"next_turn"}],"remove":["s/5b693970244d98db"]}`)
	_, project := call(http.MethodGet, "/v1/projects/rusty-dagger", "")
	got, _ := json.Marshal(project["crew_notify"])
	want := `{"on_done":[{"address":"rusty-dagger.lead","policy":"next_turn"},{"address":"rusty-dagger.deploy","policy":"next_turn"}]}`
	if string(got) != want {
		t.Fatalf("crew_notify = %s, want %s", got, want)
	}

	for _, body := range []string{
		`{"add":[{"address":"Rusty Dagger"}]}`,
		`{"add":[{"address":"rusty-dagger.lead","policy":"next_step"}]}`,
		`{"event":"on_blocked","add":[{"address":"rusty-dagger.lead"}]}`,
		`{}`,
	} {
		if code, _ := call(http.MethodPost, "/v1/projects/rusty-dagger/crew-notify", body); code != http.StatusBadRequest {
			t.Fatalf("%s = %d, want 400", body, code)
		}
	}
	targets := make([]string, 0, MaxCrewNotifyTargets)
	for index := range MaxCrewNotifyTargets {
		targets = append(targets, fmt.Sprintf(`{"address":"rusty-dagger.seat-%d"}`, index))
	}
	if code, _ := call(http.MethodPost, "/v1/projects/rusty-dagger/crew-notify", `{"add":[`+strings.Join(targets, ",")+`]}`); code != http.StatusBadRequest {
		t.Fatalf("over the bound = %d, want 400", code)
	}
	if code, _ := call(http.MethodPost, "/v1/projects/missing/crew-notify", `{"add":[{"address":"missing.lead"}]}`); code != http.StatusNotFound {
		t.Fatalf("missing project = %d, want 404", code)
	}

	// Removing every address clears the setting, and reads omit it again.
	call(http.MethodPost, "/v1/projects/rusty-dagger/crew-notify", `{"remove":["rusty-dagger.lead","rusty-dagger.deploy"]}`)
	if _, project := call(http.MethodGet, "/v1/projects/rusty-dagger", ""); project["crew_notify"] != nil {
		t.Fatalf("emptied crew_notify is not omitted: %v", project["crew_notify"])
	}
}
