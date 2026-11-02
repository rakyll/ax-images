# Antigravity agent

Runs the [Antigravity agent](https://ai.google.dev/gemini-api/docs/antigravity-agent) on the Gemini API, with its tools running in the AX task. It uses `client.Interactions` in the Go SDK (`google.golang.org/genai`) with `agent` set instead of `model`. The source is [`cmd/antigravity`](../../cmd/antigravity).

The Antigravity agent always works in a Linux sandbox hosted by Google, and its built-in tools act on that sandbox. This image keeps the work in the task instead:

- It replaces the agent's default tools, so the hosted `code_execution` is not offered.
- The API always adds the hosted filesystem tools (`view_file`, `write_to_file`, `replace_file_content`, `list_dir`, `find_by_name`). The image mounts a [hook](https://ai.google.dev/gemini-api/docs/agent-hooks) in the sandbox, at `.agents/hooks.json`, that denies every tool call made there. Hooks do not fire for function tools.
- The agent gets `run_shell`, `read_file`, `write_file`, and `edit_file` as functions ([`internal/agent`](../../internal/agent)).

The first interaction creates the sandbox, and later interactions reuse it by its `environment_id`. Any call the sandbox answers, such as a denied one, is logged as a warning and is not run in the workspace.

Each interaction ends with `requires_action` and the agent's function calls. The image runs them in the workspace and sends the results in the next interaction, chained with `previous_interaction_id`. Every command and file change happens in the task.

The agent also gets:

- The Workspace's MCP servers that have an `endpoint`. The API connects to them itself as `mcp_server` tools. Stdio servers (`command`) are skipped with a warning, because the API cannot reach into the sandbox.
- Any of the Gemini built-in tools `google_search` and `url_context` listed in `builtinTools`.

The image adds no instructions or prompts of its own.

## Configure a task

```yaml
spec:
  image: "ghcr.io/rakyll/ax-images/antigravity@sha256:571b06a4273d9750075053e72ea9e9305560418b5db61c1eeed7d375f9d699d0"
  config:
    prompt: "Run the test suite and fix any failing tests."
    model: gemini-3.8-flash
    maxTurns: 100
    builtinTools: ["google_search", "url_context"]
  env:
    - name: GEMINI_API_KEY
      value: "${GEMINI_API_KEY}"
```

| Key | Default | Meaning |
|---|---|---|
| `prompt` | required | The task for the agent. |
| `agent` | `antigravity-preview-09-2026` | The Antigravity agent version. |
| `model` | the agent's default | The model the agent reasons with, for example `gemini-3.8-flash` or `gemini-4-argon`. Passed as `agent_config.model`. |
| `maxTotalTokens` | none | Token budget, passed as `agent_config.max_total_tokens`. Exceeding it ends the interaction as `incomplete`. |
| `maxTurns` | `50` | Maximum number of interactions. Each one usually returns a single tool call, so allow about one turn per tool call. Hitting it exits with status 1. |
| `builtinTools` | none | Any of `google_search`, `url_context`. `code_execution` is rejected because it would run outside the task. |
| `systemInstruction` | none | System instruction for every interaction. |

The key comes from `GEMINI_API_KEY` (or `GOOGLE_API_KEY`). `GOOGLE_GEMINI_BASE_URL` overrides the endpoint.

The image prints the agent's text and each tool call to the container log. It exits 0 when the agent finishes. It exits non-zero when an interaction fails, is cancelled, is incomplete, or exceeds its budget, and when `maxTurns` runs out. The example prompt asks the agent to write its report to `/workspace/agent-output.md`.

See [`task.yaml`](task.yaml) for a complete Task and Workspace.
