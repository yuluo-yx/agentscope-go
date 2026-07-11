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

// Package excel provides an Excel .xlsx/.xls parser for AgentScope RAG.
package excel

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/extrame/xls"
	"github.com/xuri/excelize/v2"
	"github.com/yuluo-yx/agentscope-go/pkg/message"
	"github.com/yuluo-yx/agentscope-go/pkg/rag"
)

var (
	supportedMediaTypes = []string{
		"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
		"application/vnd.ms-excel",
	}
	supportedExtensions = []string{".xls", ".xlsx"}
)

// TableFormat controls the text format used to render tables.
type TableFormat string

const (
	// TableFormatMarkdown renders tables as Markdown pipe tables.
	TableFormatMarkdown TableFormat = "markdown"
	// TableFormatJSON renders one JSON value per row with a system-info marker.
	TableFormatJSON TableFormat = "json"
)

// Parser converts Excel worksheets into text and image sections.
type Parser struct {
	includeSheetNames      bool
	includeCellCoordinates bool
	includeImages          bool
	separateSheets         bool
	tableFormat            TableFormat
}

// Option configures Parser.
type Option func(*Parser)

var _ rag.Parser = (*Parser)(nil)

// NewParser creates an Excel parser.
func NewParser(opts ...Option) *Parser {
	parser := &Parser{
		includeSheetNames: true,
		tableFormat:       TableFormatMarkdown,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(parser)
		}
	}
	return parser
}

// WithSheetNames controls whether each table is prefixed with its worksheet name.
func WithSheetNames(enabled bool) Option {
	return func(parser *Parser) {
		parser.includeSheetNames = enabled
	}
}

// WithCellCoordinates controls whether cells include A1-style coordinates.
func WithCellCoordinates(enabled bool) Option {
	return func(parser *Parser) {
		parser.includeCellCoordinates = enabled
	}
}

// WithImages controls whether embedded images are extracted.
//
// excelize supports images in .xlsx files. extrame/xls does not expose images
// from .xls files, so those files still return table content without image sections.
func WithImages(enabled bool) Option {
	return func(parser *Parser) {
		parser.includeImages = enabled
	}
}

// WithSeparateSheets controls whether each worksheet keeps a separate text section.
func WithSeparateSheets(enabled bool) Option {
	return func(parser *Parser) {
		parser.separateSheets = enabled
	}
}

// WithTableFormat sets the table text format.
func WithTableFormat(format TableFormat) Option {
	return func(parser *Parser) {
		parser.tableFormat = format
	}
}

// Parse processes Excel data in worksheet order.
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
		return nil, fmt.Errorf("%w: unsupported excel table format %q", rag.ErrInvalidInput, p.tableFormat)
	}

	format, err := detectWorkbookFormat(data, filename)
	if err != nil {
		return nil, err
	}
	var sections []rag.Section
	switch format {
	case workbookFormatXLSX:
		sections, err = p.parseXLSX(ctx, data, filename)
	case workbookFormatXLS:
		sections, err = p.parseXLS(ctx, data, filename)
	}
	if err != nil {
		return nil, err
	}
	if p.separateSheets {
		return sections, nil
	}
	return mergeTextSections(sections, filename), nil
}

// SupportedMediaTypes returns the IANA media types handled by Parser.
func (*Parser) SupportedMediaTypes() []string {
	return append([]string(nil), supportedMediaTypes...)
}

// SupportedExtensions returns filename extensions handled by Parser.
func (*Parser) SupportedExtensions() []string {
	return append([]string(nil), supportedExtensions...)
}

type workbookFormat uint8

const (
	workbookFormatUnknown workbookFormat = iota
	workbookFormatXLSX
	workbookFormatXLS
)

