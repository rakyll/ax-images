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

package agent

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const testTask = `
metadata: {name: fix}
spec:
  config:
    prompt: Fix it.
    model: m
    maxTurns: 7
    builtinTools: [a, b]
    params: {x: 1}
  workspace: {name: repo, path: /tmp/ws}
`

const testWorkspace = `
metadata: {name: repo}
spec:
  mcp:
    servers:
      - {name: docs, endpoint: "http://docs/mcp"}
      - {name: git, command: git-mcp, args: [--repo, /workspace]}
      - {name: docs, endpoint: "http://dup/mcp"}
      - {name: empty}
    registries:
      - servers:
          - {name: search, endpoint: "http://search/mcp"}
`

func TestLoad(t *testing.T) {
	t.Setenv("AX_TASK_YAML", testTask)
	t.Setenv("AX_WORKSPACE_YAML", testWorkspace)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.TaskName != "fix" || cfg.Workspace != "/tmp/ws" {
		t.Errorf("TaskName, Workspace = %q, %q", cfg.TaskName, cfg.Workspace)
	}
	wantMCP := []MCPServer{
		{Name: "docs", URL: "http://docs/mcp"},
		{Name: "git", Command: "git-mcp", Args: []string{"--repo", "/workspace"}},
		{Name: "search", URL: "http://search/mcp"},
	}
	if !reflect.DeepEqual(cfg.MCP, wantMCP) {
		t.Errorf("MCP = %+v, want %+v", cfg.MCP, wantMCP)
	}

	if s, err := cfg.Required("prompt"); s != "Fix it." || err != nil {
		t.Errorf("Required(prompt) = %q, %v", s, err)
	}
	if _, err := cfg.Required("missing"); err == nil {
		t.Error("Required(missing) succeeded")
	}
	if n, err := cfg.Int("maxTurns", 50); n != 7 || err != nil {
		t.Errorf("Int(maxTurns) = %d, %v", n, err)
	}
	if n, err := cfg.Int("unset", 50); n != 50 || err != nil {
		t.Errorf("Int(unset) = %d, %v", n, err)
	}
	if _, err := cfg.Int("model", 50); err == nil {
		t.Error("Int(model) succeeded")
	}
	if l, err := cfg.Strings("builtinTools"); !reflect.DeepEqual(l, []string{"a", "b"}) || err != nil {
		t.Errorf("Strings(builtinTools) = %q, %v", l, err)
	}
	if m, err := cfg.Object("params"); m["x"] != 1.0 || err != nil {
		t.Errorf("Object(params) = %v, %v", m, err)
	}
}

func TestLoad_NoWorkspace(t *testing.T) {
	t.Setenv("AX_TASK_YAML", "spec: {config: {prompt: hi}}")
	t.Setenv("AX_WORKSPACE_YAML", "")
	dir := t.TempDir()
	t.Chdir(dir)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := filepath.EvalSymlinks(cfg.Workspace); got != mustEval(t, dir) {
		t.Errorf("Workspace = %q, want %q", cfg.Workspace, dir)
	}
	if len(cfg.MCP) != 0 {
		t.Errorf("MCP = %v, want none", cfg.MCP)
	}
}

func mustEval(t *testing.T, p string) string {
	t.Helper()
	p, err := filepath.EvalSymlinks(p)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestTools(t *testing.T) {
	ctx := context.Background()
	ws := t.TempDir()
	call := func(name string, args map[string]any) string { return CallTool(ctx, ws, name, args) }

	if got := call("write_file", map[string]any{"path": "a/b.txt", "content": "hello world"}); !strings.HasPrefix(got, "wrote") {
		t.Errorf("write_file = %q", got)
	}
	if got := call("edit_file", map[string]any{"path": "a/b.txt", "old_text": "world", "new_text": "ax"}); !strings.HasPrefix(got, "edited") {
		t.Errorf("edit_file = %q", got)
	}
	if got := call("read_file", map[string]any{"path": "a/b.txt"}); got != "hello ax" {
		t.Errorf("read_file = %q", got)
	}
	if got := call("run_shell", map[string]any{"command": "cat a/b.txt; exit 3"}); !strings.Contains(got, "hello ax") || !strings.Contains(got, "[exit code 3]") {
		t.Errorf("run_shell = %q", got)
	}
}

func TestTools_Errors(t *testing.T) {
	ctx := context.Background()
	ws := t.TempDir()
	if err := os.WriteFile(filepath.Join(ws, "f"), []byte("x x"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		args map[string]any
		want string
	}{
		{"read_file", map[string]any{"path": "nope"}, "error:"},
		{"nope", map[string]any{}, "error: unknown tool"},
		{"read_file", map[string]any{}, "error: missing argument"},
		{"read_file", map[string]any{"path": 1}, "error: argument"},
		{"edit_file", map[string]any{"path": "f", "old_text": "x", "new_text": "y"}, "found 2 matches"},
	} {
		if got := CallTool(ctx, ws, tc.name, tc.args); !strings.Contains(got, tc.want) {
			t.Errorf("%s(%v) = %q, want it to contain %q", tc.name, tc.args, got, tc.want)
		}
	}
}

func TestRunShell_Timeout(t *testing.T) {
	// The background sleep holds stdout open; the timeout must still return.
	got := CallTool(context.Background(), t.TempDir(), "run_shell", map[string]any{"command": "echo start; sleep 30 & sleep 30", "timeout": 1.0})
	if !strings.Contains(got, "start") || !strings.Contains(got, "timed out") {
		t.Errorf("run_shell = %q", got)
	}
}

func TestTruncate(t *testing.T) {
	s := truncate(strings.Repeat("x", MaxOutput*2))
	if len(s) > MaxOutput+100 || !strings.Contains(s, "truncated") {
		t.Errorf("truncate returned %d characters", len(s))
	}
}
