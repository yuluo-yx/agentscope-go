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

package openai

import (
	"embed"

	asmodel "github.com/yuluo-yx/agentscope-go/model"
)

//go:embed models/*.yaml
var chatModelFS embed.FS

// ListModels returns embedded OpenAI Chat Completions model cards.
func ListModels() ([]asmodel.ModelCard, error) {
	return asmodel.LoadModelCardsFSWithDefaults(chatModelFS, "models", asmodel.NewModelCardDefaults(defaultProviderName, asmodel.ModelCapabilities{
		asmodel.ModelCapabilityTools:            true,
		asmodel.ModelCapabilityStructuredOutput: false,
		asmodel.ModelCapabilityEmbedding:        false,
		asmodel.ModelCapabilityGeneration:       true,
	}, map[string]any{"api": "chat_completions"}))
}
