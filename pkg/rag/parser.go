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

package rag

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/yuluo-yx/agentscope-go/pkg/message"
)

var (
	textParserMediaTypes = []string{
		"text/plain",
		"text/markdown",
		"text/csv",
		"text/html",
		"text/x-rst",
		"application/json",
		"application/xml",
		"application/x-yaml",
	}
	textParserExtensions = []string{
		".csv",
		".htm",
		".html",
		".json",
		".markdown",
		".md",
		".rst",
		".txt",
		".xml",
		".yaml",
		".yml",
	}
)

// Parser turns raw file bytes into logical sections.
type Parser interface {
	Parse(ctx context.Context, data []byte, filename string) ([]Section, error)
	SupportedMediaTypes() []string
	SupportedExtensions() []string
}

// TextParser parses UTF-8 text formats into one unstructured section.
type TextParser struct{}

// NewTextParser creates a UTF-8 text parser.
func NewTextParser() *TextParser {
	return &TextParser{}
}

// Parse decodes UTF-8 bytes and returns one text section.
func (*TextParser) Parse(ctx context.Context, data []byte, filename string) ([]Section, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	filename = strings.TrimSpace(filename)
	if filename == "" {
		return nil, fmt.Errorf("%w: filename is required", ErrInvalidInput)
	}
	if !utf8.Valid(data) {
		return nil, fmt.Errorf("%w: %q is not valid UTF-8", ErrInvalidInput, filename)
	}
	return []Section{
		{
			Content:  message.NewTextBlock(string(data)),
			Source:   filename,
			Metadata: map[string]any{},
		},
	}, nil
}

// SupportedMediaTypes returns the IANA media types handled by TextParser.
func (*TextParser) SupportedMediaTypes() []string {
	return append([]string(nil), textParserMediaTypes...)
}

// SupportedExtensions returns common filename extensions handled by TextParser.
func (*TextParser) SupportedExtensions() []string {
	return append([]string(nil), textParserExtensions...)
}
