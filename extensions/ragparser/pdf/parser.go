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

// Package pdf provides a PDF parser for AgentScope RAG.
package pdf

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	pdfcpuapi "github.com/pdfcpu/pdfcpu/pkg/api"
	pdfcpu "github.com/pdfcpu/pdfcpu/pkg/pdfcpu"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/yuluo-yx/agentscope-go/pkg/message"
	"github.com/yuluo-yx/agentscope-go/pkg/rag"
)

var (
	supportedMediaTypes = []string{"application/pdf"}
	supportedExtensions = []string{".pdf"}
)

// Parser parses PDF files into one text section per page.
type Parser struct{}

var _ rag.Parser = (*Parser)(nil)

// NewParser creates a PDF parser.
func NewParser() *Parser {
	return &Parser{}
}

// Parse extracts one text section per PDF page.
func (*Parser) Parse(ctx context.Context, data []byte, filename string) ([]rag.Section, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	filename = strings.TrimSpace(filename)
	if filename == "" {
		return nil, fmt.Errorf("%w: filename is required", rag.ErrInvalidInput)
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("%w: %q is empty", rag.ErrInvalidInput, filename)
	}

	conf := model.NewDefaultConfiguration()
	conf.Cmd = model.EXTRACTCONTENT
	pdfCtx, err := pdfcpuapi.ReadValidateAndOptimize(bytes.NewReader(data), conf)
	if err != nil {
		return nil, err
	}

	sections := make([]rag.Section, 0, pdfCtx.PageCount)
	for page := 1; page <= pdfCtx.PageCount; page++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		content, err := pdfcpu.ExtractPageContent(pdfCtx, page)
		if err != nil {
			return nil, err
		}
		text, err := extractContentText(content)
		if err != nil {
			return nil, err
		}
		sections = append(sections, rag.Section{
			Content:  message.NewTextBlock(text),
			Source:   filename,
			Metadata: map[string]any{"page": page},
		})
	}
	return sections, nil
}

// SupportedMediaTypes returns the IANA media types handled by Parser.
func (*Parser) SupportedMediaTypes() []string {
	return append([]string(nil), supportedMediaTypes...)
}

// SupportedExtensions returns filename extensions handled by Parser.
func (*Parser) SupportedExtensions() []string {
	return append([]string(nil), supportedExtensions...)
}

type contentOperand struct {
	kind string
	text string
}

