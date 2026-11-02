# Contributing

## Develop

```bash
make test
make build-antigravity IMAGE_REPO=localhost/ax-images
make push-antigravity pin IMAGE_REPO=<your-registry>   # use your own registry
```

## Release

```bash
git tag v0.1.0 && git push origin v0.1.0
```

Every tag builds the images (amd64 and arm64), pushes them to GHCR, and pins each `task.yaml` to the new digest.
