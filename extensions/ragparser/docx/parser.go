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

// Package docx provides a Word DOCX parser for AgentScope RAG.
package docx

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/xml"
	"fmt"
	"io"
	"path"
	"strings"

	"github.com/yuluo-yx/agentscope-go/pkg/message"
	"github.com/yuluo-yx/agentscope-go/pkg/rag"
)

var (
	supportedMediaTypes = []string{
		"application/vnd.openxmlformats-officedocument.wordprocessingml.document",
	}
	supportedExtensions = []string{".docx"}
)

// Parser parses DOCX files into paragraph, table, and image sections.
type Parser struct {
	includeImages bool
	includeTables bool
}

// Option configures Parser.
type Option func(*Parser)

var _ rag.Parser = (*Parser)(nil)

// NewParser creates a Word DOCX parser.
func NewParser(opts ...Option) *Parser {
	parser := &Parser{
		includeImages: true,
		includeTables: true,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(parser)
		}
	}
	return parser
}

// WithImages controls whether embedded document images become DataBlock sections.
func WithImages(enabled bool) Option {
	return func(parser *Parser) {
		parser.includeImages = enabled
	}
}

// WithTables controls whether Word tables become text sections.
func WithTables(enabled bool) Option {
	return func(parser *Parser) {
		parser.includeTables = enabled
	}
}

// Parse extracts document body text, tables, and embedded images from a DOCX file.
func (p *Parser) Parse(ctx context.Context, data []byte, filename string) ([]rag.Section, error) {
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
	if p == nil {
		p = NewParser()
	}

	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, err
	}
	files := indexZipFiles(reader.File)
	document := files["word/document.xml"]
	if document == nil {
		return nil, fmt.Errorf("%w: %q does not contain word/document.xml", rag.ErrInvalidInput, filename)
	}
	documentData, err := readZipFile(document)
	if err != nil {
		return nil, err
	}
	relationships, err := parseRelationships(files["word/_rels/document.xml.rels"], "word")
	if err != nil {
		return nil, err
	}
	return p.parseDocument(ctx, files, relationships, documentData, filename)
}

// SupportedMediaTypes returns the IANA media types handled by Parser.
func (*Parser) SupportedMediaTypes() []string {
	return append([]string(nil), supportedMediaTypes...)
}

// SupportedExtensions returns filename extensions handled by Parser.
func (*Parser) SupportedExtensions() []string {
	return append([]string(nil), supportedExtensions...)
}

func (p *Parser) parseDocument(
	ctx context.Context,
	files map[string]*zip.File,
	relationships map[string]string,
	data []byte,
	filename string,
) ([]rag.Section, error) {
	state := &documentParseState{
		parser:        p,
		files:         files,
		relationships: relationships,
		filename:      filename,
	}
	decoder := xml.NewDecoder(bytes.NewReader(data))
	inBody := false
	bodyDepth := 0
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		switch typed := token.(type) {
		case xml.StartElement:
			if !inBody {
				if typed.Name.Local == "body" {
					inBody = true
					bodyDepth = 1
				}
				continue
			}
			if bodyDepth != 1 {
				bodyDepth++
				continue
			}
			switch typed.Name.Local {
			case "p":
				content, err := collectParagraph(decoder, typed)
				if err != nil {
					return nil, err
				}
				if err := state.appendParagraph(content); err != nil {
					return nil, err
				}
			case "tbl":
				content, err := collectTable(decoder, typed)
				if err != nil {
					return nil, err
				}
				if err := state.appendTable(content); err != nil {
					return nil, err
				}
			default:
				if err := decoder.Skip(); err != nil {
					return nil, err
				}
			}
		case xml.EndElement:
			if inBody {
				bodyDepth--
				if bodyDepth == 0 {
					inBody = false
				}
			}
		}
	}
	return state.sections, nil
}

type documentParseState struct {
	parser        *Parser
	files         map[string]*zip.File
	relationships map[string]string
	filename      string
	paragraphNo   int
	tableNo       int
	imageNo       int
	sections      []rag.Section
}

