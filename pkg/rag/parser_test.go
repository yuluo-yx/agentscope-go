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

package rag_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/yuluo-yx/agentscope-go/pkg/message"
	"github.com/yuluo-yx/agentscope-go/pkg/rag"
)

func TestTextParserParsesUTF8BytesIntoSingleSection(t *testing.T) {
	parser := rag.NewTextParser()

	sections, err := parser.Parse(context.Background(), []byte("# Guide\n\nHello."), "guide.md")
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}

	if len(sections) != 1 {
		t.Fatalf("expected one section, got %#v", sections)
	}
	text, ok := sections[0].Content.(*message.TextBlock)
	if !ok {
		t.Fatalf("section content should be TextBlock, got %T", sections[0].Content)
	}
	if text.Text != "# Guide\n\nHello." || sections[0].Source != "guide.md" || len(sections[0].Metadata) != 0 {
		t.Fatalf("section mismatch: %#v", sections[0])
	}
}

func TestTextParserRejectsInvalidInput(t *testing.T) {
	parser := rag.NewTextParser()

	_, err := parser.Parse(context.Background(), []byte("content"), "")
	if !errors.Is(err, rag.ErrInvalidInput) {
		t.Fatalf("empty filename should return ErrInvalidInput, got %v", err)
	}
	_, err = parser.Parse(context.Background(), []byte{0xff, 0xfe}, "bad.txt")
	if !errors.Is(err, rag.ErrInvalidInput) {
		t.Fatalf("invalid UTF-8 should return ErrInvalidInput, got %v", err)
	}
}

func TestTextParserSupportedTypesAndExtensions(t *testing.T) {
	parser := rag.NewTextParser()

	if !reflect.DeepEqual(parser.SupportedMediaTypes(), []string{
		"text/plain",
		"text/markdown",
		"text/csv",
		"text/html",
		"text/x-rst",
		"application/json",
		"application/xml",
		"application/x-yaml",
	}) {
		t.Fatalf("supported media types mismatch: %#v", parser.SupportedMediaTypes())
	}
	if !reflect.DeepEqual(parser.SupportedExtensions(), []string{
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
	}) {
		t.Fatalf("supported extensions mismatch: %#v", parser.SupportedExtensions())
	}
}
