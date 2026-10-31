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

// Package agent holds what the agents in this repository share: the task's
// spec.config, its workspace, the Workspace's MCP servers, and a small set of
// local tools.
//
// Every image installs an agent at /usr/local/bin/ax-agent. The task runner
// (internal/taskrunner, compiled into the same binary) starts it when a task
// leaves spec.command empty, and passes its environment through, so the agent
// reads the same AX_TASK_YAML and AX_WORKSPACE_YAML the runner did.
package agent

import (
	"fmt"
	"log/slog"
	"os"
	"slices"

	"github.com/google/ax/pkg/apis/v1alpha1"
	"github.com/google/ax/runner"
)

// Config is the part of the Task and Workspace an agent needs.
type Config struct {
	// TaskName is the task's metadata.name.
	TaskName string
	// Values is spec.config.
	Values map[string]any
	// Workspace is the directory the agent works in.
	Workspace string
	// MCP lists the Workspace's MCP servers.
	MCP []MCPServer
}

// MCPServer is an MCP server declared on the Workspace. A server with a URL
// is reached over streamable HTTP; one with a Command runs over stdio inside
// the sandbox.
type MCPServer struct {
	Name    string
	URL     string
	Command string
	Args    []string
}

// Load reads the Task from AX_TASK_YAML and the Workspace from
// AX_WORKSPACE_YAML.
func Load() (*Config, error) {
	var task v1alpha1.Task
	if err := runner.LoadSpec("", "AX_TASK_YAML", &task); err != nil {
		return nil, err
	}
	var ws v1alpha1.Workspace
	if err := runner.LoadSpec("", "AX_WORKSPACE_YAML", &ws); err != nil {
		return nil, err
	}
	dir := task.GetSpec().GetWorkspace().GetPath()
	if dir == "" {
		// The runner starts the agent in the workspace.
		wd, err := os.Getwd()
		if err != nil {
			return nil, err
		}
		dir = wd
	}
	return &Config{
		TaskName:  task.GetMetadata().GetName(),
		Values:    task.GetSpec().GetConfig().AsMap(),
		Workspace: dir,
		MCP:       mcpServers(ws.GetSpec().GetMcp()),
	}, nil
}

// mcpServers returns the static servers followed by those listed under
// registries. Servers with a missing or duplicate name, or with neither an
// endpoint nor a command, are skipped.
func mcpServers(mcp *v1alpha1.MCPConfig) []MCPServer {
	all := slices.Clone(mcp.GetServers())
	for _, r := range mcp.GetRegistries() {
		all = append(all, r.GetServers()...)
	}
	var servers []MCPServer
	seen := map[string]bool{}
	for _, srv := range all {
		name := srv.GetName()
		if name == "" || seen[name] {
			slog.Warn("skipping MCP server with a missing or duplicate name", "name", name)
			continue
		}
		switch {
		case srv.GetEndpoint() != "":
			servers = append(servers, MCPServer{Name: name, URL: srv.GetEndpoint()})
		case srv.GetCommand() != "":
			servers = append(servers, MCPServer{Name: name, Command: srv.GetCommand(), Args: srv.GetArgs()})
		default:
			slog.Warn("skipping MCP server with neither an endpoint nor a command", "name", name)
			continue
		}
		seen[name] = true
	}
	return servers
}

// String returns the string at key, or "" when it is unset.
func (c *Config) String(key string) (string, error) {
	v, ok := c.Values[key]
	if !ok {
		return "", nil
	}
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("spec.config.%s: must be a string", key)
	}
	return s, nil
}

// Required returns the non-empty string at key.
func (c *Config) Required(key string) (string, error) {
	s, err := c.String(key)
	if err == nil && s == "" {
		err = fmt.Errorf("spec.config.%s: required", key)
	}
	return s, err
}

// Int returns the integer at key, or def when it is unset.
func (c *Config) Int(key string, def int) (int, error) {
	v, ok := c.Values[key]
	if !ok {
		return def, nil
	}
	// structpb numbers are float64.
	f, ok := v.(float64)
	if !ok || f != float64(int(f)) {
		return 0, fmt.Errorf("spec.config.%s: must be an integer", key)
	}
	return int(f), nil
}

// Bool returns the boolean at key, or false when it is unset.
func (c *Config) Bool(key string) (bool, error) {
	v, ok := c.Values[key]
	if !ok {
		return false, nil
	}
	b, ok := v.(bool)
	if !ok {
		return false, fmt.Errorf("spec.config.%s: must be a boolean", key)
	}
	return b, nil
}

// Strings returns the list of strings at key, or nil when it is unset.
func (c *Config) Strings(key string) ([]string, error) {
	v, ok := c.Values[key]
	if !ok {
		return nil, nil
	}
	list, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("spec.config.%s: must be a list of strings", key)
	}
	out := make([]string, 0, len(list))
	for _, e := range list {
		s, ok := e.(string)
		if !ok {
			return nil, fmt.Errorf("spec.config.%s: must be a list of strings", key)
		}
		out = append(out, s)
	}
	return out, nil
}

// Object returns the object at key, or nil when it is unset.
func (c *Config) Object(key string) (map[string]any, error) {
	v, ok := c.Values[key]
	if !ok {
		return nil, nil
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("spec.config.%s: must be an object", key)
	}
	return m, nil
}
