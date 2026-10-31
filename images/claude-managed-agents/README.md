# Claude Managed Agents

Runs [Claude Managed Agents](https://platform.claude.com/docs/en/managed-agents/overview) sessions in an AX task, using the environment worker from the Anthropic Go SDK (`github.com/anthropics/anthropic-sdk-go/lib/environments`). The source is [`cmd/claude-managed-agents`](../../cmd/claude-managed-agents).

The task is the worker of a [self-hosted environment](https://platform.claude.com/docs/en/managed-agents/self-hosted-environments). Claude runs on the Claude Platform. When the agent calls a tool from `agent_toolset_20260401` (`bash`, `read`, `write`, `edit`, `glob`, `grep`), the task runs it in `/workspace` and sends back the result. MCP tools and the agent's other server-side tools stay on the platform.

You configure the agent itself (model, system prompt, tools, MCP servers, skills) on the Claude Platform. The task adds no instructions or prompts of its own. The agent sees only `prompt`.

The task runs in one of two modes:

- **Prompt.** With `agent` and `prompt` set, the task creates a session for the agent, sends it the prompt, and prints the session's events. It exits when the agent finishes its turn.
- **Serve.** Without them, the task serves sessions that you create in the Console or through the API, one at a time, until the task is deleted.

## Set up

1. Create an agent that has the `agent_toolset_20260401` tools.
2. Create a self-hosted environment and generate an environment key for it.
3. Give the task an environment of its own. The worker claims every session in its environment, including sessions started by other prompts.

## Configure a task

```yaml
spec:
  image: "ghcr.io/rakyll/ax-images/claude-managed-agents@sha256:2f913ac5859061c7a6d70f93b8ead655c20ff5257546e86a5131541b88294ed7"
  config:
    environment: "env_..."
    agent: "agent_..."
    prompt: "Run the test suite and fix any failing tests."
  env:
    - name: ANTHROPIC_API_KEY
      value: "${ANTHROPIC_API_KEY}"
    - name: ANTHROPIC_ENVIRONMENT_KEY
      value: "${ANTHROPIC_ENVIRONMENT_KEY}"
```

| Key | Default | Meaning |
|---|---|---|
| `environment` | required | The self-hosted environment this task works for. |
| `agent` | none | Agent to start a session for. Set it together with `prompt`. |
| `agentVersion` | latest | Agent version to pin the session to. |
| `prompt` | none | The first message of the session. |

`ANTHROPIC_ENVIRONMENT_KEY` authorizes the worker. `ANTHROPIC_API_KEY` creates the session and streams its events, so serve mode does not need it. `ANTHROPIC_BASE_URL` overrides the endpoint. The SDK removes `ANTHROPIC_*` variables from the agent's `bash` environment.

In prompt mode the task prints the agent's messages and logs each tool call. It exits 0 when the agent ends its turn. It exits non-zero when the session stops for any other reason (`retries_exhausted`, `budget_reached`, `refusal`) or is terminated. The example prompt asks the agent to write its report to `/workspace/agent-output.md`. The session stays on the platform, so you can review it in the Console.

See [`task.yaml`](task.yaml) for a complete Task and Workspace.
