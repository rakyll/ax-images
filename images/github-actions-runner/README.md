# GitHub Actions runner

Runs [GitHub Actions](https://docs.github.com/en/actions) jobs in an AX task, as [just-in-time self-hosted runners](https://docs.github.com/en/actions/reference/runners/self-hosted-runners#just-in-time-runners). The source is [`cmd/github-actions-runner`](../../cmd/github-actions-runner).

For each job, the task registers a runner through the GitHub API and starts GitHub's [actions runner](https://github.com/actions/runner) with that registration. A just-in-time runner takes exactly one job and then unregisters itself, so no runner registration outlives its job. If the runner fails or the task is stopped, the task removes the registration itself.

Workflows pick the runner by its labels:

```yaml
jobs:
  test:
    runs-on: [self-hosted, ax]
```

## Configure a task

```yaml
spec:
  image: "ghcr.io/rakyll/ax-images/github-actions-runner@sha256:cf8db661c8b241a587d85fad4a233f6631c18528117d641e2e3a5de32d14ff8e"
  config:
    repository: "<owner>/<repo>"
    labels: ["self-hosted", "ax"]
    jobs: 1
  env:
    - name: GITHUB_TOKEN
      value: "${GITHUB_TOKEN}"
```

| Key | Default | Meaning |
|---|---|---|
| `repository` | | `<owner>/<repo>` to run jobs for. Set this or `organization`. |
| `organization` | | Organization to run jobs for. Set this or `repository`. |
| `labels` | `self-hosted`, `Linux`, `X64` or `ARM64` | Runner labels that workflows match with `runs-on`. |
| `jobs` | `1` | Number of jobs to run before the task exits. `0` keeps taking jobs until the task is deleted. |
| `name` | task name | Runner name prefix. Each runner gets a random suffix. |
| `runnerGroupId` | `1` | Runner group to register in. `1` is the default group. |

`GITHUB_TOKEN` must be able to manage self-hosted runners: a fine-grained token with "Administration: write" on the repository, or "Self-hosted runners: write" on the organization. The task keeps the token to itself and removes it from the environment of workflow steps. `GITHUB_API_URL` points the task at GitHub Enterprise Server, for example `https://github.example.com/api/v3`.

The task exits 0 after its jobs finish, or when it is stopped. It exits non-zero when a registration or the runner fails. A failed workflow job is not a runner failure.

See [`task.yaml`](task.yaml) for a complete Task.