func (s *documentParseState) appendParagraph(content blockContent) error {
	text := strings.TrimSpace(content.text)
	if text != "" {
		s.paragraphNo++
		s.sections = append(s.sections, rag.Section{
			Content: message.NewTextBlock(text),
			Source:  s.filename,
			Metadata: map[string]any{
				"type":  "paragraph",
				"index": s.paragraphNo,
			},
		})
	}
	return s.appendImages(content.imageIDs)
}

func (s *documentParseState) appendTable(content tableContent) error {
	if s.parser.includeTables {
		text := formatTable(content.rows)
		if text != "" {
			s.tableNo++
			s.sections = append(s.sections, rag.Section{
				Content: message.NewTextBlock(text),
				Source:  s.filename,
				Metadata: map[string]any{
					"type":  "table",
					"index": s.tableNo,
				},
			})
		}
	}
	return s.appendImages(content.imageIDs)
}

func (s *documentParseState) appendImages(imageIDs []string) error {
	if !s.parser.includeImages {
		return nil
	}
	for _, id := range imageIDs {
		target := s.relationships[id]
		if target == "" {
			continue
		}
		file := s.files[target]
		if file == nil {
			continue
		}
		data, err := readZipFile(file)
		if err != nil {
			return err
		}
		mediaType := guessImageMediaType(data)
		s.imageNo++
		s.sections = append(s.sections, rag.Section{
			Content: message.NewDataBlock(
				message.NewBase64Source(base64.StdEncoding.EncodeToString(data), mediaType),
				message.WithDataBlockName(path.Base(target)),
			),
			Source: s.filename,
			Metadata: map[string]any{
				"type":       "image",
				"index":      s.imageNo,
				"media_type": mediaType,
			},
		})
	}
	return nil
}

type blockContent struct {
	text     string
	imageIDs []string
}

type tableContent struct {
	rows     [][]string
	imageIDs []string
}

func collectParagraph(decoder *xml.Decoder, start xml.StartElement) (blockContent, error) {
	var builder strings.Builder
	content := blockContent{}
	depth := 1
	for depth > 0 {
		token, err := decoder.Token()
		if err != nil {
			return content, err
		}
		switch typed := token.(type) {
		case xml.StartElement:
			switch typed.Name.Local {
			case "t", "delText":
				text, err := decodeText(decoder, typed)
				if err != nil {
					return content, err
				}
				builder.WriteString(text)
			case "tab":
				builder.WriteByte('\t')
				if err := decoder.Skip(); err != nil {
					return content, err
				}
			case "br", "cr":
				builder.WriteByte('\n')
				if err := decoder.Skip(); err != nil {
					return content, err
				}
			case "blip", "imagedata":
				if id := relationshipID(typed.Attr); id != "" {
					content.imageIDs = append(content.imageIDs, id)
				}
				if err := decoder.Skip(); err != nil {
					return content, err
				}
			default:
				depth++
			}
		case xml.EndElement:
			depth--
		}
	}
	content.text = builder.String()
	return content, nil
}

func collectTable(decoder *xml.Decoder, start xml.StartElement) (tableContent, error) {
	content := tableContent{}
	var cell strings.Builder
	var row []string
	inRow := false
	inCell := false
	depth := 1
	for depth > 0 {
		token, err := decoder.Token()
		if err != nil {
			return content, err
		}
		switch typed := token.(type) {
		case xml.StartElement:
			switch typed.Name.Local {
			case "tr":
				if !inRow {
					inRow = true
					row = nil
				}
				depth++
			case "tc":
				if !inCell {
					inCell = true
					cell.Reset()
				}
				depth++
			case "t", "delText":
				text, err := decodeText(decoder, typed)
				if err != nil {
					return content, err
				}
				if inCell {
					cell.WriteString(text)
				}
			case "tab":
				if inCell {
					cell.WriteByte('\t')
				}
				if err := decoder.Skip(); err != nil {
					return content, err
				}
			case "br", "cr":
				if inCell {
					cell.WriteByte('\n')
				}
				if err := decoder.Skip(); err != nil {
					return content, err
				}
			case "blip", "imagedata":
				if id := relationshipID(typed.Attr); id != "" {
					content.imageIDs = append(content.imageIDs, id)
				}
				if err := decoder.Skip(); err != nil {
					return content, err
				}
			default:
				depth++
			}
		case xml.EndElement:
			switch typed.Name.Local {
			case "p":
				if inCell && strings.TrimSpace(cell.String()) != "" {
					cell.WriteByte('\n')
				}
			case "tc":
				if inCell {
					row = append(row, normalizeTableCell(cell.String()))
					cell.Reset()
					inCell = false
				}
			case "tr":
				if inRow {
					if len(row) > 0 {
						content.rows = append(content.rows, row)
					}
					row = nil
					inRow = false
				}
			}
			depth--
		}
	}
	return content, nil
}

