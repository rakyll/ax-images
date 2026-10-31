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

# Pinned to the same digest the AX examples use so runner behavior is
# reproducible. Override with --build-arg BASE_IMAGE=... (or BASE_IMAGE= for
# the Makefile) to build on another image.
ARG BASE_IMAGE=gcr.io/ax-substrate/ate-images/ax-task-runner@sha256:3a0dea6ad8b55278685db58aca6e37dc4ba04056831d45bef3aaeafdca43cac6
FROM ${BASE_IMAGE}

# The Makefile cross-compiles the agent into bin/ before building the image.
# The same binary is the task runner when started as ax-task-runner, which
# runs /usr/local/bin/ax-agent when spec.command is empty.
ARG TARGETARCH=amd64
COPY bin/linux_${TARGETARCH}/openai-responses /usr/local/bin/ax-agent
RUN ln -sf ax-agent /usr/local/bin/ax-task-runner

ENTRYPOINT ["/usr/local/bin/ax-task-runner"]
