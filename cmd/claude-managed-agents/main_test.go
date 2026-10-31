// Copyright 2026 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/lib/environments"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// fakeAPI creates one session, accepts events for it, and streams canned
// events once the prompt has been sent.
type fakeAPI struct {
	t      *testing.T
	events []string

	mu      sync.Mutex
	session map[string]any
	sent    []any
	prompt  chan struct{}
}

func (f *fakeAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if got := r.Header.Get("anthropic-beta"); !strings.Contains(got, "managed-agents-2026-04-01") {
		f.t.Errorf("%s %s: anthropic-beta = %q", r.Method, r.URL.Path, got)
	}
	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/v1/sessions":
		f.mu.Lock()
		json.NewDecoder(r.Body).Decode(&f.session)
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id": "sess_1", "type": "session"}`)
	case r.Method == http.MethodGet && r.URL.Path == "/v1/sessions/sess_1/events/stream":
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		select {
		case <-f.prompt:
		case <-r.Context().Done():
			return
		}
		for _, ev := range f.events {
			var typ struct{ Type string }
			json.Unmarshal([]byte(ev), &typ)
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", typ.Type, strings.Join(strings.Fields(ev), " "))
		}
		w.(http.Flusher).Flush()
	case r.Method == http.MethodPost && r.URL.Path == "/v1/sessions/sess_1/events":
		var body struct{ Events []any }
		json.NewDecoder(r.Body).Decode(&body)
		f.mu.Lock()
		f.sent = append(f.sent, body.Events...)
		f.mu.Unlock()
		close(f.prompt)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"data": []}`)
	default:
		f.t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		http.NotFound(w, r)
	}
}

// runFake runs the agent against a fake API with a worker that records its
// options and waits to be stopped.
func runFake(t *testing.T, config string, events ...string) (*fakeAPI, *environments.EnvironmentWorkerOptions, error) {
	t.Helper()
	ws := t.TempDir()
	t.Setenv("AX_TASK_YAML", "metadata: {name: fix-tests}\nspec: {workspace: {name: w, path: "+ws+"}, config: "+config+"}")
	t.Setenv("AX_WORKSPACE_YAML", "")
	t.Setenv("ANTHROPIC_ENVIRONMENT_KEY", "envkey")
	api := &fakeAPI{t: t, events: events, prompt: make(chan struct{})}
	srv := httptest.NewServer(api)
	t.Cleanup(srv.Close)

	var got environments.EnvironmentWorkerOptions
	serve = func(ctx context.Context, _ anthropic.Client, opts environments.EnvironmentWorkerOptions) error {
		got = opts
		<-ctx.Done()
		return nil
	}
	err := run(context.Background(), option.WithBaseURL(srv.URL), option.WithAPIKey("key"))
	if got.Workdir != "" && got.Workdir != ws {
		t.Errorf("worker workdir = %q, want %q", got.Workdir, ws)
	}
	return api, &got, err
}

func TestRun_Prompt(t *testing.T) {
	api, worker, err := runFake(t, "{environment: env_1, agent: agent_1, prompt: Fix the tests.}",
		`{"id": "e1", "type": "agent.message", "content": [{"type": "text", "text": "Looking."}]}`,
		`{"id": "e2", "type": "agent.tool_use", "name": "bash", "input": {"command": "pytest"}}`,
		`{"id": "e3", "type": "session.status_idle", "stop_reason": {"type": "requires_action", "event_ids": ["e2"]}}`,
		`{"id": "e4", "type": "session.status_idle", "stop_reason": {"type": "end_turn"}}`,
	)
	if err != nil {
		t.Fatal(err)
	}
	if worker.EnvironmentID != "env_1" || worker.EnvironmentKey != "envkey" {
		t.Errorf("worker options = %+v", worker)
	}
	if api.session["agent"] != "agent_1" || api.session["environment_id"] != "env_1" || api.session["title"] != "fix-tests" {
		t.Errorf("session request = %v", api.session)
	}
	if _, ok := api.session["initial_events"]; ok {
		t.Errorf("session request has initial_events; the prompt must be sent after the stream opens")
	}
	if len(api.sent) != 1 {
		t.Fatalf("sent %d events, want 1", len(api.sent))
	}
	msg, _ := json.Marshal(api.sent[0])
	if want := `{"content":[{"text":"Fix the tests.","type":"text"}],"type":"user.message"}`; string(msg) != want {
		t.Errorf("prompt event = %s, want %s", msg, want)
	}
}

func TestRun_AgentVersion(t *testing.T) {
	api, _, err := runFake(t, "{environment: env_1, agent: agent_1, agentVersion: 3, prompt: Hi.}",
		`{"id": "e1", "type": "session.status_idle", "stop_reason": {"type": "end_turn"}}`,
	)
	if err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprint(api.session["agent"]); got != "map[id:agent_1 type:agent version:3]" {
		t.Errorf("agent = %s", got)
	}
}

func TestRun_Serve(t *testing.T) {
	ws := t.TempDir()
	t.Setenv("AX_TASK_YAML", "spec: {workspace: {name: w, path: "+ws+"}, config: {environment: env_1}}")
	t.Setenv("ANTHROPIC_ENVIRONMENT_KEY", "envkey")
	var got environments.EnvironmentWorkerOptions
	serve = func(_ context.Context, _ anthropic.Client, opts environments.EnvironmentWorkerOptions) error {
		got = opts
		return nil
	}
	if err := run(context.Background(), option.WithBaseURL("http://127.0.0.1:1")); err != nil {
		t.Fatal(err)
	}
	if got.EnvironmentID != "env_1" || got.EnvironmentKey != "envkey" || got.Workdir != ws {
		t.Errorf("worker options = %+v", got)
	}
}

func TestRun_Failures(t *testing.T) {
	for _, tc := range []struct {
		name, config string
		events       []string
		want         string
	}{
		{"missing environment", "{agent: a, prompt: Hi.}", nil, "spec.config.environment: required"},
		{"agent without prompt", "{environment: e, agent: a}", nil, "set agent and prompt together"},
		{"prompt without agent", "{environment: e, prompt: Hi.}", nil, "set agent and prompt together"},
		{"version without agent", "{environment: e, agentVersion: 2}", nil, "agentVersion: requires agent"},
		{"stopped", "{environment: e, agent: a, prompt: Hi.}", []string{
			`{"id": "e1", "type": "session.status_idle", "stop_reason": {"type": "retries_exhausted"}}`,
		}, "session sess_1 stopped: retries_exhausted"},
		{"terminated", "{environment: e, agent: a, prompt: Hi.}", []string{
			`{"id": "e1", "type": "session.status_terminated"}`,
		}, "session sess_1: session.status_terminated"},
		{"stream ends", "{environment: e, agent: a, prompt: Hi.}", nil, "event stream ended before the agent finished"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := runFake(t, tc.config, tc.events...)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v, want it to contain %q", err, tc.want)
			}
		})
	}
}

func TestRun_NoEnvironmentKey(t *testing.T) {
	t.Setenv("AX_TASK_YAML", "spec: {config: {environment: env_1}}")
	t.Setenv("ANTHROPIC_ENVIRONMENT_KEY", "")
	if err := run(context.Background()); err == nil || !strings.Contains(err.Error(), "ANTHROPIC_ENVIRONMENT_KEY is not set") {
		t.Errorf("err = %v", err)
	}
}
