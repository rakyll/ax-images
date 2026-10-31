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

# Task image that runs GitHub Actions jobs as just-in-time self-hosted
# runners. See images/github-actions-runner/README.md.
#
#   make push-github-actions-runner IMAGE_REPO=<your-registry>

ARG BASE_IMAGE=python:3.12-slim
FROM ${BASE_IMAGE}

RUN apt-get update && apt-get install -y --no-install-recommends \
        git \
        curl \
        ca-certificates \
        openssh-client \
        procps \
        bash \
    && rm -rf /var/lib/apt/lists/*

LABEL org.opencontainers.image.source="https://github.com/rakyll/ax-images"

# GitHub's actions runner, verified against the checksums published with
# the release. installdependencies.sh adds the libraries the runner's .NET
# runtime needs.
ARG TARGETARCH
ARG RUNNER_VERSION=2.338.2
ARG RUNNER_SHA256_X64=baebf204307bac6193480852732f0aba2ae97b6c5597c244f4d5558390147c40
ARG RUNNER_SHA256_ARM64=670adb2f268fe21dcbb58b3db0e0aa9d5b3bddfe2c651481336e561ff86de945
RUN set -eux; \
    case "${TARGETARCH}" in \
        amd64) arch=x64;   sum="${RUNNER_SHA256_X64}" ;; \
        arm64) arch=arm64; sum="${RUNNER_SHA256_ARM64}" ;; \
        *) echo "unsupported TARGETARCH ${TARGETARCH}" >&2; exit 1 ;; \
    esac; \
    tarball="actions-runner-linux-${arch}-${RUNNER_VERSION}.tar.gz"; \
    curl -fsSL -o "/tmp/${tarball}" "https://github.com/actions/runner/releases/download/v${RUNNER_VERSION}/${tarball}"; \
    echo "${sum}  /tmp/${tarball}" | sha256sum -c -; \
    mkdir -p /opt/actions-runner; \
    tar -xzf "/tmp/${tarball}" -C /opt/actions-runner; \
    rm "/tmp/${tarball}"; \
    apt-get update; \
    /opt/actions-runner/bin/installdependencies.sh; \
    rm -rf /var/lib/apt/lists/*

# The Makefile (or the release workflow) cross-compiles the binary into bin/
# before building the image. The same binary is the task runner when started
# as ax-task-runner, which runs /usr/local/bin/ax-agent when spec.command is
# empty.
COPY bin/linux_${TARGETARCH}/github-actions-runner /usr/local/bin/ax-agent
RUN ln -sf ax-agent /usr/local/bin/ax-task-runner

ENTRYPOINT ["/usr/local/bin/ax-task-runner"]
