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

package mcp

// Feature identifies MCP behaviors that are intentionally documented at the
// boundary where AgentScope Go currently matches or diverges from Python.
type Feature string

const (
	FeatureOAuthAuth                   Feature = "oauth_auth"
	FeatureToolListChangedNotification Feature = "tool_list_changed_notification"
	FeatureDeferredLoading             Feature = "deferred_loading"
	FeatureTaskAugmentedTools          Feature = "task_augmented_tools"
)

// FeatureStatus describes whether a documented MCP behavior is implemented.
type FeatureStatus string

const (
	FeatureStatusSupported   FeatureStatus = "supported"
	FeatureStatusPartial     FeatureStatus = "partial"
	FeatureStatusUnsupported FeatureStatus = "unsupported"
)

// FeatureBoundary records the implementation status and operational boundary
// for one MCP behavior.
type FeatureBoundary struct {
	Feature Feature       `json:"feature"`
	Status  FeatureStatus `json:"status"`
	Detail  string        `json:"detail"`
}

// CapabilityBoundaries returns the explicit MCP feature boundary matrix.
func CapabilityBoundaries() map[Feature]FeatureBoundary {
	return map[Feature]FeatureBoundary{
		FeatureOAuthAuth: {
			Feature: FeatureOAuthAuth,
			Status:  FeatureStatusPartial,
			Detail:  "OAuth/Auth supports static HTTP headers, gateway bearer tokens, and runtime OAuthConfig passthrough for HTTP MCP transports; OAuth token stores are not serialized into workspace .mcp indexes.",
		},
		FeatureToolListChangedNotification: {
			Feature: FeatureToolListChangedNotification,
			Status:  FeatureStatusPartial,
			Detail:  "The client observes tools/list_changed notifications, clears cached raw tools, and can invoke a callback; streamable HTTP global notifications require continuous listening and already-built eager toolkits must still be refreshed explicitly.",
		},
		FeatureDeferredLoading: {
			Feature: FeatureDeferredLoading,
			Status:  FeatureStatusUnsupported,
			Detail:  "Toolkit MCP integration eagerly performs explicit ListTools during construction rather than deferring schema loading.",
		},
		FeatureTaskAugmentedTools: {
			Feature: FeatureTaskAugmentedTools,
			Status:  FeatureStatusUnsupported,
			Detail:  "Task-augmented MCP tools are not implemented; wrapped tools use the normal CallTool request/response path.",
		},
	}
}