func decodeText(decoder *xml.Decoder, start xml.StartElement) (string, error) {
	var text string
	if err := decoder.DecodeElement(&text, &start); err != nil {
		return "", err
	}
	return text, nil
}

func formatTable(rows [][]string) string {
	lines := make([]string, 0, len(rows))
	for _, row := range rows {
		if len(row) == 0 {
			continue
		}
		cells := make([]string, 0, len(row))
		hasText := false
		for _, cell := range row {
			if cell != "" {
				hasText = true
			}
			cells = append(cells, strings.ReplaceAll(cell, "|", `\|`))
		}
		if hasText {
			lines = append(lines, "| "+strings.Join(cells, " | ")+" |")
		}
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func normalizeTableCell(text string) string {
	return strings.Join(strings.Fields(text), " ")
}

func indexZipFiles(files []*zip.File) map[string]*zip.File {
	out := make(map[string]*zip.File, len(files))
	for _, file := range files {
		out[strings.ReplaceAll(file.Name, "\\", "/")] = file
	}
	return out
}

func parseRelationships(file *zip.File, baseDir string) (map[string]string, error) {
	out := map[string]string{}
	if file == nil {
		return out, nil
	}
	data, err := readZipFile(file)
	if err != nil {
		return nil, err
	}
	decoder := xml.NewDecoder(bytes.NewReader(data))
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		start, ok := token.(xml.StartElement)
		if !ok || start.Name.Local != "Relationship" {
			continue
		}
		id := attrValue(start.Attr, "Id")
		target := attrValue(start.Attr, "Target")
		targetMode := attrValue(start.Attr, "TargetMode")
		if id == "" || target == "" || targetMode == "External" || strings.Contains(target, "://") {
			continue
		}
		out[id] = relationshipTargetPath(baseDir, target)
	}
	return out, nil
}

func relationshipTargetPath(baseDir string, target string) string {
	if strings.HasPrefix(target, "/") {
		return path.Clean(strings.TrimPrefix(target, "/"))
	}
	return path.Clean(path.Join(baseDir, target))
}

func relationshipID(attrs []xml.Attr) string {
	for _, attr := range attrs {
		if attr.Name.Local == "id" && strings.Contains(attr.Name.Space, "relationships") {
			return attr.Value
		}
		if attr.Name.Local == "embed" || attr.Name.Local == "link" {
			return attr.Value
		}
	}
	return ""
}

func attrValue(attrs []xml.Attr, local string) string {
	for _, attr := range attrs {
		if attr.Name.Local == local {
			return attr.Value
		}
	}
	return ""
}

func readZipFile(file *zip.File) ([]byte, error) {
	reader, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	return io.ReadAll(reader)
}

func guessImageMediaType(data []byte) string {
	switch {
	case bytes.HasPrefix(data, []byte("\x89PNG\r\n\x1a\n")):
		return "image/png"
	case bytes.HasPrefix(data, []byte("\xff\xd8")):
		return "image/jpeg"
	case bytes.HasPrefix(data, []byte("GIF87a")), bytes.HasPrefix(data, []byte("GIF89a")):
		return "image/gif"
	case bytes.HasPrefix(data, []byte("BM")):
		return "image/bmp"
	case len(data) > 12 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WEBP":
		return "image/webp"
	default:
		return "image/jpeg"
	}
}
