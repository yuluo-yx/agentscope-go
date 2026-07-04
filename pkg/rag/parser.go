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
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
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
	imageParserMediaTypes = []string{
		"image/png",
		"image/jpeg",
		"image/gif",
		"image/bmp",
		"image/webp",
	}
	imageParserExtensions = []string{
		".bmp",
		".gif",
		".jpeg",
		".jpg",
		".png",
		".webp",
	}
)

// Parser turns raw file bytes into logical sections.
type Parser interface {
	Parse(ctx context.Context, data []byte, filename string) ([]Section, error)
	SupportedMediaTypes() []string
	SupportedExtensions() []string
}

// ParseFile reads a local file and parses it with the filename's base name as the source.
func ParseFile(ctx context.Context, parser Parser, path string) ([]Section, error) {
	return ParseFileAs(ctx, parser, path, filepath.Base(strings.TrimSpace(path)))
}

// ParseFileAs reads a local file and parses it with an explicit source filename.
func ParseFileAs(ctx context.Context, parser Parser, path string, filename string) ([]Section, error) {
	if parser == nil {
		return nil, fmt.Errorf("%w: parser is nil", ErrInvalidInput)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("%w: file path is required", ErrInvalidInput)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return parser.Parse(ctx, data, filename)
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

// ImageParser wraps image bytes as a single DataBlock section.
type ImageParser struct{}

// NewImageParser creates a parser for common image formats.
func NewImageParser() *ImageParser {
	return &ImageParser{}
}

// Parse returns one section containing the base64-encoded image bytes.
func (*ImageParser) Parse(ctx context.Context, data []byte, filename string) ([]Section, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	filename = strings.TrimSpace(filename)
	if filename == "" {
		return nil, fmt.Errorf("%w: filename is required", ErrInvalidInput)
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("%w: %q is empty", ErrInvalidInput, filename)
	}

	mediaType := guessImageMediaType(data)
	block := message.NewDataBlock(
		message.NewBase64Source(base64.StdEncoding.EncodeToString(data), mediaType),
		message.WithDataBlockName(filename),
	)
	return []Section{
		{
			Content:  block,
			Source:   filename,
			Metadata: map[string]any{"media_type": mediaType},
		},
	}, nil
}

// SupportedMediaTypes returns the IANA media types handled by ImageParser.
func (*ImageParser) SupportedMediaTypes() []string {
	return append([]string(nil), imageParserMediaTypes...)
}

// SupportedExtensions returns common filename extensions handled by ImageParser.
func (*ImageParser) SupportedExtensions() []string {
	return append([]string(nil), imageParserExtensions...)
}

func guessImageMediaType(data []byte) string {
	switch {
	case bytesHasPrefix(data, []byte("\x89PNG\r\n\x1a\n")):
		return "image/png"
	case bytesHasPrefix(data, []byte("\xff\xd8")):
		return "image/jpeg"
	case bytesHasPrefix(data, []byte("GIF87a")), bytesHasPrefix(data, []byte("GIF89a")):
		return "image/gif"
	case bytesHasPrefix(data, []byte("BM")):
		return "image/bmp"
	case len(data) > 12 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WEBP":
		return "image/webp"
	default:
		return "image/jpeg"
	}
}

func bytesHasPrefix(data []byte, prefix []byte) bool {
	if len(data) < len(prefix) {
		return false
	}
	for index, value := range prefix {
		if data[index] != value {
			return false
		}
	}
	return true
}
