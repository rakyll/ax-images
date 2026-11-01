# Gemini API agent

A coding agent built on the [Gemini API](https://ai.google.dev/gemini-api/docs/interactions), using `client.Interactions` in the Go SDK (`google.golang.org/genai`). The source is [`cmd/gemini`](../../cmd/gemini). Each turn is one interaction, chained to the previous one with `previous_interaction_id`. Conversation state stays on the server, and the agent only sends what is new: first the prompt, then function results.

The model gets:

- `run_shell`, `read_file`, `write_file`, and `edit_file`, which run locally in the workspace ([`internal/agent`](../../internal/agent)).
- The Workspace's MCP servers that have an `endpoint`. The API connects to them itself as `mcp_server` tools. Stdio servers (`command`) are skipped with a warning, because the API cannot reach into the sandbox.
- Any Gemini built-in tools listed in `builtinTools`.

The agent adds no instructions or prompts of its own. The model sees only `prompt`, `systemInstruction` if you set it, and the tool definitions.

## Configure a task

```yaml
spec:
  image: "ghcr.io/rakyll/ax-images/gemini@sha256:e3d8d953fc1ca112e3b0d00a655d82008996a328a5e8dbd1777f68f492fb91bc"
  config:
    prompt: "Run the test suite and fix any failing tests."
    model: gemini-3.8-flash
    maxTurns: 50
    builtinTools: ["google_search", "url_context"]
  env:
    - name: GEMINI_API_KEY
      value: "${GEMINI_API_KEY}"
```

| Key | Default | Meaning |
|---|---|---|
| `prompt` | required | The task for the agent. |
| `model` | required | Any Gemini API model, for example `gemini-3.8-flash`. |
| `maxTurns` | `50` | Maximum number of interactions. Hitting it exits with status 1. |
| `builtinTools` | none | Any of `google_search`, `url_context`, `code_execution`. |
| `generationConfig` | none | Passed through as the interaction's `generation_config`, using the API's field names, for example `{thinking_level: high}`. |
| `systemInstruction` | none | System instruction for every interaction. |

The key comes from `GEMINI_API_KEY` (or `GOOGLE_API_KEY`). `GOOGLE_GEMINI_BASE_URL` overrides the endpoint.

The agent prints the model's text and each tool call to the container log. It exits 0 when the model finishes. It exits non-zero when an interaction fails, is cancelled, is incomplete, or exceeds its budget, and when `maxTurns` runs out. The example prompt asks the agent to write its report to `/workspace/agent-output.md`.

See [`task.yaml`](task.yaml) for a complete Task and Workspace.
