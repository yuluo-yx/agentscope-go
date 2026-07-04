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

// Package pptx provides a PowerPoint parser for AgentScope RAG.
package pptx

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
	"regexp"
	"sort"
	"strings"

	"github.com/yuluo-yx/agentscope-go/pkg/message"
	"github.com/yuluo-yx/agentscope-go/pkg/rag"
)

var (
	supportedMediaTypes = []string{
		"application/vnd.openxmlformats-officedocument.presentationml.presentation",
	}
	supportedExtensions = []string{".pptx"}
	slideFilePattern    = regexp.MustCompile(`^ppt/slides/slide([0-9]+)\.xml$`)
)

// TableFormat controls how PPTX tables are rendered as text sections.
type TableFormat string

const (
	// TableFormatMarkdown renders tables as markdown tables.
	TableFormatMarkdown TableFormat = "markdown"
	// TableFormatJSON renders tables as JSON arrays with a system-info prefix.
	TableFormatJSON TableFormat = "json"
)

// Parser parses PPTX files into slide-scoped text and image sections.
type Parser struct {
	includeImages  bool
	separateTables bool
	tableFormat    TableFormat
	slidePrefix    *string
	slideSuffix    *string
}

// Option configures Parser.
type Option func(*Parser)

var _ rag.Parser = (*Parser)(nil)

// NewParser creates a PowerPoint parser.
func NewParser(opts ...Option) *Parser {
	prefix := "<slide index={index}>"
	suffix := "</slide>"
	parser := &Parser{
		includeImages: true,
		tableFormat:   TableFormatMarkdown,
		slidePrefix:   &prefix,
		slideSuffix:   &suffix,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(parser)
		}
	}
	return parser
}

// WithImages controls whether embedded slide images become DataBlock sections.
func WithImages(enabled bool) Option {
	return func(parser *Parser) {
		parser.includeImages = enabled
	}
}

// WithSeparateTables controls whether PPTX tables become standalone text sections.
func WithSeparateTables(enabled bool) Option {
	return func(parser *Parser) {
		parser.separateTables = enabled
	}
}

// WithTableFormat sets the format used when rendering PPTX tables.
func WithTableFormat(format TableFormat) Option {
	return func(parser *Parser) {
		parser.tableFormat = format
	}
}

// WithSlidePrefix sets the prefix added to each slide's first text section.
func WithSlidePrefix(prefix string) Option {
	return func(parser *Parser) {
		parser.slidePrefix = &prefix
	}
}

// WithoutSlidePrefix disables slide text prefixes.
func WithoutSlidePrefix() Option {
	return func(parser *Parser) {
		parser.slidePrefix = nil
	}
}

// WithSlideSuffix sets the suffix added to each slide's last text section.
func WithSlideSuffix(suffix string) Option {
	return func(parser *Parser) {
		parser.slideSuffix = &suffix
	}
}

// WithoutSlideSuffix disables slide text suffixes.
func WithoutSlideSuffix() Option {
	return func(parser *Parser) {
		parser.slideSuffix = nil
	}
}