func detectWorkbookFormat(data []byte, filename string) (workbookFormat, error) {
	extension := strings.ToLower(filepath.Ext(filename))
	switch extension {
	case ".xlsx":
		if hasAnyPrefix(data, []byte("PK\x03\x04"), []byte("PK\x05\x06"), []byte("PK\x07\x08")) {
			return workbookFormatXLSX, nil
		}
	case ".xls":
		if bytes.HasPrefix(data, []byte("\xd0\xcf\x11\xe0\xa1\xb1\x1a\xe1")) {
			return workbookFormatXLS, nil
		}
	default:
		return workbookFormatUnknown, fmt.Errorf("%w: unsupported excel extension %q", rag.ErrInvalidInput, extension)
	}
	return workbookFormatUnknown, fmt.Errorf(
		"%w: %q content does not match extension %q",
		rag.ErrInvalidInput,
		filename,
		extension,
	)
}

func hasAnyPrefix(data []byte, prefixes ...[]byte) bool {
	for _, prefix := range prefixes {
		if bytes.HasPrefix(data, prefix) {
			return true
		}
	}
	return false
}

func validTableFormat(format TableFormat) bool {
	return format == TableFormatMarkdown || format == TableFormatJSON
}

func (p *Parser) parseXLSX(ctx context.Context, data []byte, filename string) ([]rag.Section, error) {
	workbook, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("opening xlsx workbook: %w", err)
	}

	sections, parseErr := p.parseXLSXSheets(ctx, workbook, filename)
	closeErr := workbook.Close()
	if parseErr != nil {
		return nil, parseErr
	}
	if closeErr != nil {
		return nil, fmt.Errorf("closing xlsx workbook: %w", closeErr)
	}
	return sections, nil
}

func (p *Parser) parseXLSXSheets(
	ctx context.Context,
	workbook *excelize.File,
	filename string,
) ([]rag.Section, error) {
	sections := make([]rag.Section, 0, len(workbook.GetSheetList()))
	for _, sheetName := range workbook.GetSheetList() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		rows, err := workbook.GetRows(sheetName)
		if err != nil {
			continue
		}
		table := normalizeTable(rows)
		if len(table) < 2 {
			continue
		}
		text := p.renderTable(table, sheetName)
		sections = append(sections, newTextSection(text, filename, sheetName))

		if !p.includeImages {
			continue
		}
		images, err := xlsxImageSections(ctx, workbook, sheetName, filename)
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return nil, ctxErr
			}
			continue
		}
		sections = append(sections, images...)
	}
	return sections, nil
}

func (p *Parser) parseXLS(ctx context.Context, data []byte, filename string) ([]rag.Section, error) {
	workbook, err := openXLS(data)
	if err != nil {
		return nil, err
	}
	sections := make([]rag.Section, 0, workbook.NumSheets())
	for index := 0; index < workbook.NumSheets(); index++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		sheet, err := getXLSSheet(workbook, index)
		if err != nil || sheet == nil {
			continue
		}
		rows, err := readXLSSheet(ctx, sheet)
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return nil, ctxErr
			}
			continue
		}
		table := normalizeTable(rows)
		if len(table) < 2 {
			continue
		}
		text := p.renderTable(table, sheet.Name)
		sections = append(sections, newTextSection(text, filename, sheet.Name))
	}
	return sections, nil
}

func openXLS(data []byte) (workbook *xls.WorkBook, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			workbook = nil
			err = fmt.Errorf("opening xls workbook: %v", recovered)
		}
	}()
	workbook, err = xls.OpenReader(bytes.NewReader(data), "utf-8")
	if err != nil {
		return nil, fmt.Errorf("opening xls workbook: %w", err)
	}
	if workbook == nil {
		return nil, fmt.Errorf("%w: xls workbook is invalid", rag.ErrInvalidInput)
	}
	return workbook, nil
}

func getXLSSheet(workbook *xls.WorkBook, index int) (sheet *xls.WorkSheet, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			sheet = nil
			err = fmt.Errorf("reading xls sheet %d: %v", index, recovered)
		}
	}()
	return workbook.GetSheet(index), nil
}

