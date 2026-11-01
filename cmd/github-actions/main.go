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

// Command github-actions runs GitHub Actions jobs in an AX task.
//
// For each job it registers a just-in-time runner with the GitHub API and
// runs GitHub's actions runner with that configuration. A just-in-time runner
// takes exactly one job and then removes itself, so every job starts from a
// fresh registration. The task exits after spec.config.jobs jobs, or keeps
// taking jobs when jobs is 0.
//
// The token comes from GITHUB_TOKEN and needs permission to manage
// self-hosted runners for the repository or organization. It is removed from
// the runner's environment so that workflow steps never see it.
package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/rakyll/ax-images/internal/agent"
	"github.com/rakyll/ax-images/internal/taskrunner"
)

// runScript starts GitHub's actions runner, installed by the Dockerfile.
var runScript = "/opt/actions-runner/run.sh"

func main() {
	if taskrunner.Invoked() {
		taskrunner.Main()
		return
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx); err != nil {
		slog.Error("runner failed", "err", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	cfg, err := agent.Load()
	if err != nil {
		return err
	}
	o, err := parseOptions(cfg)
	if err != nil {
		return err
	}
	gh := &github{
		apiURL: strings.TrimSuffix(cmp(os.Getenv("GITHUB_API_URL"), "https://api.github.com"), "/"),
		token:  os.Getenv("GITHUB_TOKEN"),
		scope:  o.scope,
	}
	if gh.token == "" {
		return errors.New("GITHUB_TOKEN is not set")
	}

	for n := 1; o.jobs == 0 || n <= o.jobs; n++ {
		name := fmt.Sprintf("%s-%s", o.name, suffix())
		jit, err := gh.generateJITConfig(ctx, name, o)
		if err != nil {
			return err
		}
		slog.Info("waiting for a job", "runner", name, "id", jit.Runner.ID, "job", n, "labels", o.labels)
		err = runJob(ctx, jit.EncodedJITConfig)
		if err != nil || ctx.Err() != nil {
			// A runner that never finished a job stays registered, so
			// remove it before giving up.
			gh.deleteRunner(jit.Runner.ID)
		}
		if ctx.Err() != nil {
			slog.Info("stopped", "jobs", n-1)
			return nil
		}
		if err != nil {
			return err
		}
	}
	slog.Info("finished", "jobs", o.jobs)
	return nil
}

// options are the image's spec.config keys.
type options struct {
	// scope is "repos/<owner>/<repo>" or "orgs/<org>".
	scope  string
	name   string
	labels []string
	group  int
	jobs   int
}

func parseOptions(cfg *agent.Config) (*options, error) {
	var (
		o    options
		errs []error
	)
	repo, err := cfg.String("repository")
	errs = append(errs, err)
	org, err := cfg.String("organization")
	errs = append(errs, err)
	switch {
	case (repo == "") == (org == ""):
		errs = append(errs, errors.New("spec.config: set exactly one of repository or organization"))
	case repo != "":
		if owner, name, ok := strings.Cut(repo, "/"); !ok || owner == "" || name == "" || strings.Contains(name, "/") {
			errs = append(errs, fmt.Errorf("spec.config.repository: %q is not <owner>/<repo>", repo))
		}
		o.scope = "repos/" + repo
	default:
		o.scope = "orgs/" + org
	}

	o.name, err = cfg.String("name")
	errs = append(errs, err)
	if o.name == "" {
		o.name = cmp(cfg.TaskName, "ax")
	}
	o.labels, err = cfg.Strings("labels")
	errs = append(errs, err)
	if len(o.labels) == 0 {
		// The labels GitHub gives a self-hosted runner by default.
		o.labels = []string{"self-hosted", "Linux", archLabel()}
	}
	o.group, err = cfg.Int("runnerGroupId", 1)
	errs = append(errs, err)
	o.jobs, err = cfg.Int("jobs", 1)
	errs = append(errs, err)
	if o.jobs < 0 {
		errs = append(errs, errors.New("spec.config.jobs: must not be negative"))
	}
	return &o, errors.Join(errs...)
}

func archLabel() string {
	if runtime.GOARCH == "arm64" {
		return "ARM64"
	}
	return "X64"
}

// runJob runs the actions runner with a just-in-time configuration until it
// has finished its job. Cancelling ctx asks the runner to stop.
func runJob(ctx context.Context, jitConfig string) error {
	cmd := exec.CommandContext(ctx, runScript, "--jitconfig", jitConfig)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = runnerEnv(os.Environ())
	// The runner stops gracefully on SIGINT. Signal the whole process group,
	// since run.sh starts the listener as a child.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGINT) }
	cmd.WaitDelay = 30 * time.Second
	if err := cmd.Run(); err != nil && ctx.Err() == nil {
		return fmt.Errorf("actions runner: %w", err)
	}
	return nil
}

// runnerEnv returns env without the GitHub token, which workflow steps would
// otherwise inherit, and with the runner allowed to run as root.
func runnerEnv(env []string) []string {
	out := []string{"RUNNER_ALLOW_RUNASROOT=1"}
	for _, kv := range env {
		k, _, _ := strings.Cut(kv, "=")
		switch k {
		case "GITHUB_TOKEN", "GH_TOKEN", "RUNNER_ALLOW_RUNASROOT":
			continue
		}
		out = append(out, kv)
	}
	return out
}

// github is the part of the GitHub REST API the runner uses.
type github struct {
	apiURL string
	token  string
	scope  string
}

type jitConfig struct {
	Runner struct {
		ID int64 `json:"id"`
	} `json:"runner"`
	EncodedJITConfig string `json:"encoded_jit_config"`
}

func (g *github) generateJITConfig(ctx context.Context, name string, o *options) (*jitConfig, error) {
	body, err := json.Marshal(map[string]any{
		"name":            name,
		"runner_group_id": o.group,
		"labels":          o.labels,
	})
	if err != nil {
		return nil, err
	}
	var jit jitConfig
	if err := g.do(ctx, http.MethodPost, g.scope+"/actions/runners/generate-jitconfig", body, http.StatusCreated, &jit); err != nil {
		return nil, fmt.Errorf("registering runner: %w", err)
	}
	if jit.EncodedJITConfig == "" {
		return nil, errors.New("registering runner: response has no encoded_jit_config")
	}
	return &jit, nil
}

// deleteRunner removes a runner registration, logging rather than returning
// failures: GitHub also removes offline just-in-time runners on its own.
func (g *github) deleteRunner(id int64) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	err := g.do(ctx, http.MethodDelete, fmt.Sprintf("%s/actions/runners/%d", g.scope, id), nil, http.StatusNoContent, nil)
	if err != nil {
		slog.Warn("removing runner registration", "id", id, "err", err)
	}
}

func (g *github) do(ctx context.Context, method, path string, body []byte, want int, out any) error {
	req, err := http.NewRequestWithContext(ctx, method, g.apiURL+"/"+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+g.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode != want {
		var msg struct {
			Message string `json:"message"`
		}
		json.Unmarshal(data, &msg)
		return fmt.Errorf("%s %s: %s: %s", method, path, resp.Status, cmp(msg.Message, strings.TrimSpace(string(data))))
	}
	if out != nil {
		return json.Unmarshal(data, out)
	}
	return nil
}

func suffix() string {
	b := make([]byte, 4)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// cmp returns the first non-empty string.
func cmp(s ...string) string {
	for _, v := range s {
		if v != "" {
			return v
		}
	}
	return ""
}
