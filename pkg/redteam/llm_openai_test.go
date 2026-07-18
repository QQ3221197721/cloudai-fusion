package redteam

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/capability"
)

func TestOpenAICompatClient_CompleteAndParse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"{\"done\":true,\"actions\":[]}"}}]}`))
	}))
	defer srv.Close()

	c := NewOpenAICompatClient(srv.URL, "test-key", "gpt-x")
	if c.Mode() != capability.ModeReal {
		t.Fatal("configured client must report real")
	}
	out, err := c.Complete(context.Background(), "plan please")
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	// The returned content must be usable by the planner parser.
	if actions, err := parsePlan(out); err != nil || len(actions) != 0 {
		t.Fatalf("planner must parse the model output (done): actions=%+v err=%v", actions, err)
	}
}

func TestOpenAICompatClient_ModeSimulatedWhenUnconfigured(t *testing.T) {
	if NewOpenAICompatClient("", "", "").Mode() != capability.ModeSimulated {
		t.Fatal("an unconfigured client must honestly report simulated")
	}
}