// Parse extracts slide text and embedded images from a PPTX file.
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
		return nil, fmt.Errorf("%w: unsupported PPTX table format %q", rag.ErrInvalidInput, p.tableFormat)
	}

	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, err
	}
	files := indexZipFiles(reader.File)
	slidePaths := orderedSlidePaths(files)

	sections := make([]rag.Section, 0, len(slidePaths))
	for index, slidePath := range slidePaths {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		slideData, err := readZipFile(files[slidePath])
		if err != nil {
			return nil, err
		}
		relationships, err := slideRelationships(files, slidePath)
		if err != nil {
			return nil, err
		}
		slideSections, err := p.parseSlide(files, slideData, relationships, filename, index+1)
		if err != nil {
			return nil, err
		}
		sections = append(sections, slideSections...)
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

func (p *Parser) parseSlide(
	files map[string]*zip.File,
	data []byte,
	relationships map[string]string,
	filename string,
	slideNo int,
) ([]rag.Section, error) {
	state := &slideParseState{
		parser:        p,
		files:         files,
		relationships: relationships,
		filename:      filename,
		slideNo:       slideNo,
	}
	if p.slidePrefix != nil {
		state.textParts = append(state.textParts, strings.ReplaceAll(*p.slidePrefix, "{index}", fmt.Sprintf("%d", slideNo)))
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
		switch typed := token.(type) {
		case xml.StartElement:
			switch typed.Name.Local {
			case "tbl":
				state.flushParagraph()
				table, err := collectTable(decoder, typed)
				if err != nil {
					return nil, err
				}
				if err := state.appendTable(table); err != nil {
					return nil, err
				}
			case "p":
				state.startParagraph()
			case "t":
				var text string
				if err := decoder.DecodeElement(&text, &typed); err != nil {
					return nil, err
				}
				state.appendText(text)
			case "blip":
				if p.includeImages {
					state.flushParagraph()
					if err := state.appendImage(relationshipID(typed.Attr)); err != nil {
						return nil, err
					}
				}
			}
		case xml.EndElement:
			if typed.Name.Local == "p" {
				state.flushParagraph()
			}
		}
	}
	state.flushParagraph()
	state.finishText()
	return state.sections, nil
}

type slideParseState struct {
	parser        *Parser
	files         map[string]*zip.File
	relationships map[string]string
	filename      string
	slideNo       int
	tableNo       int
	paragraph     strings.Builder
	inParagraph   bool
	textParts     []string
	sections      []rag.Section
}

func (s *slideParseState) startParagraph() {
	s.flushParagraph()
	s.inParagraph = true
}

func (s *slideParseState) appendText(text string) {
	if s.inParagraph {
		s.paragraph.WriteString(text)
		return
	}
	s.textParts = append(s.textParts, text)
}

func (s *slideParseState) flushParagraph() {
	if !s.inParagraph {
		return
	}
	text := strings.TrimSpace(s.paragraph.String())
	if text != "" {
		s.textParts = append(s.textParts, text)
	}
	s.paragraph.Reset()
	s.inParagraph = false
}

func (s *slideParseState) flushText() {
	if len(s.textParts) == 0 {
		return
	}
	text := strings.TrimSpace(strings.Join(s.textParts, "\n"))
	if text != "" {
		s.sections = append(s.sections, rag.Section{
			Content:  message.NewTextBlock(text),
			Source:   s.filename,
			Metadata: map[string]any{"slide": s.slideNo},
		})
	}
	s.textParts = nil
}

func (s *slideParseState) appendTable(rows [][]string) error {
	text, err := formatTable(rows, s.parser.tableFormat)
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
	s.tableNo++
	s.sections = append(s.sections, rag.Section{
		Content: message.NewTextBlock(text),
		Source:  s.filename,
		Metadata: map[string]any{
			"slide": s.slideNo,
			"type":  "table",
			"index": s.tableNo,
		},
	})
	return nil
}

func (s *slideParseState) finishText() {
	if s.parser.slideSuffix != nil {
		suffix := strings.ReplaceAll(*s.parser.slideSuffix, "{index}", fmt.Sprintf("%d", s.slideNo))
		if len(s.textParts) > 0 {
			s.textParts = append(s.textParts, suffix)
			s.flushText()
			return
		}
		for index := len(s.sections) - 1; index >= 0; index-- {
			if s.sections[index].Metadata["type"] == "table" {
				continue
			}
			block, ok := s.sections[index].Content.(*message.TextBlock)
			if !ok {
				continue
			}
			block.Text = strings.TrimSpace(block.Text + "\n" + suffix)
			return
		}
	}
	s.flushText()
}

func (s *slideParseState) appendImage(relationshipID string) error {
	target := s.relationships[relationshipID]
	if target == "" {
		return nil
	}
	file := s.files[target]
	if file == nil {
		return nil
	}
	data, err := readZipFile(file)
	if err != nil {
		return err
	}
	mediaType := guessImageMediaType(data)
	s.flushText()
	s.sections = append(s.sections, rag.Section{
		Content: message.NewDataBlock(
			message.NewBase64Source(base64.StdEncoding.EncodeToString(data), mediaType),
			message.WithDataBlockName(path.Base(target)),
		),
		Source: s.filename,
		Metadata: map[string]any{
			"slide":      s.slideNo,
			"media_type": mediaType,
		},
	})
	return nil
}

func collectTable(decoder *xml.Decoder, start xml.StartElement) ([][]string, error) {
	rows := [][]string{}
	row := []string{}
	cellParts := []string{}
	inRow := false
	inCell := false
	for {
		token, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		switch typed := token.(type) {
		case xml.StartElement:
			switch typed.Name.Local {
			case "tr":
				inRow = true
				row = nil
			case "tc":
				if inRow {
					inCell = true
					cellParts = nil
				}
			case "t":
				var text string
				if err := decoder.DecodeElement(&text, &typed); err != nil {
					return nil, err
				}
				if inCell {
					cellParts = append(cellParts, text)
				}
			}
		case xml.EndElement:
			switch typed.Name.Local {
			case "tc":
				if inCell {
					row = append(row, strings.TrimSpace(strings.Join(cellParts, "")))
					cellParts = nil
					inCell = false
				}
			case "tr":
				if inRow {
					rows = append(rows, row)
					row = nil
					inRow = false
				}
			case start.Name.Local:
				return rows, nil
			}
		}
	}
}

func formatTable(rows [][]string, format TableFormat) (string, error) {
	rows = normalizeTableRows(rows)
	if len(rows) == 0 {
		return "", nil
	}
	switch format {
	case TableFormatMarkdown:
		return formatMarkdownTable(rows), nil
	case TableFormatJSON:
		data, err := json.Marshal(rows)
		if err != nil {
			return "", err
		}
		return "<system-info>A table loaded as a JSON array:</system-info>\n" + string(data), nil
	default:
		return "", fmt.Errorf("%w: unsupported PPTX table format %q", rag.ErrInvalidInput, format)
	}
}

func normalizeTableRows(rows [][]string) [][]string {
	width := 0
	normalized := make([][]string, 0, len(rows))
	for _, row := range rows {
		cells := make([]string, 0, len(row))
		hasText := false
		for _, cell := range row {
			text := strings.TrimSpace(cell)
			if text != "" {
				hasText = true
			}
			cells = append(cells, text)
		}
		if !hasText {
			continue
		}
		if len(cells) > width {
			width = len(cells)
		}
		normalized = append(normalized, cells)
	}
	if width == 0 {
		return nil
	}
	for index := range normalized {
		for len(normalized[index]) < width {
			normalized[index] = append(normalized[index], "")
		}
	}
	return normalized
}

func formatMarkdownTable(rows [][]string) string {
	var builder strings.Builder
	writeMarkdownTableRow(&builder, rows[0])
	builder.WriteString("\n")
	separator := make([]string, len(rows[0]))
	for index := range separator {
		separator[index] = "---"
	}
	writeMarkdownTableRow(&builder, separator)
	for _, row := range rows[1:] {
		builder.WriteString("\n")
		writeMarkdownTableRow(&builder, row)
	}
	return builder.String()
}

func writeMarkdownTableRow(builder *strings.Builder, row []string) {
	builder.WriteString("| ")
	for index, cell := range row {
		if index > 0 {
			builder.WriteString(" | ")
		}
		builder.WriteString(escapeMarkdownCell(cell))
	}
	builder.WriteString(" |")
}

func escapeMarkdownCell(cell string) string {
	return strings.ReplaceAll(cell, "|", `\|`)
}

func validTableFormat(format TableFormat) bool {
	return format == TableFormatMarkdown || format == TableFormatJSON
}

func indexZipFiles(files []*zip.File) map[string]*zip.File {
	out := make(map[string]*zip.File, len(files))
	for _, file := range files {
		out[file.Name] = file
	}
	return out
}

func orderedSlidePaths(files map[string]*zip.File) []string {
	relationships, _ := parseRelationships(files["ppt/_rels/presentation.xml.rels"], "ppt")
	if presentation := files["ppt/presentation.xml"]; presentation != nil {
		data, err := readZipFile(presentation)
		if err == nil {
			if paths := slidePathsFromPresentation(data, relationships); len(paths) > 0 {
				return paths
			}
		}
	}

	type slideFile struct {
		path  string
		index int
	}
	slideFiles := []slideFile{}
	for name := range files {
		matches := slideFilePattern.FindStringSubmatch(name)
		if len(matches) != 2 {
			continue
		}
		var number int
		fmt.Sscanf(matches[1], "%d", &number)
		slideFiles = append(slideFiles, slideFile{path: name, index: number})
	}
	sort.SliceStable(slideFiles, func(i, j int) bool {
		return slideFiles[i].index < slideFiles[j].index
	})
	out := make([]string, 0, len(slideFiles))
	for _, file := range slideFiles {
		out = append(out, file.path)
	}
	return out
}

func slidePathsFromPresentation(data []byte, relationships map[string]string) []string {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	paths := []string{}
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil
		}
		start, ok := token.(xml.StartElement)
		if !ok || start.Name.Local != "sldId" {
			continue
		}
		target := relationships[relationshipID(start.Attr)]
		if target != "" {
			paths = append(paths, target)
		}
	}
	return paths
}

func slideRelationships(files map[string]*zip.File, slidePath string) (map[string]string, error) {
	relsPath := path.Join(path.Dir(slidePath), "_rels", path.Base(slidePath)+".rels")
	return parseRelationships(files[relsPath], path.Dir(slidePath))
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
		if id == "" || target == "" || strings.Contains(target, "://") {
			continue
		}
		out[id] = path.Clean(path.Join(baseDir, target))
	}
	return out, nil
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
