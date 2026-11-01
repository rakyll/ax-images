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

// Command openai is a coding agent on the OpenAI Responses API.
// Each turn is one response chained to the previous one with
// previous_response_id, so the conversation lives on the server and the
// agent only sends what is new: first the prompt, then function call outputs.
//
// The API key comes from OPENAI_API_KEY, and OPENAI_BASE_URL can point the
// agent at a compatible endpoint.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"sort"
	"syscall"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/packages/param"
	"github.com/openai/openai-go/v3/responses"

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

// run loads the task and runs it. Extra client options are for tests.
func run(ctx context.Context, clientOpts ...option.RequestOption) error {
	cfg, err := agent.Load()
	if err != nil {
		return err
	}
	opts, err := parseOptions(cfg)
	if err != nil {
		return err
	}
	client := openai.NewClient(clientOpts...)
	return loop(ctx, &client, cfg.Workspace, opts)
}

// options are the agent's spec.config keys.
type options struct {
	prompt       string
	model        string
	maxTurns     int
	instructions string
	// parameters are extra top-level request fields, such as reasoning.
	parameters []option.RequestOption
	tools      []responses.ToolUnionParam
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
	o.instructions, err = cfg.String("instructions")
	errs = append(errs, err)

	params, err := cfg.Object("parameters")
	errs = append(errs, err)
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		switch k {
		case "model", "input", "instructions", "previous_response_id", "tools":
			errs = append(errs, fmt.Errorf("spec.config.parameters.%s: set by the agent", k))
		default:
			o.parameters = append(o.parameters, option.WithJSONSet(k, params[k]))
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
func tools(servers []agent.MCPServer, builtin []string) ([]responses.ToolUnionParam, error) {
	var out []responses.ToolUnionParam
	for _, t := range agent.Tools {
		// Strict mode would require every property to be required.
		tool := responses.ToolParamOfFunction(t.Name, t.Parameters, false)
		tool.OfFunction.Description = param.NewOpt(t.Description)
		out = append(out, tool)
	}
	for _, srv := range servers {
		if srv.URL == "" {
			// The API connects to MCP servers itself and cannot reach a
			// process inside the sandbox.
			slog.Warn("skipping stdio MCP server; only servers with an endpoint are supported", "name", srv.Name)
			continue
		}
		out = append(out, responses.ToolUnionParam{OfMcp: &responses.ToolMcpParam{
			ServerLabel: srv.Name,
			ServerURL:   param.NewOpt(srv.URL),
			// The sandbox is the isolation boundary, as for local tools.
			RequireApproval: responses.ToolMcpRequireApprovalUnionParam{
				OfMcpToolApprovalSetting: param.NewOpt("never"),
			},
		}})
	}
	for _, name := range builtin {
		switch name {
		case "web_search":
			out = append(out, responses.ToolParamOfWebSearch(responses.WebSearchToolTypeWebSearch))
		default:
			return nil, fmt.Errorf("spec.config.builtinTools: unknown tool %q (want web_search)", name)
		}
	}
	return out, nil
}

func loop(ctx context.Context, client *openai.Client, workspace string, o *options) error {
	input := responses.ResponseNewParamsInputUnion{OfString: param.NewOpt(o.prompt)}
	var previousID string
	for turn := 1; ; turn++ {
		if turn > o.maxTurns {
			return fmt.Errorf("reached maxTurns (%d) before the model finished", o.maxTurns)
		}
		req := responses.ResponseNewParams{
			Model: o.model,
			Input: input,
			// Instructions and tools are not inherited from the previous
			// response, so every turn sends them.
			Tools: o.tools,
		}
		if o.instructions != "" {
			req.Instructions = param.NewOpt(o.instructions)
		}
		if previousID != "" {
			req.PreviousResponseID = param.NewOpt(previousID)
		}
		resp, err := client.Responses.New(ctx, req, o.parameters...)
		if err != nil {
			return fmt.Errorf("turn %d: %w", turn, err)
		}
		previousID = resp.ID

		var outputs responses.ResponseInputParam
		for _, item := range resp.Output {
			switch item.Type {
			case "message":
				for _, c := range item.AsMessage().Content {
					switch {
					case c.Text != "":
						fmt.Println(c.Text)
					case c.Refusal != "":
						fmt.Println("refusal:", c.Refusal)
					}
				}
			case "mcp_call":
				call := item.AsMcpCall()
				slog.Info("mcp tool call", "server", call.ServerLabel, "tool", call.Name, "args", call.Arguments)
			case "function_call":
				call := item.AsFunctionCall()
				slog.Info("tool call", "tool", call.Name, "args", call.Arguments)
				out := responses.ResponseInputItemParamOfFunctionCallOutput(agent.CallToolJSON(ctx, workspace, call.Name, call.Arguments))
				out.OfFunctionCallOutput.CallID = param.NewOpt(call.CallID)
				outputs = append(outputs, out)
			}
		}

		switch resp.Status {
		case responses.ResponseStatusFailed, responses.ResponseStatusCancelled:
			return fmt.Errorf("turn %d: response %s: %s", turn, resp.Status, resp.Error.Message)
		case responses.ResponseStatusIncomplete:
			return fmt.Errorf("turn %d: response incomplete: %s", turn, resp.IncompleteDetails.Reason)
		}
		if len(outputs) == 0 {
			slog.Info("model finished", "turns", turn, "status", resp.Status)
			return nil
		}
		input = responses.ResponseNewParamsInputUnion{OfInputItemList: outputs}
	}
}
