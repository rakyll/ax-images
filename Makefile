# Copyright 2026 Google LLC
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# Every directory under images/ is one image: images/<name>/Dockerfile is
# built with the repository root as context and pushed as
# $(IMAGE_REPO)/<name>:latest. The digest is written to bin/<name>.digest,
# and make pin writes it into the task.yaml.
#
#   make push-antigravity IMAGE_REPO=gcr.io/my-project/ax-images
#   make images IMAGE_REPO=...       # build and push all of them
#   make pin IMAGE_REPO=...          # point the task.yaml files at the pushed digests
#   make test
#
# images/<name> runs the Go agent in cmd/<name>. Set ARCH=arm64 to build
# arm64 images.

IMAGES := $(notdir $(patsubst %/,%,$(wildcard images/*/)))

IMAGE_REPO ?=
CONTAINER_CLI ?= $(shell command -v podman 2>/dev/null || command -v docker 2>/dev/null)

# Optional build-arg overrides, passed only when set.
BUILD_ARGS := $(if $(BASE_IMAGE),--build-arg BASE_IMAGE=$(BASE_IMAGE),)

ARCH ?= amd64

.PHONY: binaries images pin list-images test check-image-repo $(addprefix build-,$(IMAGES)) $(addprefix push-,$(IMAGES))

# Every agent, cross-compiled for the images. Each includes the task runner.
binaries:
	@mkdir -p bin/linux_$(ARCH)
	GOOS=linux GOARCH=$(ARCH) CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o bin/linux_$(ARCH)/ ./cmd/...

check-image-repo:
	@test -n "$(IMAGE_REPO)" || { echo "IMAGE_REPO is not set, e.g. IMAGE_REPO=gcr.io/my-project/ax-images"; exit 1; }

define image_rules
build-$(1): binaries check-image-repo
	$$(CONTAINER_CLI) build --platform linux/$$(ARCH) --build-arg TARGETARCH=$$(ARCH) $$(BUILD_ARGS) -t $$(IMAGE_REPO)/$(1):latest -f images/$(1)/Dockerfile .

push-$(1): build-$(1)
	@echo "==> Pushing $$(IMAGE_REPO)/$(1):latest..."
	@if $$(CONTAINER_CLI) --version 2>/dev/null | grep -qi podman; then \
		$$(CONTAINER_CLI) push --digestfile bin/$(1).digest $$(IMAGE_REPO)/$(1):latest; \
	else \
		$$(CONTAINER_CLI) push $$(IMAGE_REPO)/$(1):latest && \
		$$(CONTAINER_CLI) image inspect --format '{{range .RepoDigests}}{{println .}}{{end}}' $$(IMAGE_REPO)/$(1):latest | \
			grep "^$$(IMAGE_REPO)/$(1)@" | head -n1 | sed 's/.*@//' > bin/$(1).digest; \
	fi
	@echo "==> Pushed digest: $$(IMAGE_REPO)/$(1)@$$$$(cat bin/$(1).digest)"
endef
$(foreach img,$(IMAGES),$(eval $(call image_rules,$(img))))

images: $(addprefix push-,$(IMAGES))

# Point the spec.image in each images/<name>/task.yaml and README.md at
# $(IMAGE_REPO)/<name>@<digest>, using the digests that push-<name> writes to
# bin/<name>.digest.
pin: check-image-repo
	@for img in $(IMAGES); do \
		test -f bin/$$img.digest || continue; \
		ref="$(IMAGE_REPO)/$$img@$$(cat bin/$$img.digest)"; \
		for f in images/$$img/task.yaml images/$$img/README.md; do \
			sed -i.bak -E "s|^(  image: ).*|\1\"$$ref\"|" $$f && rm $$f.bak; \
		done; \
		echo "$$img -> $$ref"; \
	done

list-images:
	@echo $(IMAGES)

test:
	go vet ./...
	go test ./...
