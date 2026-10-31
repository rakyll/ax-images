# AX images

[![release](https://github.com/rakyll/ax-images/actions/workflows/release.yml/badge.svg)](https://github.com/rakyll/ax-images/actions/workflows/release.yml)
[![license](https://img.shields.io/badge/license-Apache%202.0-blue)](LICENSE)

Ready-to-run coding agents for [AX](https://github.com/google/ax).
Point an agent at a git repo with a prompt. Get back a diff and a markdown report.

| Image | Agent | API key |
|---|---|---|
| [`gemini-interactions`](images/gemini-interactions) | [Gemini Interactions API](https://ai.google.dev/gemini-api/docs/interactions) | `GEMINI_API_KEY` |
| [`openai-responses`](images/openai-responses) | [OpenAI Responses API](https://platform.openai.com/docs/api-reference/responses) | `OPENAI_API_KEY` |

Images are public at `ghcr.io/rakyll/ax-images/<image>`.

## Quickstart

**You need:** an AX cluster, the `ax` CLI, and `envsubst`.

The sample task clones [pallets/itsdangerous](https://github.com/pallets/itsdangerous),
runs its tests, and fixes any failures:

```bash
GEMINI_API_KEY=... envsubst < images/gemini-interactions/task.yaml | ax apply -f -

ax ssh gemini-interactions -- cat /workspace/agent-output.md   # the report
ax ssh gemini-interactions -- git -C /workspace diff           # the changes
```

## Make it yours

Edit `spec.config` in the task:

```yaml
spec:
  image: ghcr.io/rakyll/ax-images/gemini-interactions@sha256:...
  config:
    prompt: Fix the flaky tests and explain what you changed.
    model: gemini-3.8-flash
    maxTurns: 50
```

Each image's README lists every config key.

## Contributing

### Develop

```bash
make test
make build-gemini-interactions IMAGE_REPO=localhost/ax-images
make push-gemini-interactions pin IMAGE_REPO=<your-registry>   # use your own registry
```

### Release

```bash
git tag v0.1.0 && git push origin v0.1.0
```

Every tag builds the images (amd64 and arm64), pushes them to GHCR, and pins each `task.yaml` to the new digest.
