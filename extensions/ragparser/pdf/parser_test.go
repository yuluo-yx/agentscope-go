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

package pdf

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"codeberg.org/go-pdf/fpdf"
	"github.com/yuluo-yx/agentscope-go/pkg/message"
	"github.com/yuluo-yx/agentscope-go/pkg/rag"
)

func TestParserExtractsOneSectionPerPage(t *testing.T) {
	parser := NewParser()
	data := testPDF(t)

	sections, err := parser.Parse(context.Background(), data, "sample.pdf")
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}

	if len(sections) != 2 {
		t.Fatalf("expected two sections, got %#v", sections)
	}
	first := sections[0].Content.(*message.TextBlock).Text
	second := sections[1].Content.(*message.TextBlock).Text
	if !strings.Contains(first, "First page text") {
		t.Fatalf("first page text mismatch: %q", first)
	}
	if !strings.Contains(second, "Second page text") {
		t.Fatalf("second page text mismatch: %q", second)
	}
	if sections[0].Source != "sample.pdf" || sections[0].Metadata["page"] != 1 {
		t.Fatalf("first section metadata mismatch: %#v", sections[0])
	}
	if sections[1].Source != "sample.pdf" || sections[1].Metadata["page"] != 2 {
		t.Fatalf("second section metadata mismatch: %#v", sections[1])
	}
}

func TestParserSupportedTypesAndExtensions(t *testing.T) {
	parser := NewParser()

	if !reflect.DeepEqual(parser.SupportedMediaTypes(), []string{"application/pdf"}) {
		t.Fatalf("supported media types mismatch: %#v", parser.SupportedMediaTypes())
	}
	if !reflect.DeepEqual(parser.SupportedExtensions(), []string{".pdf"}) {
		t.Fatalf("supported extensions mismatch: %#v", parser.SupportedExtensions())
	}
}

func TestParserRejectsInvalidInputs(t *testing.T) {
	parser := NewParser()
	canceled, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := parser.Parse(canceled, []byte("pdf"), "sample.pdf"); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled Parse should fail with context.Canceled, got %v", err)
	}
	if _, err := parser.Parse(context.Background(), []byte("pdf"), " "); !errors.Is(err, rag.ErrInvalidInput) {
		t.Fatalf("blank filename should fail, got %v", err)
	}
	if _, err := parser.Parse(context.Background(), nil, "sample.pdf"); !errors.Is(err, rag.ErrInvalidInput) {
		t.Fatalf("empty PDF should fail, got %v", err)
	}
	if _, err := parser.Parse(context.Background(), []byte("not a pdf"), "sample.pdf"); err == nil {
		t.Fatal("invalid PDF bytes should fail")
	}
}

func TestExtractContentTextBytesHandlesCommonTextOperators(t *testing.T) {
	stream := []byte(`BT /F1 12 Tf 72 720 Td (Hello\040PDF) Tj T* [(A) -20 (B)] TJ T* <feff0043> Tj ET`)

	got := extractContentTextBytes(stream)
	if got != "Hello PDF\nAB\nC" {
		t.Fatalf("extractContentTextBytes = %q", got)
	}
}

func TestContentTextHelpersCoverEscapesAndOperators(t *testing.T) {
	if text, err := extractContentText(nil); err != nil || text != "" {
		t.Fatalf("nil reader should return empty text, got %q err=%v", text, err)
	}
	if _, err := extractContentText(errorReader{}); err == nil {
		t.Fatal("reader error should be returned")
	}

	stream := []byte("% comment\nBT (Line1) ' (Line2) \" << /Ignored true >> [<41> (B\\nC)] TJ (D\\)E) Tj ET")
	got := extractContentTextBytes(stream)
	if !strings.Contains(got, "Line1\nLine2") ||
		!strings.Contains(got, "AB\nC") ||
		!strings.Contains(got, "D)E") {
		t.Fatalf("unexpected extracted text: %q", got)
	}

	operands := collapseArrayOperand([]contentOperand{{kind: "string", text: "x"}})
	if len(operands) != 1 || operands[0].text != "x" {
		t.Fatalf("collapse without array should preserve operands: %#v", operands)
	}
	var builder strings.Builder
	if applyTextOperator(&builder, nil, "noop") {
		t.Fatal("unknown text operator should return false")
	}
	appendLastText(&builder, []contentOperand{{kind: "token", text: "12"}}, true)
	if builder.String() != "" {
		t.Fatalf("appendLastText without string operands should not write, got %q", builder.String())
	}
	appendLineBreak(&builder)
	if builder.String() != "" {
		t.Fatalf("appendLineBreak on empty builder should not write, got %q", builder.String())
	}

	for _, tt := range []struct {
		name string
		data []byte
		want string
	}{
		{name: "newlines", data: []byte(`\n\r\t\b\f\(\)\\`), want: "\n\r\t\b\f()\\"},
		{name: "line continuation lf", data: []byte("\\\nnext"), want: "next"},
		{name: "line continuation crlf", data: []byte("\\\r\nnext"), want: "next"},
		{name: "unknown escape", data: []byte(`\x`), want: "x"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, _ := parseLiteralString(append([]byte{'('}, append(tt.data, ')')...), 1)
			if got != tt.want {
				t.Fatalf("parseLiteralString = %q, want %q", got, tt.want)
			}
		})
	}
	if decoded, _ := parseLiteralEscape(nil, 0); string(decoded) != "\\" {
		t.Fatalf("trailing literal escape = %q, want backslash", decoded)
	}
	if got, _ := parseHexString([]byte("4>"), 0); got != "@" {
		t.Fatalf("odd hex string = %q, want @", got)
	}
	if got, _ := parseHexString([]byte("zz>"), 0); got != "" {
		t.Fatalf("invalid hex string = %q, want empty", got)
	}
	if got := decodePDFString([]byte{0xff}); got != "ÿ" {
		t.Fatalf("PDFDocEncoding fallback mismatch: %q", got)
	}
}

func testPDF(t *testing.T) []byte {
	t.Helper()

	doc := fpdf.New("P", "mm", "A4", "")
	doc.SetCompression(false)
	doc.SetFont("Arial", "", 12)
	doc.AddPage()
	doc.Text(10, 10, "First page text")
	doc.AddPage()
	doc.Text(10, 10, "Second page text")

	var buf bytes.Buffer
	if err := doc.Output(&buf); err != nil {
		t.Fatalf("Output returned error: %v", err)
	}
	return buf.Bytes()
}

type errorReader struct{}

func (errorReader) Read([]byte) (int, error) {
	return 0, errors.New("read failed")
}
