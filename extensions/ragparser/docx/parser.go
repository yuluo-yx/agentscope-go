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
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"path"
	"strconv"
	"strings"

	"github.com/yuluo-yx/agentscope-go/pkg/message"
	"github.com/yuluo-yx/agentscope-go/pkg/rag"
)

const (
	maxArchiveEntries          = 10_000
	maxArchiveUncompressedSize = 512 << 20
	maxPartSize                = 64 << 20
	maxTableColumns            = 16_384
)

var (
	supportedMediaTypes = []string{
		"application/vnd.openxmlformats-officedocument.wordprocessingml.document",
	}
	supportedExtensions = []string{".docx"}
)

// TableFormat controls how DOCX tables are rendered as text.
type TableFormat string

const (
	// TableFormatMarkdown renders tables as Markdown pipe tables.
	TableFormatMarkdown TableFormat = "markdown"
	// TableFormatJSON renders tables as JSON arrays with a system-info prefix.
	TableFormatJSON TableFormat = "json"
)

// Parser parses DOCX files into merged text and image sections.
type Parser struct {
	includeImages  bool
	includeTables  bool
	separateTables bool
	tableFormat    TableFormat
}

// Option configures Parser.
type Option func(*Parser)

var _ rag.Parser = (*Parser)(nil)

