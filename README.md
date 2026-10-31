# AX images

Task images and ready-to-apply manifests for [AX](https://github.com/google/ax). Each one packages a coding agent so that a `Task` only has to describe the job in `spec.config`.

Everything is written in Go. OpenAI's Agents SDK has no Go version, so `openai-responses` runs its own agent loop on the Responses API.

Claude Code has its own runner and image in the AX repository; see [`examples/claude-code`](https://github.com/google/ax/tree/main/examples/claude-code).

## Images

### [`gemini-interactions`](images/gemini-interactions)

A coding agent on the [Gemini Interactions API](https://ai.google.dev/gemini-api/docs/interactions), using `google.golang.org/genai`.

- Source: [`cmd/gemini-interactions`](cmd/gemini-interactions)
- Manifest: [`images/gemini-interactions/task.yaml`](images/gemini-interactions/task.yaml)
- Credentials: `GEMINI_API_KEY`
- `spec.config`: `prompt` and `model` (required), `maxTurns`, `builtinTools` (`google_search`, `url_context`, `code_execution`), `generationConfig`, `systemInstruction`

### [`openai-responses`](images/openai-responses)

A coding agent on the [OpenAI Responses API](https://platform.openai.com/docs/api-reference/responses), using `github.com/openai/openai-go/v3`.

- Source: [`cmd/openai-responses`](cmd/openai-responses)
- Manifest: [`images/openai-responses/task.yaml`](images/openai-responses/task.yaml)
- Credentials: `OPENAI_API_KEY`
- `spec.config`: `prompt` and `model` (required), `maxTurns`, `builtinTools` (`web_search`), `instructions`, `parameters`

Both agents give the model the same local tools (`run_shell`, `read_file`, `write_file`, `edit_file`) and the Workspace's HTTP MCP servers. Each image's README documents its keys in full.

## Quick start

```bash
make push-gemini-interactions IMAGE_REPO=gcr.io/my-project/ax-images
# Pin the printed digest in images/gemini-interactions/task.yaml, then:
GEMINI_API_KEY=... envsubst < images/gemini-interactions/task.yaml | ax apply -f -

ax describe task gemini-interactions
ax ssh gemini-interactions -- cat /workspace/agent-output.md
```

## Development

The binaries depend on AX's Go packages. `go.mod` points them at a checkout in `../ax`:

```bash
git clone git@github.com:google/ax.git ../ax   # or: go mod edit -replace github.com/google/ax=<path>
make test        # go vet and go test
make binaries    # cross-compile the agents (each includes the runner) into bin/linux_amd64
```

The agent tests run against fake API servers, so they need no keys. Images are built for linux/amd64 by default; set `ARCH=arm64` for arm64. The default base image is pulled from `gcr.io/ax-substrate`. To build without access to it, set `BASE_IMAGE=python:3.12-slim`, which is what that image is built on.
