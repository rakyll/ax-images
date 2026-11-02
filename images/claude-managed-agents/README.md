# Claude Managed Agents

Runs [Claude Managed Agents](https://platform.claude.com/docs/en/managed-agents/overview) sessions in an AX task. The source is [`cmd/claude-managed-agents`](../../cmd/claude-managed-agents).

The task is the worker of a [self-hosted environment](https://platform.claude.com/docs/en/managed-agents/self-hosted-environments). Claude runs on the Claude Platform. The agent's `bash`, `read`, `write`, `edit`, `glob`, and `grep` tools run in `/workspace`. MCP and other server-side tools stay on the platform.

The task runs in one of three modes:

- **Prompt.** Set `agent` or `agentId`, and `prompt`. The task starts a session, sends the prompt, and exits when the agent finishes.
- **Continue.** Set `session` and `prompt` to follow up on an earlier session. Bind the same Workspace as the earlier task.
- **Serve.** Set neither. The task serves sessions that you create in the Console or through the API until the task is deleted.

Give each task its own environment. The worker claims every session in its environment.

## Configure a task

```yaml
spec:
  image: "ghcr.io/rakyll/ax-images/claude-managed-agents@sha256:1a532d86c58a5fea3005e9c525683cf8f354e2c403be6460a308ad2f8f0510c0"
  config:
    environment: "env_..."
    agent:
      model: "claude-opus-5-5"
      system: "You are a careful software engineer."
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
| `agent` | none | An agent to create on each run: `model` (required), `name` (defaults to the task name), `system`, and `description`. |
| `agentId` | none | An existing agent to use instead, for example one with MCP servers or skills. It needs the `agent_toolset_20260401` tools. |
| `agentVersion` | latest | Version of `agentId` to use. |
| `session` | none | An existing session to continue. Leave `agent` and `agentId` unset. |
| `prompt` | none | The message to send to the session. |

`ANTHROPIC_ENVIRONMENT_KEY` authorizes the worker. `ANTHROPIC_API_KEY` creates agents and sessions, so serve mode does not need it. `ANTHROPIC_BASE_URL` overrides the endpoint.

The task logs the agent and session IDs, the agent's messages, and each tool call. It exits 0 when the agent ends its turn, and non-zero when the session stops for any other reason. You can review the session in the Console.

See [`task.yaml`](task.yaml) for a complete Task and Workspace.
