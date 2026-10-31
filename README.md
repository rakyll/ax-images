# AX images

Ready-to-run coding agents for [AX](https://github.com/google/ax). Pick an image, set a prompt, apply.

| Image | Agent | API key |
|---|---|---|
| [`gemini-interactions`](images/gemini-interactions) | [Gemini Interactions API](https://ai.google.dev/gemini-api/docs/interactions) | `GEMINI_API_KEY` |
| [`openai-responses`](images/openai-responses) | [OpenAI Responses API](https://platform.openai.com/docs/api-reference/responses) | `OPENAI_API_KEY` |

Images are public at `ghcr.io/rakyll/ax-images/<image>`. Each image's README lists its `spec.config` keys.

## Run one

```bash
GEMINI_API_KEY=... envsubst < images/gemini-interactions/task.yaml | ax apply -f -
ax ssh gemini-interactions -- cat /workspace/agent-output.md
```

The task only sets `spec.config`:

```yaml
config:
  prompt: "Run the tests and fix any failures."
  model: gemini-3.8-flash
```

The agent works in `/workspace` with shell and file tools, plus the Workspace's HTTP MCP servers. It adds no prompts of its own.

## Release

```bash
git tag v0.1.0 && git push origin v0.1.0
```

Every tag builds the images (amd64 and arm64), pushes them to GHCR, and pins each `task.yaml` to the new digest.

## Develop

```bash
make test
make build-gemini-interactions IMAGE_REPO=localhost/ax-images
make push-gemini-interactions pin IMAGE_REPO=<your-registry>   # use your own registry
```

Everything is Go. Agents live in `cmd/`, shared code in `internal/`.
