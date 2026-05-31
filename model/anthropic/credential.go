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

package anthropic

import (
	"fmt"
	"strings"
)

// Credential stores Anthropic provider auth and endpoint configuration.
type Credential struct {
	APIKey  string
	BaseURL string
}

// CredentialOption configures Anthropic credentials.
type CredentialOption func(*Credential)

// NewCredential creates Anthropic credentials.
func NewCredential(apiKey string, opts ...CredentialOption) Credential {
	credential := Credential{APIKey: strings.TrimSpace(apiKey)}
	for _, opt := range opts {
		opt(&credential)
	}
	credential.BaseURL = normalizeBaseURL(credential.BaseURL)
	return credential
}

// WithBaseURL sets the Anthropic API base URL, mainly for compatible endpoints and tests.
func WithBaseURL(baseURL string) CredentialOption {
	return func(credential *Credential) {
		credential.BaseURL = normalizeBaseURL(baseURL)
	}
}

// Validate checks whether credentials can create an SDK client.
func (c Credential) Validate() error {
	if strings.TrimSpace(c.APIKey) == "" {
		return fmt.Errorf("api key is empty")
	}
	return nil
}

func normalizeBaseURL(baseURL string) string {
	return strings.TrimRight(strings.TrimSpace(baseURL), "/")
}
