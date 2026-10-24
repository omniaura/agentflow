package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/omniaura/agentflow/runtime/afagent"
	"github.com/omniaura/agentflow/runtime/afhttp"
)

func serveTriage(t *testing.T, srv *TriagerAgentServer, body string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	srv.Register(mux)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("POST", "/triage", strings.NewReader(body)))
	return w
}

func TestServeTriage(t *testing.T) {
	model, seen := script(t, afagent.Message{Content: `{"category":"account","order_id":null,"urgent":false}`})
	history := []afagent.Message{{Role: afagent.User, Content: "hi"}, {Role: afagent.Assistant, Content: "Hello! How can I help?"}}
	var stored []afagent.Message
	srv := &TriagerAgentServer{
		Agent: &TriagerAgent{Model: model, Tools: &shop{}},
		Prepare: func(r *http.Request) (*Support, []afagent.Message, error) {
			return &Support{Shop: "Acme"}, history, nil
		},
		Done: func(r *http.Request, res TriagerAgentResult) error {
			stored = res.Messages
			return nil
		},
	}
	w := serveTriage(t, srv, `{"message":"I can't log in"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}
	var resp TriagerAgentResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Output.Category != TriageCategoryAccount {
		t.Errorf("response = %+v", resp)
	}
	// The model saw the stored history, then the new message.
	msgs := (*seen)[0].Messages
	if len(msgs) != 3 || msgs[1].Content != "Hello! How can I help?" || msgs[2].Content != "I can't log in" {
		t.Errorf("model saw %+v", msgs)
	}
	if len(stored) != 4 || len(history) != 2 {
		t.Errorf("stored %d messages; history now %d", len(stored), len(history))
	}
}

func TestServeTriageErrors(t *testing.T) {
	failing := afagent.ModelFunc(func(ctx context.Context, req afagent.Request) (afagent.Response, error) {
		return afagent.Response{}, fmt.Errorf("adapter: %w", afagent.ErrRefused)
	})
	broken := afagent.ModelFunc(func(ctx context.Context, req afagent.Request) (afagent.Response, error) {
		return afagent.Response{}, errors.New("connection reset")
	})
	ok := func(r *http.Request) (*Support, []afagent.Message, error) { return &Support{Shop: "Acme"}, nil, nil }
	for _, tt := range []struct {
		name    string
		srv     *TriagerAgentServer
		body    string
		status  int
		message string
	}{
		{"unauthenticated", &TriagerAgentServer{Prepare: func(r *http.Request) (*Support, []afagent.Message, error) {
			return nil, nil, afhttp.Unauthorized(errors.New("no token"))
		}}, `{"message":"x"}`, http.StatusUnauthorized, ""},
		{"no message", &TriagerAgentServer{Prepare: ok}, `{}`, http.StatusBadRequest, "message is required"},
		{"unknown field", &TriagerAgentServer{Prepare: ok}, `{"message":"x","messages":[]}`, http.StatusBadRequest, ""},
		{"too big", &TriagerAgentServer{Prepare: ok, MaxBytes: 8}, `{"message":"a long message"}`, http.StatusRequestEntityTooLarge, ""},
		{"model refused", &TriagerAgentServer{Prepare: ok, Agent: &TriagerAgent{Model: failing, Tools: &shop{}}}, `{"message":"x"}`, http.StatusBadGateway, "the model could not answer"},
		{"model down", &TriagerAgentServer{Prepare: ok, Agent: &TriagerAgent{Model: broken, Tools: &shop{}}}, `{"message":"x"}`, http.StatusInternalServerError, "internal error"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			w := serveTriage(t, tt.srv, tt.body)
			if w.Code != tt.status || !strings.Contains(w.Body.String(), tt.message) {
				t.Fatalf("got %d %s, want %d %q", w.Code, w.Body, tt.status, tt.message)
			}
			if strings.Contains(w.Body.String(), "connection reset") || strings.Contains(w.Body.String(), "no token") {
				t.Errorf("leaked the cause: %s", w.Body)
			}
		})
	}
}
