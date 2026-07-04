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
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
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

func TestParseFileReadsLocalFileWithBaseNameSource(t *testing.T) {
	parser := rag.NewTextParser()
	path := filepath.Join(t.TempDir(), "guide.md")
	if err := os.WriteFile(path, []byte("hello from disk"), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	sections, err := rag.ParseFile(context.Background(), parser, path)
	if err != nil {
		t.Fatalf("ParseFile returned error: %v", err)
	}

	if len(sections) != 1 || sections[0].Source != "guide.md" {
		t.Fatalf("sections mismatch: %#v", sections)
	}
	if got := sections[0].Content.(*message.TextBlock).Text; got != "hello from disk" {
		t.Fatalf("section text = %q", got)
	}
}

func TestImageParserWrapsImageBytesAsDataBlock(t *testing.T) {
	parser := rag.NewImageParser()
	data := []byte("\x89PNG\r\n\x1a\npng-data")

	sections, err := parser.Parse(context.Background(), data, "diagram.png")
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}

	if len(sections) != 1 {
		t.Fatalf("expected one section, got %#v", sections)
	}
	block, ok := sections[0].Content.(*message.DataBlock)
	if !ok {
		t.Fatalf("section content should be DataBlock, got %T", sections[0].Content)
	}
	source, ok := block.Source.(*message.Base64Source)
	if !ok {
		t.Fatalf("data source should be Base64Source, got %T", block.Source)
	}
	if source.MediaType != "image/png" {
		t.Fatalf("media type = %q, want image/png", source.MediaType)
	}
	if source.Data != base64.StdEncoding.EncodeToString(data) {
		t.Fatalf("base64 data mismatch: %q", source.Data)
	}
	if block.Name == nil || *block.Name != "diagram.png" {
		t.Fatalf("data block name mismatch: %#v", block.Name)
	}
	if sections[0].Source != "diagram.png" || sections[0].Metadata["media_type"] != "image/png" {
		t.Fatalf("section metadata mismatch: %#v", sections[0])
	}
}

func TestImageParserSupportedTypesAndExtensions(t *testing.T) {
	parser := rag.NewImageParser()

	if !reflect.DeepEqual(parser.SupportedMediaTypes(), []string{
		"image/png",
		"image/jpeg",
		"image/gif",
		"image/bmp",
		"image/webp",
	}) {
		t.Fatalf("supported media types mismatch: %#v", parser.SupportedMediaTypes())
	}
	if !reflect.DeepEqual(parser.SupportedExtensions(), []string{
		".bmp",
		".gif",
		".jpeg",
		".jpg",
		".png",
		".webp",
	}) {
		t.Fatalf("supported extensions mismatch: %#v", parser.SupportedExtensions())
	}
}