func readXLSSheet(ctx context.Context, sheet *xls.WorkSheet) ([][]string, error) {
	rows := make([][]string, int(sheet.MaxRow)+1)
	for rowIndex := 0; rowIndex <= int(sheet.MaxRow); rowIndex++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		row, ok := readXLSRow(sheet, rowIndex)
		if ok {
			rows[rowIndex] = row
		}
	}
	return rows, nil
}

// extrame/xls dereferences nil in WorkSheet.Row for sparse rows. Contain that
// dependency panic at the boundary and treat the affected row as missing.
func readXLSRow(sheet *xls.WorkSheet, rowIndex int) (values []string, ok bool) {
	defer func() {
		if recover() != nil {
			values = nil
			ok = false
		}
	}()
	row := sheet.Row(rowIndex)
	if row == nil {
		return nil, false
	}
	lastColumn := row.LastCol()
	if lastColumn <= 0 {
		return []string{}, true
	}
	values = make([]string, lastColumn)
	for column := 0; column < lastColumn; column++ {
		values[column] = row.Col(column)
	}
	return values, true
}

func normalizeTable(rows [][]string) [][]string {
	lastRow := -1
	lastColumn := -1
	normalized := make([][]string, len(rows))
	for rowIndex, row := range rows {
		normalized[rowIndex] = make([]string, len(row))
		for columnIndex, cell := range row {
			cell = normalizeCell(cell)
			normalized[rowIndex][columnIndex] = cell
			if cell != "" {
				lastRow = rowIndex
				lastColumn = max(lastColumn, columnIndex)
			}
		}
	}
	if lastRow < 1 || lastColumn < 0 {
		return nil
	}
	normalized = normalized[:lastRow+1]
	for rowIndex, row := range normalized {
		padded := make([]string, lastColumn+1)
		copy(padded, row)
		normalized[rowIndex] = padded
	}
	return normalized
}

func normalizeCell(cell string) string {
	cell = strings.TrimSpace(cell)
	return strings.ReplaceAll(strings.ReplaceAll(cell, "\r\n", "\n"), "\r", "\n")
}

func (p *Parser) renderTable(table [][]string, sheetName string) string {
	if p.tableFormat == TableFormatJSON {
		return p.renderJSON(table, sheetName)
	}
	return p.renderMarkdown(table, sheetName)
}

func (p *Parser) renderMarkdown(table [][]string, sheetName string) string {
	var builder strings.Builder
	if p.includeSheetNames {
		builder.WriteString("Sheet: ")
		builder.WriteString(sheetName)
		builder.WriteByte('\n')
	}
	writeMarkdownRow(&builder, p.formatRow(table[0], 0))
	separator := make([]string, len(table[0]))
	for index := range separator {
		separator[index] = "---"
	}
	writeMarkdownRow(&builder, separator)
	for rowIndex, row := range table[1:] {
		writeMarkdownRow(&builder, p.formatRow(row, rowIndex+1))
	}
	return builder.String()
}

func writeMarkdownRow(builder *strings.Builder, row []string) {
	builder.WriteString("| ")
	for index, cell := range row {
		if index > 0 {
			builder.WriteString(" | ")
		}
		builder.WriteString(cell)
	}
	builder.WriteString(" |\n")
}

func (p *Parser) renderJSON(table [][]string, sheetName string) string {
	parts := make([]string, 0, len(table)+2)
	if p.includeSheetNames {
		parts = append(parts, "Sheet: "+sheetName)
	}
	parts = append(parts, "<system-info>A table loaded as a JSON array:</system-info>")
	for rowIndex, row := range table {
		parts = append(parts, encodeJSONRow(row, rowIndex, p.includeCellCoordinates))
	}
	return strings.Join(parts, "\n")
}

