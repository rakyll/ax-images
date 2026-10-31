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
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// MaxOutput is the most characters a tool returns. Longer output keeps its
// head and tail so one noisy command cannot fill the context window.
const MaxOutput = 30_000

// defaultTimeout bounds a run_shell command that sets no timeout.
const defaultTimeout = 300 * time.Second

// Tool is a local tool the model can call. The sandbox is the isolation
// boundary, so tools run without approval. A failure is reported in the
// returned text rather than as an error, so the model can react to it.
type Tool struct {
	Name        string
	Description string
	// Parameters is the JSON Schema of the arguments object.
	Parameters map[string]any
	run        func(ctx context.Context, workspace string, args map[string]any) string
}

// Tools are the tools every agent gives its model.
var Tools = []Tool{
	{
		Name:        "run_shell",
		Description: "Run a bash command in the workspace and return its combined stdout, stderr, and exit code.",
		Parameters: object(map[string]any{
			"command": str("The bash command to run."),
			"timeout": map[string]any{"type": "integer", "description": "Timeout in seconds. Defaults to 300."},
		}, "command"),
		run: runShell,
	},
	{
		Name:        "read_file",
		Description: "Read a text file. Relative paths resolve against the workspace.",
		Parameters:  object(map[string]any{"path": str("Path of the file to read.")}, "path"),
		run:         readFile,
	},
	{
		Name:        "write_file",
		Description: "Create or overwrite a text file with the given content.",
		Parameters: object(map[string]any{
			"path":    str("Path of the file to write."),
			"content": str("The full new content of the file."),
		}, "path", "content"),
		run: writeFile,
	},
	{
		Name:        "edit_file",
		Description: "Replace old_text, which must appear exactly once in the file, with new_text.",
		Parameters: object(map[string]any{
			"path":     str("Path of the file to edit."),
			"old_text": str("The exact text to replace."),
			"new_text": str("The replacement text."),
		}, "path", "old_text", "new_text"),
		run: editFile,
	},
}

// CallTool runs the tool called name with arguments and returns its output.
func CallTool(ctx context.Context, workspace, name string, args map[string]any) string {
	for _, t := range Tools {
		if t.Name == name {
			return t.run(ctx, workspace, args)
		}
	}
	return fmt.Sprintf("error: unknown tool %q", name)
}

// CallToolJSON is CallTool with the arguments encoded as a JSON object.
func CallToolJSON(ctx context.Context, workspace, name, args string) string {
	m := map[string]any{}
	if strings.TrimSpace(args) != "" {
		if err := json.Unmarshal([]byte(args), &m); err != nil {
			return fmt.Sprintf("error: arguments for %s are not a JSON object: %v", name, err)
		}
	}
	return CallTool(ctx, workspace, name, m)
}

func object(props map[string]any, required ...string) map[string]any {
	return map[string]any{"type": "object", "properties": props, "required": required}
}

func str(description string) map[string]any {
	return map[string]any{"type": "string", "description": description}
}

// stringArg returns the string argument key. Missing optional arguments are
// "", and a non-string value is an error.
func stringArg(args map[string]any, key string, required bool) (string, error) {
	v, ok := args[key]
	if !ok {
		if required {
			return "", fmt.Errorf("missing argument %q", key)
		}
		return "", nil
	}
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("argument %q must be a string", key)
	}
	return s, nil
}

func truncate(s string) string {
	if len(s) <= MaxOutput {
		return s
	}
	half := MaxOutput / 2
	return fmt.Sprintf("%s\n... [%d characters truncated] ...\n%s", s[:half], len(s)-MaxOutput, s[len(s)-half:])
}

func resolve(workspace, path string) string {
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			path = filepath.Join(home, path[2:])
		}
	}
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Join(workspace, path)
}

func runShell(ctx context.Context, workspace string, args map[string]any) string {
	command, err := stringArg(args, "command", true)
	if err != nil {
		return "error: " + err.Error()
	}
	timeout := defaultTimeout
	if v, ok := args["timeout"].(float64); ok && v > 0 {
		timeout = time.Duration(v * float64(time.Second))
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "bash", "-lc", command)
	cmd.Dir = workspace
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	// Kill the whole process group on timeout, not just bash, and stop
	// waiting for pipes held open by background children.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 5 * time.Second

	err = cmd.Run()
	var exit *exec.ExitError
	switch {
	case ctx.Err() == context.DeadlineExceeded:
		return truncate(fmt.Sprintf("%s\n[timed out after %s]", out.String(), timeout))
	case errors.As(err, &exit):
		return truncate(fmt.Sprintf("%s\n[exit code %d]", out.String(), exit.ExitCode()))
	case err != nil:
		return truncate(fmt.Sprintf("%s\nerror: %v", out.String(), err))
	}
	return truncate(out.String() + "\n[exit code 0]")
}

func readFile(_ context.Context, workspace string, args map[string]any) string {
	path, err := stringArg(args, "path", true)
	if err != nil {
		return "error: " + err.Error()
	}
	data, err := os.ReadFile(resolve(workspace, path))
	if err != nil {
		return "error: " + err.Error()
	}
	return truncate(string(data))
}

func writeFile(_ context.Context, workspace string, args map[string]any) string {
	path, err := stringArg(args, "path", true)
	if err != nil {
		return "error: " + err.Error()
	}
	content, err := stringArg(args, "content", true)
	if err != nil {
		return "error: " + err.Error()
	}
	p := resolve(workspace, path)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return "error: " + err.Error()
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		return "error: " + err.Error()
	}
	return fmt.Sprintf("wrote %d bytes to %s", len(content), p)
}

func editFile(_ context.Context, workspace string, args map[string]any) string {
	var vals [3]string
	for i, key := range []string{"path", "old_text", "new_text"} {
		s, err := stringArg(args, key, true)
		if err != nil {
			return "error: " + err.Error()
		}
		vals[i] = s
	}
	p, oldText, newText := resolve(workspace, vals[0]), vals[1], vals[2]
	data, err := os.ReadFile(p)
	if err != nil {
		return "error: " + err.Error()
	}
	if n := strings.Count(string(data), oldText); n != 1 || oldText == "" {
		return fmt.Sprintf("error: old_text must appear exactly once in %s, found %d matches", p, n)
	}
	info, err := os.Stat(p)
	if err != nil {
		return "error: " + err.Error()
	}
	if err := os.WriteFile(p, []byte(strings.Replace(string(data), oldText, newText, 1)), info.Mode().Perm()); err != nil {
		return "error: " + err.Error()
	}
	return "edited " + p
}