// NewParser creates a Word DOCX parser.
func NewParser(opts ...Option) *Parser {
	parser := &Parser{
		includeImages: true,
		includeTables: true,
		tableFormat:   TableFormatMarkdown,
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

// WithSeparateTables controls whether each Word table becomes a standalone text section.
func WithSeparateTables(enabled bool) Option {
	return func(parser *Parser) {
		parser.separateTables = enabled
	}
}

// WithTableFormat sets the format used when rendering Word tables.
func WithTableFormat(format TableFormat) Option {
	return func(parser *Parser) {
		parser.tableFormat = format
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
	if !validTableFormat(p.tableFormat) {
		return nil, fmt.Errorf("%w: unsupported docx table format %q", rag.ErrInvalidInput, p.tableFormat)
	}

	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("open docx archive: %w", err)
	}
	files, err := indexZipFiles(reader.File)
	if err != nil {
		return nil, err
	}
	document := files["word/document.xml"]
	if document == nil {
		return nil, fmt.Errorf("%w: %q does not contain word/document.xml", rag.ErrInvalidInput, filename)
	}
	documentData, err := readZipFile(document)
	if err != nil {
		return nil, fmt.Errorf("read word/document.xml: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	relationships, err := parseRelationships(ctx, files["word/_rels/document.xml.rels"], "word")
	if err != nil {
		return nil, fmt.Errorf("parse document relationships: %w", err)
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
		ctx:           ctx,
		parser:        p,
		files:         files,
		relationships: relationships,
		filename:      filename,
	}
	decoder := xml.NewDecoder(bytes.NewReader(data))
	inBody := false
	sawBody := false
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
					sawBody = true
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
				content, err := collectParagraph(ctx, decoder, typed)
				if err != nil {
					return nil, fmt.Errorf("parse docx paragraph: %w", err)
				}
				if err := state.appendParagraph(content); err != nil {
					return nil, err
				}
			case "tbl":
				content, err := collectTable(ctx, decoder, typed)
				if err != nil {
					return nil, fmt.Errorf("parse docx table: %w", err)
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
	if !sawBody {
		return nil, fmt.Errorf("%w: docx document body is missing", rag.ErrInvalidInput)
	}
	state.flushText()
	return state.sections, nil
}

type documentParseState struct {
	ctx           context.Context
	parser        *Parser
	files         map[string]*zip.File
	relationships map[string]string
	filename      string
	textParts     []string
	sections      []rag.Section
}

func (s *documentParseState) appendParagraph(content blockContent) error {
	text := strings.TrimSpace(content.text)
	if text != "" {
		s.textParts = append(s.textParts, text)
	}
	if !s.parser.includeImages || len(content.imageIDs) == 0 {
		return nil
	}
	s.flushText()
	return s.appendImages(content.imageIDs)
}

func (s *documentParseState) appendTable(content tableContent) error {
	if !s.parser.includeTables {
		return nil
	}
	text, err := formatTable(content.rows, s.parser.tableFormat)
	if err != nil {
		return err
	}
	if text == "" {
		return nil
	}
	if !s.parser.separateTables {
		s.textParts = append(s.textParts, text)
		return nil
	}
	s.flushText()
	s.sections = append(s.sections, rag.Section{
		Content:  message.NewTextBlock(text),
		Source:   s.filename,
		Metadata: map[string]any{},
	})
	return nil
}

func (s *documentParseState) flushText() {
	if len(s.textParts) == 0 {
		return
	}
	s.sections = append(s.sections, rag.Section{
		Content:  message.NewTextBlock(strings.Join(s.textParts, "\n")),
		Source:   s.filename,
		Metadata: map[string]any{},
	})
	s.textParts = nil
}

func (s *documentParseState) appendImages(imageIDs []string) error {
	if !s.parser.includeImages {
		return nil
	}
	for _, id := range imageIDs {
		if err := s.ctx.Err(); err != nil {
			return err
		}
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
		s.sections = append(s.sections, rag.Section{
			Content: message.NewDataBlock(
				message.NewBase64Source(base64.StdEncoding.EncodeToString(data), mediaType),
				message.WithDataBlockName(s.filename),
			),
			Source: s.filename,
			Metadata: map[string]any{
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
	rows [][]string
}

func collectParagraph(ctx context.Context, decoder *xml.Decoder, start xml.StartElement) (blockContent, error) {
	var builder strings.Builder
	content := blockContent{}
	depth := 1
	for depth > 0 {
		if err := ctx.Err(); err != nil {
			return content, err
		}
		token, err := decoder.Token()
		if err != nil {
			return content, err
		}
		switch typed := token.(type) {
		case xml.StartElement:
			switch typed.Name.Local {
			case "t":
				text, err := decodeText(decoder, typed)
				if err != nil {
					return content, err
				}
				builder.WriteString(text)
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

func collectTable(ctx context.Context, decoder *xml.Decoder, start xml.StartElement) (tableContent, error) {
	content := tableContent{}
	for {
		if err := ctx.Err(); err != nil {
			return content, err
		}
		token, err := decoder.Token()
		if err != nil {
			return content, err
		}
		switch typed := token.(type) {
		case xml.StartElement:
			if typed.Name.Local != "tr" {
				if err := decoder.Skip(); err != nil {
					return content, err
				}
				continue
			}
			row, err := collectTableRow(ctx, decoder, typed)
			if err != nil {
				return content, err
			}
			content.rows = append(content.rows, row)
		case xml.EndElement:
			if typed.Name == start.Name {
				return content, nil
			}
		}
	}
}

func collectTableRow(ctx context.Context, decoder *xml.Decoder, start xml.StartElement) ([]string, error) {
	var row []string
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		token, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		switch typed := token.(type) {
		case xml.StartElement:
			if typed.Name.Local != "tc" {
				if err := decoder.Skip(); err != nil {
					return nil, err
				}
				continue
			}
			cell, span, err := collectTableCell(ctx, decoder, typed)
			if err != nil {
				return nil, err
			}
			if len(row)+span > maxTableColumns {
				return nil, fmt.Errorf("%w: docx table exceeds %d columns", rag.ErrInvalidInput, maxTableColumns)
			}
			row = append(row, cell)
			row = append(row, make([]string, span-1)...)
		case xml.EndElement:
			if typed.Name == start.Name {
				return row, nil
			}
		}
	}
}

func collectTableCell(ctx context.Context, decoder *xml.Decoder, start xml.StartElement) (string, int, error) {
	paragraphs := make([]string, 0, 1)
	span := 1
	for {
		if err := ctx.Err(); err != nil {
			return "", 0, err
		}
		token, err := decoder.Token()
		if err != nil {
			return "", 0, err
		}
		switch typed := token.(type) {
		case xml.StartElement:
			switch typed.Name.Local {
			case "tcPr":
				span, err = collectGridSpan(ctx, decoder, typed)
				if err != nil {
					return "", 0, err
				}
			case "p":
				paragraph, err := collectParagraph(ctx, decoder, typed)
				if err != nil {
					return "", 0, err
				}
				if paragraph.text != "" {
					paragraphs = append(paragraphs, paragraph.text)
				}
			default:
				if err := decoder.Skip(); err != nil {
					return "", 0, err
				}
			}
		case xml.EndElement:
			if typed.Name == start.Name {
				return strings.Join(paragraphs, "\n"), span, nil
			}
		}
	}
}

func collectGridSpan(ctx context.Context, decoder *xml.Decoder, start xml.StartElement) (int, error) {
	span := 1
	found := false
	depth := 1
	for depth > 0 {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		token, err := decoder.Token()
		if err != nil {
			return 0, err
		}
		switch typed := token.(type) {
		case xml.StartElement:
			if typed.Name.Local != "gridSpan" || found {
				depth++
				continue
			}
			found = true
			value := attrValue(typed.Attr, "val")
			if value == "" {
				value = "1"
			}
			parsed, err := strconv.Atoi(value)
			if err != nil || parsed < 1 || parsed > maxTableColumns {
				return 0, fmt.Errorf("%w: invalid docx gridSpan value %q", rag.ErrInvalidInput, value)
			}
			span = parsed
			if err := decoder.Skip(); err != nil {
				return 0, err
			}
		case xml.EndElement:
			depth--
		}
	}
	return span, nil
}

func decodeText(decoder *xml.Decoder, start xml.StartElement) (string, error) {
	var text string
	if err := decoder.DecodeElement(&text, &start); err != nil {
		return "", err
	}
	return text, nil
}

func formatTable(rows [][]string, format TableFormat) (string, error) {
	switch format {
	case TableFormatMarkdown:
		return formatMarkdownTable(rows), nil
	case TableFormatJSON:
		return formatJSONTable(rows)
	default:
		return "", fmt.Errorf("%w: unsupported docx table format %q", rag.ErrInvalidInput, format)
	}
}

func formatMarkdownTable(rows [][]string) string {
	if len(rows) == 0 || len(rows[0]) == 0 {
		return ""
	}
	width := len(rows[0])
	var builder strings.Builder
	writeMarkdownRow(&builder, rows[0], width)
	builder.WriteByte('\n')
	separator := make([]string, width)
	for index := range separator {
		separator[index] = "---"
	}
	writeMarkdownRow(&builder, separator, width)
	for _, row := range rows[1:] {
		builder.WriteByte('\n')
		writeMarkdownRow(&builder, row, width)
	}
	builder.WriteByte('\n')
	return builder.String()
}

func writeMarkdownRow(builder *strings.Builder, row []string, width int) {
	builder.WriteString("| ")
	for index := 0; index < width; index++ {
		if index > 0 {
			builder.WriteString(" | ")
		}
		if index < len(row) {
			builder.WriteString(row[index])
		}
	}
	builder.WriteString(" |")
}

func formatJSONTable(rows [][]string) (string, error) {
	var builder strings.Builder
	builder.WriteString("<system-info>A table loaded as a JSON array:</system-info>\n[")
	for rowIndex, row := range rows {
		if rowIndex > 0 {
			builder.WriteString(", ")
		}
		builder.WriteByte('[')
		for columnIndex, cell := range row {
			if columnIndex > 0 {
				builder.WriteString(", ")
			}
			var encoded bytes.Buffer
			encoder := json.NewEncoder(&encoded)
			encoder.SetEscapeHTML(false)
			if err := encoder.Encode(cell); err != nil {
				return "", fmt.Errorf("encode docx table cell: %w", err)
			}
			builder.WriteString(strings.TrimSuffix(encoded.String(), "\n"))
		}
		builder.WriteByte(']')
	}
	builder.WriteByte(']')
	return builder.String(), nil
}

func validTableFormat(format TableFormat) bool {
	return format == TableFormatMarkdown || format == TableFormatJSON
}

func indexZipFiles(files []*zip.File) (map[string]*zip.File, error) {
	if len(files) > maxArchiveEntries {
		return nil, fmt.Errorf("%w: docx archive contains too many entries", rag.ErrInvalidInput)
	}
	out := make(map[string]*zip.File, len(files))
	var totalSize uint64
	for _, file := range files {
		if file.FileInfo().IsDir() {
			continue
		}
		name, ok := cleanArchivePath(file.Name)
		if !ok {
			return nil, fmt.Errorf("%w: unsafe docx archive path %q", rag.ErrInvalidInput, file.Name)
		}
		if _, exists := out[name]; exists {
			return nil, fmt.Errorf("%w: duplicate docx archive path %q", rag.ErrInvalidInput, name)
		}
		if file.UncompressedSize64 > maxArchiveUncompressedSize-totalSize {
			return nil, fmt.Errorf("%w: docx archive is too large", rag.ErrInvalidInput)
		}
		totalSize += file.UncompressedSize64
		out[name] = file
	}
	return out, nil
}

func cleanArchivePath(name string) (string, bool) {
	name = strings.ReplaceAll(name, "\\", "/")
	if name == "" || strings.HasPrefix(name, "/") {
		return "", false
	}
	name = path.Clean(name)
	if name == "." || name == ".." || strings.HasPrefix(name, "../") {
		return "", false
	}
	return name, true
}

func parseRelationships(ctx context.Context, file *zip.File, baseDir string) (map[string]string, error) {
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
		start, ok := token.(xml.StartElement)
		if !ok || start.Name.Local != "Relationship" {
			continue
		}
		id := attrValue(start.Attr, "Id")
		target := attrValue(start.Attr, "Target")
		targetMode := attrValue(start.Attr, "TargetMode")
		if id == "" || target == "" || strings.EqualFold(targetMode, "External") || strings.Contains(target, "://") {
			continue
		}
		if target = relationshipTargetPath(baseDir, target); target != "" {
			out[id] = target
		}
	}
	return out, nil
}

func relationshipTargetPath(baseDir string, target string) string {
	target = strings.ReplaceAll(target, "\\", "/")
	var joined string
	if strings.HasPrefix(target, "/") {
		joined = strings.TrimLeft(target, "/")
	} else {
		joined = path.Join(baseDir, target)
	}
	cleaned, ok := cleanArchivePath(joined)
	if !ok {
		return ""
	}
	return cleaned
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
	if file == nil {
		return nil, fmt.Errorf("%w: docx archive part is missing", rag.ErrInvalidInput)
	}
	if file.UncompressedSize64 > maxPartSize {
		return nil, fmt.Errorf("%w: docx archive part %q is too large", rag.ErrInvalidInput, file.Name)
	}
	reader, err := file.Open()
	if err != nil {
		return nil, err
	}
	data, readErr := io.ReadAll(io.LimitReader(reader, maxPartSize+1))
	closeErr := reader.Close()
	if readErr != nil {
		return nil, readErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	if len(data) > maxPartSize {
		return nil, fmt.Errorf("%w: docx archive part %q is too large", rag.ErrInvalidInput, file.Name)
	}
	return data, nil
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
