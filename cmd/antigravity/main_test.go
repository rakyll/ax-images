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
	"os"
	"path/filepath"
	"strings"
	"testing"

	"google.golang.org/genai"
)

// fakeAPI serves canned interactions in order and records the requests.
type fakeAPI struct {
	t         *testing.T
	responses []string
	requests  []map[string]any
}

func (f *fakeAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/interactions") {
		f.t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		http.NotFound(w, r)
		return
	}
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		f.t.Errorf("decoding request: %v", err)
	}
	f.requests = append(f.requests, body)
	if len(f.requests) > len(f.responses) {
		f.t.Errorf("unexpected request %d", len(f.requests))
		http.Error(w, "no more responses", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(f.responses[len(f.requests)-1]))
}

func runFake(t *testing.T, config string, responses ...string) (*fakeAPI, string, error) {
	t.Helper()
	ws := t.TempDir()
	t.Setenv("AX_TASK_YAML", "spec: {workspace: {name: w, path: "+ws+"}, config: "+config+"}")
	t.Setenv("AX_WORKSPACE_YAML", "spec: {mcp: {servers: [{name: docs, endpoint: 'http://docs/mcp'}, {name: local, command: x}]}}")
	api := &fakeAPI{t: t, responses: responses}
	srv := httptest.NewServer(api)
	t.Cleanup(srv.Close)
	err := run(context.Background(), &genai.ClientConfig{APIKey: "test", HTTPOptions: genai.HTTPOptions{BaseURL: srv.URL}})
	return api, ws, err
}

func TestRun(t *testing.T) {
	api, ws, err := runFake(t,
		"{prompt: Do it., model: gemini-4-argon, maxTotalTokens: 50000, systemInstruction: Be brief., builtinTools: [google_search]}",
		`{"id": "i1", "status": "requires_action", "steps": [
			{"type": "model_output", "content": [{"type": "text", "text": "Writing."}]},
			{"type": "function_call", "id": "c1", "name": "write_file", "arguments": {"path": "out.txt", "content": "hi"}},
			{"type": "function_call", "id": "c2", "name": "run_shell", "arguments": {"command": "echo hosted > hosted.txt"}},
			{"type": "function_result", "call_id": "c2", "result": "done"}
		]}`,
		`{"id": "i2", "status": "completed", "steps": [
			{"type": "model_output", "content": [{"type": "text", "text": "Done."}]}
		]}`,
	)
	if err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(filepath.Join(ws, "out.txt")); string(data) != "hi" {
		t.Errorf("out.txt = %q, %v", data, err)
	}
	// c2 already has a result, so it is not run again in the workspace.
	if _, err := os.Stat(filepath.Join(ws, "hosted.txt")); !os.IsNotExist(err) {
		t.Errorf("answered call c2 ran locally: %v", err)
	}
	if len(api.requests) != 2 {
		t.Fatalf("got %d requests, want 2", len(api.requests))
	}

	first := api.requests[0]
	if first["agent"] != defaultAgent || first["input"] != "Do it." || first["system_instruction"] != "Be brief." {
		t.Errorf("first request = %v", first)
	}
	for _, key := range []string{"model", "environment", "previous_interaction_id"} {
		if v, ok := first[key]; ok {
			t.Errorf("first request sets %s = %v", key, v)
		}
	}
	ac, _ := first["agent_config"].(map[string]any)
	if ac["type"] != "antigravity" || ac["model"] != "gemini-4-argon" || fmt.Sprint(ac["max_total_tokens"]) != "50000" {
		t.Errorf("agent_config = %v", first["agent_config"])
	}
	var types []string
	for _, tool := range first["tools"].([]any) {
		types = append(types, tool.(map[string]any)["type"].(string))
	}
	// Four local tools, the HTTP MCP server, and google_search; the stdio
	// server is skipped and code_execution is not offered.
	if got, want := strings.Join(types, ","), "function,function,function,function,mcp_server,google_search"; got != want {
		t.Errorf("tool types = %s, want %s", got, want)
	}

	second := api.requests[1]
	if second["previous_interaction_id"] != "i1" || second["agent_config"] == nil || len(second["tools"].([]any)) != 6 {
		t.Errorf("second request = %v", second)
	}
	input, _ := second["input"].([]any)
	if len(input) != 1 {
		t.Fatalf("second input = %v", second["input"])
	}
	result := input[0].(map[string]any)
	if result["type"] != "function_result" || result["call_id"] != "c1" || !strings.HasPrefix(result["result"].(string), "wrote 2 bytes") {
		t.Errorf("function result = %v", result)
	}
}

func TestRun_Defaults(t *testing.T) {
	api, _, err := runFake(t, "{prompt: Hi., agent: antigravity-preview-12-2026}", `{"id": "i1", "status": "completed", "steps": []}`)
	if err != nil {
		t.Fatal(err)
	}
	req := api.requests[0]
	if req["agent"] != "antigravity-preview-12-2026" {
		t.Errorf("agent = %v", req["agent"])
	}
	if ac, _ := req["agent_config"].(map[string]any); len(ac) != 1 || ac["type"] != "antigravity" {
		t.Errorf("agent_config = %v", req["agent_config"])
	}
	if v, ok := req["system_instruction"]; ok {
		t.Errorf("request sets system_instruction = %v", v)
	}
}

func TestRun_Failures(t *testing.T) {
	call := `{"id": "i", "status": "requires_action", "steps": [{"type": "function_call", "id": "%s", "name": "run_shell", "arguments": {"command": "true"}}]}`
	for _, tc := range []struct {
		name, config string
		responses    []string
		want         string
	}{
		{"missing prompt", "{model: m}", nil, "spec.config.prompt: required"},
		{"code execution", "{prompt: Hi., builtinTools: [code_execution]}", nil, "code_execution runs outside the workspace"},
		{"unknown builtin", "{prompt: Hi., builtinTools: [nope]}", nil, `unknown tool "nope"`},
		{"negative budget", "{prompt: Hi., maxTotalTokens: -1}", nil, "maxTotalTokens: must not be negative"},
		{"failed", "{prompt: Hi.}", []string{`{"id": "i", "status": "failed", "errors": [{"message": "boom"}]}`}, "interaction failed: boom"},
		{"incomplete", "{prompt: Hi.}", []string{`{"id": "i", "status": "incomplete"}`}, "interaction incomplete"},
		{"stuck", "{prompt: Hi.}", []string{`{"id": "i", "status": "requires_action", "steps": []}`}, "no function calls to run"},
		{"max turns", "{prompt: Hi., maxTurns: 2}", []string{
			strings.Replace(call, "%s", "a", 1), strings.Replace(call, "%s", "b", 1),
		}, "reached maxTurns (2)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := runFake(t, tc.config, tc.responses...)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v, want it to contain %q", err, tc.want)
			}
		})
	}
}
