# AX images

[![release](https://github.com/rakyll/ax-images/actions/workflows/release.yml/badge.svg)](https://github.com/rakyll/ax-images/actions/workflows/release.yml)
[![license](https://img.shields.io/badge/license-Apache%202.0-blue)](LICENSE)

Ready-to-run task images for [AX](https://github.com/google/ax): coding agents and CI runners.
Point an agent at a git repo with a prompt. Get back a diff and a markdown report.

| Image | Runs | Secrets |
|---|---|---|
| [`antigravity`](images/antigravity) | The [Antigravity agent](https://ai.google.dev/gemini-api/docs/antigravity-agent) on the Gemini API, with tools running in the workspace | `GEMINI_API_KEY` |
| [`openai`](images/openai) | A coding agent on the [OpenAI Responses API](https://platform.openai.com/docs/api-reference/responses) | `OPENAI_API_KEY` |
| [`claude-managed-agents`](images/claude-managed-agents) | [Claude Managed Agents](https://platform.claude.com/docs/en/managed-agents/overview) sessions, with tools running in the workspace | `ANTHROPIC_API_KEY`, `ANTHROPIC_ENVIRONMENT_KEY` |
| [`github-actions`](images/github-actions) | [GitHub Actions](https://docs.github.com/en/actions) jobs, as just-in-time self-hosted runners | `GITHUB_TOKEN` |

Images are public at `ghcr.io/rakyll/ax-images/<image>`.

## Quickstart

**You need:** an AX cluster, the `ax` CLI, and `envsubst`.

The sample task clones [pallets/itsdangerous](https://github.com/pallets/itsdangerous),
runs its tests, and fixes any failures:

```bash
GEMINI_API_KEY=... envsubst < images/antigravity/task.yaml | ax apply -f -

ax ssh antigravity -- cat /workspace/agent-output.md   # the report
ax ssh antigravity -- git -C /workspace diff           # the changes
```

## Make it yours

Edit `spec.config` in the task:

```yaml
spec:
  image: ghcr.io/rakyll/ax-images/antigravity@sha256:...
  config:
    prompt: Fix the flaky tests and explain what you changed.
    model: gemini-3.8-flash
    maxTurns: 100
```

Each image's README lists every config key.

See [CONTRIBUTING.md](CONTRIBUTING.md) to build images or cut a release.
