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
	"sync"
	"testing"
)

// fakeGitHub hands out just-in-time configs and records runner deletions.
type fakeGitHub struct {
	t        *testing.T
	mu       sync.Mutex
	requests []map[string]any
	paths    []string
	deleted  []string
}

func (f *fakeGitHub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.Header.Get("Authorization") != "Bearer tok" {
		http.Error(w, `{"message": "Bad credentials"}`, http.StatusUnauthorized)
		return
	}
	switch {
	case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/actions/runners/generate-jitconfig"):
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			f.t.Errorf("decoding request: %v", err)
		}
		f.requests = append(f.requests, body)
		f.paths = append(f.paths, r.URL.Path)
		w.WriteHeader(http.StatusCreated)
		fmt.Fprintf(w, `{"runner": {"id": %d}, "encoded_jit_config": "jit-%d"}`, len(f.requests), len(f.requests))
	case r.Method == http.MethodDelete:
		f.deleted = append(f.deleted, r.URL.Path)
		w.WriteHeader(http.StatusNoContent)
	default:
		f.t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		http.NotFound(w, r)
	}
}

// runFake runs the runner against a fake GitHub API, with run.sh replaced by
// script. The script's arguments are appended to args.log in a temp dir.
func runFake(t *testing.T, config, script string) (*fakeGitHub, string, error) {
	t.Helper()
	dir := t.TempDir()
	runScript = filepath.Join(dir, "run.sh")
	if err := os.WriteFile(runScript, []byte("#!/bin/bash\necho \"$@\" >> "+dir+"/args.log\n"+script), 0o755); err != nil {
		t.Fatal(err)
	}
	gh := &fakeGitHub{t: t}
	srv := httptest.NewServer(gh)
	t.Cleanup(srv.Close)
	t.Setenv("GITHUB_API_URL", srv.URL+"/")
	t.Setenv("GITHUB_TOKEN", "tok")
	t.Setenv("AX_TASK_YAML", "metadata: {name: ci}\nspec: {config: "+config+"}")
	t.Setenv("AX_WORKSPACE_YAML", "")
	err := run(context.Background())
	log, _ := os.ReadFile(filepath.Join(dir, "args.log"))
	return gh, string(log), err
}

func TestRun(t *testing.T) {
	gh, log, err := runFake(t, "{repository: o/r, jobs: 2, labels: [ax, gpu]}", `
[ -z "$GITHUB_TOKEN" ] || { echo token leaked; exit 1; }
[ "$RUNNER_ALLOW_RUNASROOT" = 1 ] || exit 1
`)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := log, "--jitconfig jit-1\n--jitconfig jit-2\n"; got != want {
		t.Errorf("run.sh calls = %q, want %q", got, want)
	}
	if len(gh.requests) != 2 {
		t.Fatalf("got %d registrations, want 2", len(gh.requests))
	}
	if gh.paths[0] != "/repos/o/r/actions/runners/generate-jitconfig" {
		t.Errorf("path = %s", gh.paths[0])
	}
	req := gh.requests[0]
	if name, _ := req["name"].(string); !strings.HasPrefix(name, "ci-") {
		t.Errorf("name = %v, want ci-<suffix>", req["name"])
	}
	if req["runner_group_id"] != 1.0 || fmt.Sprint(req["labels"]) != "[ax gpu]" {
		t.Errorf("request = %v", req)
	}
	if req["name"] == gh.requests[1]["name"] {
		t.Errorf("both runners are named %v", req["name"])
	}
	if len(gh.deleted) != 0 {
		t.Errorf("deleted %v after successful jobs", gh.deleted)
	}
}

func TestRun_Organization(t *testing.T) {
	gh, _, err := runFake(t, "{organization: acme, name: build, runnerGroupId: 3}", "")
	if err != nil {
		t.Fatal(err)
	}
	if gh.paths[0] != "/orgs/acme/actions/runners/generate-jitconfig" {
		t.Errorf("path = %s", gh.paths[0])
	}
	req := gh.requests[0]
	if name, _ := req["name"].(string); !strings.HasPrefix(name, "build-") || req["runner_group_id"] != 3.0 {
		t.Errorf("request = %v", req)
	}
	if labels := fmt.Sprint(req["labels"]); !strings.HasPrefix(labels, "[self-hosted Linux ") {
		t.Errorf("default labels = %s", labels)
	}
}

func TestRun_RunnerFails(t *testing.T) {
	gh, _, err := runFake(t, "{repository: o/r, jobs: 3}", "exit 2")
	if err == nil || !strings.Contains(err.Error(), "exit status 2") {
		t.Fatalf("err = %v, want exit status 2", err)
	}
	if fmt.Sprint(gh.deleted) != "[/repos/o/r/actions/runners/1]" {
		t.Errorf("deleted = %v", gh.deleted)
	}
}

func TestRun_ConfigErrors(t *testing.T) {
	for _, tc := range []struct{ config, want string }{
		{"{}", "set exactly one of repository or organization"},
		{"{repository: o/r, organization: acme}", "set exactly one of repository or organization"},
		{"{repository: nope}", `"nope" is not <owner>/<repo>`},
		{"{repository: o/r, jobs: -1}", "spec.config.jobs: must not be negative"},
	} {
		t.Run(tc.config, func(t *testing.T) {
			_, _, err := runFake(t, tc.config, "")
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v, want it to contain %q", err, tc.want)
			}
		})
	}
}

func TestRun_BadToken(t *testing.T) {
	dir := t.TempDir()
	runScript = filepath.Join(dir, "run.sh")
	srv := httptest.NewServer(&fakeGitHub{t: t})
	defer srv.Close()
	t.Setenv("GITHUB_API_URL", srv.URL)
	t.Setenv("GITHUB_TOKEN", "wrong")
	t.Setenv("AX_TASK_YAML", "spec: {config: {repository: o/r}}")
	err := run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "401 Unauthorized: Bad credentials") {
		t.Errorf("err = %v", err)
	}
}

func TestRunnerEnv(t *testing.T) {
	got := runnerEnv([]string{"PATH=/bin", "GITHUB_TOKEN=x", "GH_TOKEN=y", "RUNNER_ALLOW_RUNASROOT=0", "HOME=/root"})
	if want := "[RUNNER_ALLOW_RUNASROOT=1 PATH=/bin HOME=/root]"; fmt.Sprint(got) != want {
		t.Errorf("runnerEnv = %v, want %s", got, want)
	}
}
