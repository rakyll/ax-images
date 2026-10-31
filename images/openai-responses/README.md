# OpenAI Responses agent

A coding agent built on the [OpenAI Responses API](https://platform.openai.com/docs/api-reference/responses), using the official Go SDK (`github.com/openai/openai-go/v3`). The source is [`cmd/openai-responses`](../../cmd/openai-responses). OpenAI's Agents SDK has no Go version, so this agent runs its loop on the API directly. Each turn is one response, chained to the previous one with `previous_response_id`. Conversation state stays on the server, and the agent only sends what is new: first the prompt, then function call outputs.

The model gets:

- `run_shell`, `read_file`, `write_file`, and `edit_file`, which run locally in the workspace ([`internal/agent`](../../internal/agent)).
- The Workspace's MCP servers that have an `endpoint`. The API connects to them itself as hosted `mcp` tools, with `require_approval: never`. Stdio servers (`command`) are skipped with a warning, because the API cannot reach into the sandbox.
- Any hosted tools listed in `builtinTools`.

The agent adds no instructions or prompts of its own. The model sees only `prompt`, `instructions` if you set it, and the tool definitions.

## Configure a task

```yaml
spec:
  image: "ghcr.io/rakyll/ax-images/openai-responses@sha256:39d5c5dc7900f132b7e8a5f613efc7aa173c901fcf873f777a67a5edd2bba4e2"
  config:
    prompt: "Run the test suite and fix any failing tests."
    model: gpt-5.5
    maxTurns: 50
    builtinTools: ["web_search"]
  env:
    - name: OPENAI_API_KEY
      value: "${OPENAI_API_KEY}"
```

| Key | Default | Meaning |
|---|---|---|
| `prompt` | required | The task for the agent. |
| `model` | required | Any model the Responses API accepts. |
| `maxTurns` | `50` | Maximum number of responses. Hitting it exits with status 1. |
| `builtinTools` | none | `web_search`. |
| `instructions` | none | Instructions for every response. |
| `parameters` | none | Extra top-level request fields, passed through unchanged, for example `{reasoning: {effort: high}}`. The agent sets `model`, `input`, `instructions`, `previous_response_id`, and `tools` itself, so you cannot set them here. |

The key comes from `OPENAI_API_KEY`. `OPENAI_BASE_URL` points the agent at a compatible endpoint.

The agent prints the model's text and each tool call to the container log. It exits 0 when the model finishes. It exits non-zero when a response fails, is cancelled, or is incomplete, and when `maxTurns` runs out. The example prompt asks the agent to write its report to `/workspace/agent-output.md`.

See [`task.yaml`](task.yaml) for a complete Task and Workspace.
