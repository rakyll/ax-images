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

// Command claude-managed-agents runs Claude Managed Agents sessions in an AX
// task.
//
// The task is the worker of a self-hosted Managed Agents environment: it
// claims the environment's sessions and runs their agent_toolset tools
// (bash, read, write, edit, glob, grep) in the AX workspace, using the
// environment worker from the Anthropic Go SDK.
//
// With spec.config.prompt set, the task also starts a session with the
// prompt, prints the session's events, and exits when the agent finishes.
// The session's agent is either defined in spec.config.agent, which the task
// creates first, or an existing agent named by spec.config.agentId. With
// spec.config.session and
// spec.config.prompt set, it sends the prompt to that existing session
// instead, so a task can continue where an earlier one left off. Without a
// prompt, it serves sessions created elsewhere until it is stopped.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/lib/environments"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/anthropics/anthropic-sdk-go/packages/param"

	"github.com/rakyll/ax-images/internal/agent"
	"github.com/rakyll/ax-images/internal/taskrunner"
)

func main() {
	if taskrunner.Invoked() {
		taskrunner.Main()
		return
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx); err != nil {
		slog.Error("agent failed", "err", err)
		os.Exit(1)
	}
}

// serve runs the environment worker until ctx is cancelled. Tests replace it.
var serve = func(ctx context.Context, client anthropic.Client, opts environments.EnvironmentWorkerOptions) error {
	return environments.NewEnvironmentWorker(client, opts).Run(ctx)
}

func run(ctx context.Context, clientOpts ...option.RequestOption) error {
	cfg, err := agent.Load()
	if err != nil {
		return err
	}
	o, err := parseOptions(cfg)
	if err != nil {
		return err
	}
	key := os.Getenv("ANTHROPIC_ENVIRONMENT_KEY")
	if key == "" {
		return errors.New("ANTHROPIC_ENVIRONMENT_KEY is not set")
	}
	client := anthropic.NewClient(clientOpts...)
	workerOpts := environments.EnvironmentWorkerOptions{
		EnvironmentID:  o.environment,
		EnvironmentKey: key,
		Workdir:        cfg.Workspace,
		RequestOptions: clientOpts,
	}

	if o.prompt == "" {
		slog.Info("serving sessions", "environment", o.environment, "workdir", cfg.Workspace)
		return serve(ctx, client, workerOpts)
	}

	if o.newAgent != nil {
		a, err := client.Beta.Agents.New(ctx, *o.newAgent)
		if err != nil {
			return fmt.Errorf("creating agent: %w", err)
		}
		o.agent.OfString = param.NewOpt(a.ID)
		slog.Info("agent created", "agent", a.ID, "version", a.Version)
	}

	sessionID := o.session
	if sessionID == "" {
		session, err := client.Beta.Sessions.New(ctx, anthropic.BetaSessionNewParams{
			Agent:         o.agent,
			EnvironmentID: o.environment,
			Title:         param.NewOpt(cmp(cfg.TaskName, "ax")),
		})
		if err != nil {
			return fmt.Errorf("creating session: %w", err)
		}
		sessionID = session.ID
		slog.Info("session created", "session", sessionID, "environment", o.environment)
	} else {
		slog.Info("continuing session", "session", sessionID, "environment", o.environment)
	}

	workerCtx, stopWorker := context.WithCancel(ctx)
	defer stopWorker()
	workerDone := make(chan error, 1)
	go func() { workerDone <- serve(workerCtx, client, workerOpts) }()

	err = follow(ctx, client, sessionID, o.prompt)
	stopWorker()
	if werr := <-workerDone; werr != nil && err == nil {
		err = fmt.Errorf("environment worker: %w", werr)
	}
	return err
}

// options are the image's spec.config keys.
type options struct {
	environment string
	// newAgent, when set, is created before the session, which then uses it
	// in place of agent.
	newAgent *anthropic.BetaAgentNewParams
	agent    anthropic.BetaSessionNewParamsAgentUnion
	// session is an existing session to continue instead of creating one.
	session string
	prompt  string
}

func parseOptions(cfg *agent.Config) (*options, error) {
	var (
		o    options
		errs []error
	)
	var err error
	o.environment, err = cfg.Required("environment")
	errs = append(errs, err)
	o.newAgent, err = parseAgent(cfg)
	errs = append(errs, err)
	agentID, err := cfg.String("agentId")
	errs = append(errs, err)
	version, err := cfg.Int("agentVersion", 0)
	errs = append(errs, err)
	o.session, err = cfg.String("session")
	errs = append(errs, err)
	o.prompt, err = cfg.String("prompt")
	errs = append(errs, err)
	hasAgent := o.newAgent != nil || agentID != ""

	switch {
	case o.session != "" && (hasAgent || version != 0):
		errs = append(errs, errors.New("spec.config.session: the session already has an agent, so leave agent, agentId, and agentVersion unset"))
	case o.session != "" && o.prompt == "":
		errs = append(errs, errors.New("spec.config.session: requires prompt"))
	case o.session != "":
	case o.newAgent != nil && agentID != "":
		errs = append(errs, errors.New("spec.config: set agent to create an agent or agentId to use an existing one, not both"))
	case hasAgent != (o.prompt != ""):
		errs = append(errs, errors.New("spec.config: set agent or agentId together with prompt, or none of them to serve sessions created elsewhere"))
	case version != 0 && agentID == "":
		errs = append(errs, errors.New("spec.config.agentVersion: requires agentId"))
	case o.newAgent != nil:
	case version != 0:
		o.agent.OfBetaManagedAgentsAgents = &anthropic.BetaManagedAgentsAgentParams{
			ID:      agentID,
			Type:    "agent",
			Version: param.NewOpt(int64(version)),
		}
	default:
		o.agent.OfString = param.NewOpt(agentID)
	}
	return &o, errors.Join(errs...)
}

