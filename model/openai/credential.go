// Copyright 20\d\d AgentScope Go
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
	"fmt"
	"strings"
)

// Credential stores auth data for OpenAI or OpenAI-compatible endpoints.
type Credential struct {
	APIKey       string
	Organization string
	BaseURL      string
}

// CredentialOption configures OpenAI credentials.
type CredentialOption func(*Credential)

// NewCredential creates OpenAI credentials.
func NewCredential(apiKey string, opts ...CredentialOption) Credential {
	credential := Credential{APIKey: apiKey}
	for _, opt := range opts {
		opt(&credential)
	}
	credential.BaseURL = normalizeBaseURL(credential.BaseURL)
	return credential
}

// WithBaseURL sets the base URL for an OpenAI-compatible endpoint.
func WithBaseURL(baseURL string) CredentialOption {
	return func(credential *Credential) {
		credential.BaseURL = normalizeBaseURL(baseURL)
	}
}

// WithOrganization sets the OpenAI organization ID.
func WithOrganization(organization string) CredentialOption {
	return func(credential *Credential) {
		credential.Organization = organization
	}
}

// Validate validates credentials.
func (c Credential) Validate() error {
	if c.APIKey == "" {
		return fmt.Errorf("openai: api key is empty")
	}
	return nil
}

func normalizeBaseURL(baseURL string) string {
	return strings.TrimRight(strings.TrimSpace(baseURL), "/")
}