func encodeJSONRow(row []string, rowIndex int, includeCoordinates bool) string {
	var builder strings.Builder
	if includeCoordinates {
		builder.WriteByte('{')
	} else {
		builder.WriteByte('[')
	}
	for columnIndex, cell := range row {
		if columnIndex > 0 {
			builder.WriteString(", ")
		}
		if includeCoordinates {
			builder.WriteString(encodeJSONString(cellCoordinate(rowIndex, columnIndex)))
			builder.WriteString(": ")
		}
		builder.WriteString(encodeJSONString(cell))
	}
	if includeCoordinates {
		builder.WriteByte('}')
	} else {
		builder.WriteByte(']')
	}
	return builder.String()
}

func encodeJSONString(value string) string {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		// bytes.Buffer and string cannot fail encoding; keep the fallback valid JSON.
		return `""`
	}
	return strings.TrimSuffix(buffer.String(), "\n")
}

func (p *Parser) formatRow(row []string, rowIndex int) []string {
	formatted := make([]string, len(row))
	for columnIndex, cell := range row {
		cell = strings.ReplaceAll(cell, "|", "\\|")
		if p.includeCellCoordinates {
			cell = "[" + cellCoordinate(rowIndex, columnIndex) + "] " + cell
		}
		formatted[columnIndex] = cell
	}
	return formatted
}

func cellCoordinate(rowIndex, columnIndex int) string {
	return excelColumnName(columnIndex) + fmt.Sprintf("%d", rowIndex+1)
}

func excelColumnName(columnIndex int) string {
	var name string
	for columnIndex++; columnIndex > 0; columnIndex /= 26 {
		columnIndex--
		name = string(rune('A'+columnIndex%26)) + name
	}
	return name
}

func newTextSection(text, filename, sheetName string) rag.Section {
	return rag.Section{
		Content: message.NewTextBlock(text),
		Source:  filename,
		Metadata: map[string]any{
			"sheet": sheetName,
		},
	}
}

type anchoredCell struct {
	name   string
	row    int
	column int
}

func xlsxImageSections(
	ctx context.Context,
	workbook *excelize.File,
	sheetName string,
	filename string,
) ([]rag.Section, error) {
	cells, err := workbook.GetPictureCells(sheetName)
	if err != nil {
		return nil, fmt.Errorf("listing pictures in sheet %q: %w", sheetName, err)
	}
	anchors := make([]anchoredCell, 0, len(cells))
	for _, cell := range cells {
		column, row, err := excelize.CellNameToCoordinates(cell)
		if err == nil {
			anchors = append(anchors, anchoredCell{name: cell, row: row, column: column})
		}
	}
	sort.SliceStable(anchors, func(left, right int) bool {
		if anchors[left].row == anchors[right].row {
			return anchors[left].column < anchors[right].column
		}
		return anchors[left].row < anchors[right].row
	})

	sections := make([]rag.Section, 0, len(anchors))
	for _, anchor := range anchors {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		pictures, err := workbook.GetPictures(sheetName, anchor.name)
		if err != nil {
			continue
		}
		for _, picture := range pictures {
			mediaType := guessImageMediaType(picture.File)
			sections = append(sections, rag.Section{
				Content: message.NewDataBlock(
					message.NewBase64Source(base64.StdEncoding.EncodeToString(picture.File), mediaType),
					message.WithDataBlockName(filename),
				),
				Source: filename,
				Metadata: map[string]any{
					"sheet":      sheetName,
					"media_type": mediaType,
				},
			})
		}
	}
	return sections, nil
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

func mergeTextSections(sections []rag.Section, filename string) []rag.Section {
	textParts := make([]string, 0, len(sections))
	nonText := make([]rag.Section, 0, len(sections))
	for _, section := range sections {
		if block, ok := section.Content.(*message.TextBlock); ok {
			textParts = append(textParts, block.Text)
		} else {
			nonText = append(nonText, section)
		}
	}
	result := make([]rag.Section, 0, 1+len(nonText))
	if len(textParts) > 0 {
		result = append(result, rag.Section{
			Content:  message.NewTextBlock(strings.Join(textParts, "\n")),
			Source:   filename,
			Metadata: map[string]any{},
		})
	}
	return append(result, nonText...)
}
