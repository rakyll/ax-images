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

// Command gemini-interactions is a coding agent on the Gemini API, using its
// Interactions endpoint. Each turn is one interaction chained to the previous
// one with previous_interaction_id, so the conversation lives on the server
// and the agent only sends what is new: first the prompt, then function
// results.
//
// The API key comes from GEMINI_API_KEY or GOOGLE_API_KEY.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"google.golang.org/genai"
	"google.golang.org/genai/interactions/models/interactions"
	"google.golang.org/genai/interactions/models/operations"

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
	if err := run(ctx, nil); err != nil {
		slog.Error("agent failed", "err", err)
		os.Exit(1)
	}
}

// run loads the task and runs it. A nil cc uses the default client config,
// which reads the API key and base URL from the environment.
func run(ctx context.Context, cc *genai.ClientConfig) error {
	cfg, err := agent.Load()
	if err != nil {
		return err
	}
	opts, err := parseOptions(cfg)
	if err != nil {
		return err
	}
	if cc == nil {
		cc = &genai.ClientConfig{}
	}
	cc.Backend = genai.BackendGeminiAPI
	client, err := genai.NewClient(ctx, cc)
	if err != nil {
		return err
	}
	return loop(ctx, client, cfg.Workspace, opts)
}

// options are the agent's spec.config keys.
type options struct {
	prompt            string
	model             string
	maxTurns          int
	systemInstruction string
	generationConfig  *interactions.GenerationConfig
	tools             []interactions.Tool
}

func parseOptions(cfg *agent.Config) (*options, error) {
	var (
		o    options
		errs []error
		err  error
	)
	o.prompt, err = cfg.Required("prompt")
	errs = append(errs, err)
	o.model, err = cfg.Required("model")
	errs = append(errs, err)
	o.maxTurns, err = cfg.Int("maxTurns", 50)
	errs = append(errs, err)
	o.systemInstruction, err = cfg.String("systemInstruction")
	errs = append(errs, err)

	gc, err := cfg.Object("generationConfig")
	errs = append(errs, err)
	if gc != nil {
		// GenerationConfig mirrors the API's generation_config, so the
		// manifest uses the API's field names.
		data, err := json.Marshal(gc)
		if err == nil {
			o.generationConfig = &interactions.GenerationConfig{}
			err = json.Unmarshal(data, o.generationConfig)
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("spec.config.generationConfig: %w", err))
		}
	}

	builtin, err := cfg.Strings("builtinTools")
	errs = append(errs, err)
	o.tools, err = tools(cfg.MCP, builtin)
	errs = append(errs, err)
	return &o, errors.Join(errs...)
}

// tools returns the local tools, the Workspace's HTTP MCP servers, and the
// requested built-in tools.
func tools(servers []agent.MCPServer, builtin []string) ([]interactions.Tool, error) {
	var out []interactions.Tool
	for _, t := range agent.Tools {
		out = append(out, interactions.NewTool(interactions.Function{
			Name:        &t.Name,
			Description: &t.Description,
			Parameters:  t.Parameters,
		}))
	}
	for _, srv := range servers {
		if srv.URL == "" {
			// The API connects to MCP servers itself and cannot reach a
			// process inside the sandbox.
			slog.Warn("skipping stdio MCP server; only servers with an endpoint are supported", "name", srv.Name)
			continue
		}
		out = append(out, interactions.NewTool(interactions.MCPServer{Name: &srv.Name, URL: &srv.URL}))
	}
	for _, name := range builtin {
		switch name {
		case "google_search":
			out = append(out, interactions.NewTool(interactions.GoogleSearch{}))
		case "url_context":
			out = append(out, interactions.NewTool(interactions.URLContext{}))
		case "code_execution":
			out = append(out, interactions.NewTool(interactions.CodeExecution{}))
		default:
			return nil, fmt.Errorf("spec.config.builtinTools: unknown tool %q (want google_search, url_context, or code_execution)", name)
		}
	}
	return out, nil
}

func loop(ctx context.Context, client *genai.Client, workspace string, o *options) error {
	input := interactions.NewInteractionsInput(o.prompt)
	var previousID *string
	handled := map[string]bool{}
	for turn := 1; ; turn++ {
		if turn > o.maxTurns {
			return fmt.Errorf("reached maxTurns (%d) before the model finished", o.maxTurns)
		}
		req := interactions.CreateModelInteraction{
			Model:                 interactions.Model(o.model),
			Input:                 &input,
			PreviousInteractionID: previousID,
			// Tools and the system instruction are not inherited from
			// the previous interaction, so every turn sends them.
			Tools:            o.tools,
			GenerationConfig: o.generationConfig,
		}
		if o.systemInstruction != "" {
			req.SystemInstruction = &o.systemInstruction
		}
		res, err := client.Interactions.Create(ctx, operations.CreateInteractionRequest{
			Body: operations.NewCreateInteractionRequestBody(req),
		})
		if err != nil {
			return fmt.Errorf("turn %d: %w", turn, err)
		}
		in := res.Interaction
		if in == nil {
			return fmt.Errorf("turn %d: response has no interaction", turn)
		}
		previousID = in.ID

		var results []interactions.Step
		for _, step := range in.Steps {
			switch {
			case step.ModelOutputStep != nil:
				for _, c := range step.ModelOutputStep.Content {
					if c.TextContent != nil && c.TextContent.Text != "" {
						fmt.Println(c.TextContent.Text)
					}
				}
			case step.MCPServerToolCallStep != nil:
				s := step.MCPServerToolCallStep
				slog.Info("mcp tool call", "server", s.ServerName, "tool", s.Name, "args", s.Arguments)
			case step.FunctionCallStep != nil:
				call := step.FunctionCallStep
				if handled[call.ID] {
					continue
				}
				handled[call.ID] = true
				slog.Info("tool call", "tool", call.Name, "args", call.Arguments)
				out := agent.CallTool(ctx, workspace, call.Name, call.Arguments)
				results = append(results, interactions.NewStep(interactions.FunctionResultStep{
					CallID: call.ID,
					Name:   &call.Name,
					Result: interactions.NewFunctionResultStepResultUnion(out),
				}))
			}
		}

		switch in.Status {
		case interactions.InteractionStatusFailed,
			interactions.InteractionStatusCancelled,
			interactions.InteractionStatusIncomplete,
			interactions.InteractionStatusBudgetExceeded:
			return fmt.Errorf("turn %d: interaction %s%s", turn, in.Status, describe(in.Errors))
		}
		if len(results) == 0 {
			slog.Info("model finished", "turns", turn, "status", in.Status)
			return nil
		}
		input = interactions.NewInteractionsInput(results)
	}
}

func describe(errs []interactions.Error) string {
	var b strings.Builder
	for _, e := range errs {
		if e.Message != nil {
			b.WriteString(": ")
			b.WriteString(*e.Message)
		}
	}
	return b.String()
}
