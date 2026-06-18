// Copyright The AgentScope Go Authors
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

package dashscope

import (
	"embed"

	"github.com/yuluo-yx/agentscope-go/audio/stt"
)

//go:embed models/*.yaml
var modelFS embed.FS

// ListModels returns embedded DashScope STT model cards.
func ListModels() ([]stt.ModelCard, error) {
	return stt.LoadModelCardsFS(modelFS, "models", stt.CommonParameterSchema())
}
