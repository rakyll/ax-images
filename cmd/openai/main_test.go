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
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/openai/openai-go/v3/option"
)

// fakeAPI serves canned responses in order and records the requests.
type fakeAPI struct {
	t         *testing.T
	responses []string
	requests  []map[string]any
}

func (f *fakeAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || r.URL.Path != "/responses" {
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
		http.Error(w, "no more responses", http.StatusBadRequest)
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
	err := run(context.Background(), option.WithBaseURL(srv.URL), option.WithAPIKey("test"), option.WithMaxRetries(0))
	return api, ws, err
}

func TestRun(t *testing.T) {
	api, ws, err := runFake(t,
		"{prompt: Do it., model: m, instructions: Be brief., builtinTools: [web_search], parameters: {reasoning: {effort: high}}}",
		`{"id": "r1", "object": "response", "status": "completed", "output": [
			{"type": "message", "id": "m1", "role": "assistant", "status": "completed", "content": [{"type": "output_text", "text": "Writing.", "annotations": []}]},
			{"type": "function_call", "id": "f1", "call_id": "c1", "name": "write_file", "arguments": "{\"path\": \"out.txt\", \"content\": \"hi\"}", "status": "completed"}
		]}`,
		`{"id": "r2", "object": "response", "status": "completed", "output": [
			{"type": "message", "id": "m2", "role": "assistant", "status": "completed", "content": [{"type": "output_text", "text": "Done.", "annotations": []}]}
		]}`,
	)
	if err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(filepath.Join(ws, "out.txt")); string(data) != "hi" {
		t.Errorf("out.txt = %q, %v", data, err)
	}
	if len(api.requests) != 2 {
		t.Fatalf("got %d requests, want 2", len(api.requests))
	}

	first := api.requests[0]
	if first["model"] != "m" || first["input"] != "Do it." || first["instructions"] != "Be brief." {
		t.Errorf("first request = %v", first)
	}
	if _, ok := first["previous_response_id"]; ok {
		t.Errorf("first request has previous_response_id")
	}
	if r, _ := first["reasoning"].(map[string]any); r["effort"] != "high" {
		t.Errorf("reasoning = %v", first["reasoning"])
	}
	var types []string
	for _, tool := range first["tools"].([]any) {
		tool := tool.(map[string]any)
		types = append(types, tool["type"].(string))
		if tool["type"] == "mcp" && (tool["server_url"] != "http://docs/mcp" || tool["require_approval"] != "never") {
			t.Errorf("mcp tool = %v", tool)
		}
	}
	// Four local tools, the HTTP MCP server, and web_search; the stdio
	// server is skipped.
	if got, want := strings.Join(types, ","), "function,function,function,function,mcp,web_search"; got != want {
		t.Errorf("tool types = %s, want %s", got, want)
	}

	second := api.requests[1]
	if second["previous_response_id"] != "r1" || second["instructions"] != "Be brief." || len(second["tools"].([]any)) != 6 {
		t.Errorf("second request = %v", second)
	}
	input, _ := second["input"].([]any)
	if len(input) != 1 {
		t.Fatalf("second input = %v", second["input"])
	}
	out := input[0].(map[string]any)
	if out["type"] != "function_call_output" || out["call_id"] != "c1" || !strings.HasPrefix(out["output"].(string), "wrote 2 bytes") {
		t.Errorf("function call output = %v", out)
	}
}

func TestRun_NoBuiltinInstructions(t *testing.T) {
	api, _, err := runFake(t, "{prompt: Hi., model: m}", `{"id": "r1", "object": "response", "status": "completed", "output": []}`)
	if err != nil {
		t.Fatal(err)
	}
	if v, ok := api.requests[0]["instructions"]; ok {
		t.Errorf("request sets instructions = %v", v)
	}
}

func TestRun_Failures(t *testing.T) {
	call := `{"id": "r", "object": "response", "status": "completed", "output": [{"type": "function_call", "id": "f", "call_id": "c", "name": "run_shell", "arguments": "{\"command\": \"true\"}"}]}`
	for _, tc := range []struct {
		name, config string
		responses    []string
		want         string
	}{
		{"missing model", "{prompt: Hi.}", nil, "spec.config.model: required"},
		{"missing prompt", "{model: m}", nil, "spec.config.prompt: required"},
		{"unknown builtin", "{prompt: Hi., model: m, builtinTools: [nope]}", nil, `unknown tool "nope"`},
		{"reserved parameter", "{prompt: Hi., model: m, parameters: {tools: []}}", nil, "parameters.tools: set by the agent"},
		{"failed", "{prompt: Hi., model: m}", []string{`{"id": "r", "object": "response", "status": "failed", "error": {"code": "server_error", "message": "boom"}, "output": []}`}, "response failed: boom"},
		{"incomplete", "{prompt: Hi., model: m}", []string{`{"id": "r", "object": "response", "status": "incomplete", "incomplete_details": {"reason": "max_output_tokens"}, "output": []}`}, "incomplete: max_output_tokens"},
		{"max turns", "{prompt: Hi., model: m, maxTurns: 2}", []string{call, call}, "reached maxTurns (2)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := runFake(t, tc.config, tc.responses...)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v, want it to contain %q", err, tc.want)
			}
		})
	}
}