// parseAgent reads the agent definition at spec.config.agent, or returns nil
// when it is unset. The agent always gets the agent_toolset_20260401 tools,
// since running them is what the task is for.
func parseAgent(cfg *agent.Config) (*anthropic.BetaAgentNewParams, error) {
	if id, err := cfg.String("agent"); err == nil && id != "" {
		return nil, errors.New("spec.config.agent: must be an object that defines the agent; set agentId to use an existing agent")
	}
	def, err := cfg.Object("agent")
	if err != nil || def == nil {
		return nil, err
	}
	fields := map[string]string{"name": "", "model": "", "system": "", "description": ""}
	var errs []error
	for k, v := range def {
		if _, ok := fields[k]; !ok {
			errs = append(errs, fmt.Errorf("spec.config.agent.%s: unknown field; want name, model, system, or description", k))
			continue
		}
		s, ok := v.(string)
		if !ok {
			errs = append(errs, fmt.Errorf("spec.config.agent.%s: must be a string", k))
		}
		fields[k] = s
	}
	if fields["model"] == "" {
		errs = append(errs, errors.New("spec.config.agent.model: required"))
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	p := &anthropic.BetaAgentNewParams{
		Name:  cmp(fields["name"], cfg.TaskName, "ax"),
		Model: anthropic.BetaManagedAgentsModelConfigParams{ID: fields["model"]},
		Tools: []anthropic.BetaAgentNewParamsToolUnion{{
			OfAgentToolset20260401: &anthropic.BetaManagedAgentsAgentToolset20260401Params{Type: "agent_toolset_20260401"},
		}},
	}
	if fields["system"] != "" {
		p.System = param.NewOpt(fields["system"])
	}
	if fields["description"] != "" {
		p.Description = param.NewOpt(fields["description"])
	}
	return p, nil
}

// follow sends the prompt to the session and prints its events until the
// agent finishes its turn.
func follow(ctx context.Context, client anthropic.Client, sessionID, prompt string) error {
	// Open the stream before sending the prompt so that no event is missed.
	stream := client.Beta.Sessions.Events.StreamEvents(ctx, sessionID, anthropic.BetaSessionEventStreamParams{})
	defer stream.Close()

	_, err := client.Beta.Sessions.Events.Send(ctx, sessionID, anthropic.BetaSessionEventSendParams{
		Events: []anthropic.BetaManagedAgentsEventParamsUnion{{
			OfUserMessage: &anthropic.BetaManagedAgentsUserMessageEventParams{
				Type: "user.message",
				Content: []anthropic.BetaManagedAgentsUserMessageEventParamsContentUnion{{
					OfText: &anthropic.BetaManagedAgentsTextBlockParam{Type: "text", Text: prompt},
				}},
			},
		}},
	})
	if err != nil {
		return fmt.Errorf("sending prompt: %w", err)
	}

	for stream.Next() {
		ev := stream.Current()
		switch ev.Type {
		case "agent.message":
			for _, c := range ev.AsAgentMessage().Content {
				if c.Type == "text" {
					fmt.Println(c.Text)
				}
			}
		case "agent.tool_use":
			e := ev.AsAgentToolUse()
			slog.Info("tool call", "tool", e.Name, "args", e.Input)
		case "agent.custom_tool_use":
			e := ev.AsAgentCustomToolUse()
			slog.Info("custom tool call", "tool", e.Name, "args", e.Input)
		case "agent.mcp_tool_use":
			e := ev.AsAgentMCPToolUse()
			slog.Info("mcp tool call", "server", e.MCPServerName, "tool", e.Name, "args", e.Input)
		case "session.error":
			slog.Warn("session error", "message", ev.AsSessionError().Error.Message)
		case "session.status_idle":
			switch reason := ev.AsSessionStatusIdle().StopReason.Type; reason {
			case "requires_action":
				// Waiting on a tool result or a confirmation.
			case "end_turn":
				slog.Info("agent finished", "session", sessionID)
				return nil
			default:
				return fmt.Errorf("session %s stopped: %s", sessionID, reason)
			}
		case "session.status_terminated", "session.deleted":
			return fmt.Errorf("session %s: %s", sessionID, ev.Type)
		}
	}
	if err := stream.Err(); err != nil {
		return fmt.Errorf("streaming session events: %w", err)
	}
	if ctx.Err() != nil {
		return nil
	}
	return fmt.Errorf("session %s: event stream ended before the agent finished", sessionID)
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
