// Copyright 2026 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package taskrunner

import (
	"slices"
	"testing"

	"github.com/google/ax/pkg/apis/v1alpha1"
)

func TestWithAgentCommand(t *testing.T) {
	for name, tc := range map[string]struct {
		command []string
		agent   string
		want    []string
	}{
		"empty command runs the agent": {nil, defaultAgent, []string{defaultAgent}},
		"explicit command is kept":     {[]string{"sh", "-c", "true"}, defaultAgent, []string{"sh", "-c", "true"}},
		"no agent leaves it empty":     {nil, "", nil},
	} {
		t.Run(name, func(t *testing.T) {
			task := &v1alpha1.Task{Spec: &v1alpha1.TaskSpec{Command: tc.command}}
			got := withAgentCommand(task, tc.agent).GetSpec().GetCommand()
			if !slices.Equal(got, tc.want) {
				t.Errorf("command = %q, want %q", got, tc.want)
			}
			if !slices.Equal(task.GetSpec().GetCommand(), tc.command) {
				t.Errorf("input task was modified: %q", task.GetSpec().GetCommand())
			}
		})
	}
}

func TestWithAgentCommand_NilSpec(t *testing.T) {
	got := withAgentCommand(&v1alpha1.Task{}, defaultAgent).GetSpec().GetCommand()
	if !slices.Equal(got, []string{defaultAgent}) {
		t.Errorf("command = %q, want %q", got, []string{defaultAgent})
	}
}