func extractContentText(reader io.Reader) (string, error) {
	if reader == nil {
		return "", nil
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(extractContentTextBytes(data)), nil
}

func extractContentTextBytes(data []byte) string {
	var out strings.Builder
	operands := make([]contentOperand, 0, 8)
	for offset := 0; offset < len(data); {
		offset = skipSpaceAndComments(data, offset)
		if offset >= len(data) {
			break
		}

		switch data[offset] {
		case '(':
			text, next := parseLiteralString(data, offset+1)
			operands = append(operands, contentOperand{kind: "string", text: text})
			offset = next
		case '<':
			if offset+1 < len(data) && data[offset+1] == '<' {
				operands = append(operands, contentOperand{kind: "dict_start"})
				offset += 2
				continue
			}
			text, next := parseHexString(data, offset+1)
			operands = append(operands, contentOperand{kind: "string", text: text})
			offset = next
		case '[':
			operands = append(operands, contentOperand{kind: "array_start"})
			offset++
		case ']':
			operands = collapseArrayOperand(operands)
			offset++
		default:
			token, next := parseRegularToken(data, offset)
			offset = next
			if token == "" {
				continue
			}
			if applyTextOperator(&out, operands, token) {
				operands = operands[:0]
				continue
			}
			if isPDFOperandToken(token) {
				operands = append(operands, contentOperand{kind: "token", text: token})
				continue
			}
			operands = operands[:0]
		}
	}
	return out.String()
}

func collapseArrayOperand(operands []contentOperand) []contentOperand {
	index := len(operands) - 1
	for index >= 0 && operands[index].kind != "array_start" {
		index--
	}
	if index < 0 {
		return operands
	}

	var text strings.Builder
	for _, operand := range operands[index+1:] {
		if operand.kind == "string" {
			text.WriteString(operand.text)
		}
	}
	operands = operands[:index]
	return append(operands, contentOperand{kind: "array", text: text.String()})
}

func applyTextOperator(out *strings.Builder, operands []contentOperand, operator string) bool {
	switch operator {
	case "Tj":
		appendLastText(out, operands, false)
		return true
	case "TJ":
		appendLastText(out, operands, false)
		return true
	case "'":
		appendLastText(out, operands, true)
		return true
	case `"`:
		appendLastText(out, operands, true)
		return true
	case "T*", "Td", "TD":
		appendLineBreak(out)
		return true
	default:
		return false
	}
}

func appendLastText(out *strings.Builder, operands []contentOperand, lineBreak bool) {
	for index := len(operands) - 1; index >= 0; index-- {
		if operands[index].kind != "string" && operands[index].kind != "array" {
			continue
		}
		if lineBreak {
			appendLineBreak(out)
		}
		out.WriteString(operands[index].text)
		return
	}
}

func appendLineBreak(out *strings.Builder) {
	if out.Len() == 0 {
		return
	}
	current := out.String()
	if strings.HasSuffix(current, "\n") {
		return
	}
	out.WriteByte('\n')
}

func skipSpaceAndComments(data []byte, offset int) int {
	for offset < len(data) {
		switch data[offset] {
		case 0, '\t', '\n', '\f', '\r', ' ':
			offset++
		case '%':
			for offset < len(data) && data[offset] != '\n' && data[offset] != '\r' {
				offset++
			}
		default:
			return offset
		}
	}
	return offset
}

func parseLiteralString(data []byte, offset int) (string, int) {
	depth := 1
	out := make([]byte, 0)
	for offset < len(data) {
		ch := data[offset]
		offset++
		switch ch {
		case '(':
			depth++
			out = append(out, ch)
		case ')':
			depth--
			if depth == 0 {
				return decodePDFString(out), offset
			}
			out = append(out, ch)
		case '\\':
			decoded, next := parseLiteralEscape(data, offset)
			out = append(out, decoded...)
			offset = next
		default:
			out = append(out, ch)
		}
	}
	return decodePDFString(out), offset
}

func parseLiteralEscape(data []byte, offset int) ([]byte, int) {
	if offset >= len(data) {
		return []byte{'\\'}, offset
	}
	ch := data[offset]
	offset++
	switch ch {
	case 'n':
		return []byte{'\n'}, offset
	case 'r':
		return []byte{'\r'}, offset
	case 't':
		return []byte{'\t'}, offset
	case 'b':
		return []byte{'\b'}, offset
	case 'f':
		return []byte{'\f'}, offset
	case '(', ')', '\\':
		return []byte{ch}, offset
	case '\n':
		return nil, offset
	case '\r':
		if offset < len(data) && data[offset] == '\n' {
			offset++
		}
		return nil, offset
	default:
		if ch < '0' || ch > '7' {
			return []byte{ch}, offset
		}
		value := int(ch - '0')
		count := 1
		for offset < len(data) && count < 3 && data[offset] >= '0' && data[offset] <= '7' {
			value = value*8 + int(data[offset]-'0')
			offset++
			count++
		}
		return []byte{byte(value)}, offset
	}
}

func parseHexString(data []byte, offset int) (string, int) {
	var digits []byte
	for offset < len(data) {
		ch := data[offset]
		offset++
		if ch == '>' {
			break
		}
		if isPDFSpace(ch) {
			continue
		}
		digits = append(digits, ch)
	}
	if len(digits)%2 == 1 {
		digits = append(digits, '0')
	}
	decoded := make([]byte, hex.DecodedLen(len(digits)))
	if _, err := hex.Decode(decoded, digits); err != nil {
		return "", offset
	}
	return decodePDFString(decoded), offset
}

func parseRegularToken(data []byte, offset int) (string, int) {
	start := offset
	for offset < len(data) {
		if isPDFSpace(data[offset]) || isPDFDelimiter(data[offset]) {
			break
		}
		offset++
	}
	if start == offset {
		return string(data[offset : offset+1]), offset + 1
	}
	return string(data[start:offset]), offset
}

func isPDFOperandToken(token string) bool {
	if strings.HasPrefix(token, "/") {
		return true
	}
	_, err := strconv.ParseFloat(token, 64)
	return err == nil
}

func isPDFSpace(ch byte) bool {
	return ch == 0 || ch == '\t' || ch == '\n' || ch == '\f' || ch == '\r' || ch == ' '
}

func isPDFDelimiter(ch byte) bool {
	return strings.ContainsRune("()<>[]{}%", rune(ch))
}

func decodePDFString(data []byte) string {
	if len(data) >= 2 && data[0] == 0xfe && data[1] == 0xff {
		words := make([]uint16, 0, (len(data)-2)/2)
		for index := 2; index+1 < len(data); index += 2 {
			words = append(words, uint16(data[index])<<8|uint16(data[index+1]))
		}
		return string(utf16.Decode(words))
	}
	if utf8.Valid(data) {
		return string(data)
	}
	runes := make([]rune, 0, len(data))
	for _, value := range data {
		runes = append(runes, rune(value))
	}
	return string(runes)
}
