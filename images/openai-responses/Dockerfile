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

# Task image for a coding agent on the OpenAI Responses API.
# See images/openai-responses/README.md.
#
#   make push-openai-responses IMAGE_REPO=<your-registry>

# The same packages as AX's default ax-task-runner image, which is built on
# python:3.12-slim. The workspace setup clones repositories with git, and
# agents use the shell tools.
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

# The Makefile (or the release workflow) cross-compiles the agent into bin/
# before building the image. The same binary is the task runner when started
# as ax-task-runner, which runs /usr/local/bin/ax-agent when spec.command is
# empty.
ARG TARGETARCH
COPY bin/linux_${TARGETARCH}/openai-responses /usr/local/bin/ax-agent
RUN ln -sf ax-agent /usr/local/bin/ax-task-runner

ENTRYPOINT ["/usr/local/bin/ax-task-runner"]
