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

package excel

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"
	"github.com/yuluo-yx/agentscope-go/pkg/message"
	"github.com/yuluo-yx/agentscope-go/pkg/rag"
)

func TestParserParsesAndMergesXLSXSheets(t *testing.T) {
	workbook := excelize.NewFile()
	mustSetSheetName(t, workbook, "Sheet1", "First")
	mustSetCell(t, workbook, "First", "A1", " Name ")
	mustSetCell(t, workbook, "First", "B1", "City|Region")
	mustSetCell(t, workbook, "First", "A2", " Alice\r\nA ")
	mustSetCell(t, workbook, "First", "B2", " Paris ")
	mustNewSheet(t, workbook, "Second")
	mustSetCell(t, workbook, "Second", "A1", "Key")
	mustSetCell(t, workbook, "Second", "A2", "值")
	mustNewSheet(t, workbook, "HeaderOnly")
	mustSetCell(t, workbook, "HeaderOnly", "A1", "Header")

	sections, err := NewParser().Parse(context.Background(), workbookBytes(t, workbook), "book.xlsx")
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if len(sections) != 1 {
		t.Fatalf("expected one merged section, got %#v", sections)
	}
	text := textContent(t, sections[0])
	expected := "Sheet: First\n" +
		"| Name | City\\|Region |\n" +
		"| --- | --- |\n" +
		"| Alice\nA | Paris |\n\n" +
		"Sheet: Second\n" +
		"| Key |\n" +
		"| --- |\n" +
		"| 值 |\n"
	if text != expected {
		t.Fatalf("merged text mismatch:\n got: %q\nwant: %q", text, expected)
	}
	if sections[0].Source != "book.xlsx" || !reflect.DeepEqual(sections[0].Metadata, map[string]any{}) {
		t.Fatalf("merged section metadata mismatch: %#v", sections[0])
	}
}

func TestParserRendersSeparateJSONSheetsWithCoordinates(t *testing.T) {
	workbook := excelize.NewFile()
	mustSetCell(t, workbook, "Sheet1", "A1", "Name")
	mustSetCell(t, workbook, "Sheet1", "B1", "Age")
	mustSetCell(t, workbook, "Sheet1", "A2", "Alice")
	mustSetCell(t, workbook, "Sheet1", "B2", "25")

	parser := NewParser(
		WithSheetNames(false),
		WithCellCoordinates(true),
		WithSeparateSheets(true),
		WithTableFormat(TableFormatJSON),
	)
	sections, err := parser.Parse(context.Background(), workbookBytes(t, workbook), "book.xlsx")
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if len(sections) != 1 {
		t.Fatalf("expected one section, got %#v", sections)
	}
	expected := "<system-info>A table loaded as a JSON array:</system-info>\n" +
		`{"A1": "Name", "B1": "Age"}` + "\n" +
		`{"A2": "Alice", "B2": "25"}`
	if textContent(t, sections[0]) != expected {
		t.Fatalf("JSON text mismatch: %q", textContent(t, sections[0]))
	}
	if !reflect.DeepEqual(sections[0].Metadata, map[string]any{"sheet": "Sheet1"}) {
		t.Fatalf("sheet metadata mismatch: %#v", sections[0].Metadata)
	}
}

func TestParserExtractsXLSXImagesByAnchorOrder(t *testing.T) {
	workbook := excelize.NewFile()
	mustSetCell(t, workbook, "Sheet1", "A1", "Name")
	mustSetCell(t, workbook, "Sheet1", "A2", "Alice")
	laterImage := solidPNG(t, color.RGBA{B: 255, A: 255})
	earlierImage := solidPNG(t, color.RGBA{R: 255, A: 255})
	mustAddPicture(t, workbook, "Sheet1", "B5", laterImage)
	mustAddPicture(t, workbook, "Sheet1", "A2", earlierImage)

	sections, err := NewParser(WithImages(true)).Parse(
		context.Background(),
		workbookBytes(t, workbook),
		"images.xlsx",
	)
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if len(sections) != 3 {
		t.Fatalf("expected text and two image sections, got %#v", sections)
	}
	assertImageSection(t, sections[1], "images.xlsx", earlierImage)
	assertImageSection(t, sections[2], "images.xlsx", laterImage)
}

func TestParserParsesXLSAndSafelyOmitsImages(t *testing.T) {
	parser := NewParser(WithImages(true), WithSeparateSheets(true))
	sections, err := parser.Parse(context.Background(), testXLS(t), "TABLE.XLS")
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if len(sections) != 1 {
		t.Fatalf("expected one table section and no images, got %#v", sections)
	}
	expected := "Sheet: Table\n" +
		"| Code | Name | Description |\n" +
		"| --- | --- | --- |\n" +
		"| code1 | name1 | description1 |\n" +
		"| code2 | name2 | description2 |\n" +
		"| code3 | name3 | description3 |\n" +
		"| code4 | name4 | description4 |\n" +
		"| code5 | name5 | description5 |\n" +
		"| code6 | name6 | description6 |\n" +
		"| code7 | name7 | description7 |\n" +
		"| code8 | name8 | description8 |\n" +
		"| code9 | name9 | description9 |\n" +
		"| code10 | name10 | description10 |\n" +
		"| code11 | name11 | description11 |\n"
	if textContent(t, sections[0]) != expected {
		t.Fatalf("XLS text mismatch: %q", textContent(t, sections[0]))
	}
	if sections[0].Source != "TABLE.XLS" || sections[0].Metadata["sheet"] != "Table" {
		t.Fatalf("XLS section metadata mismatch: %#v", sections[0])
	}
}

func TestParserRejectsInvalidInput(t *testing.T) {
	validXLSX := simpleXLSX(t)
	tests := []struct {
		name     string
		parser   *Parser
		data     []byte
		filename string
	}{
		{name: "blank filename", parser: NewParser(), data: validXLSX, filename: " "},
		{name: "empty data", parser: NewParser(), filename: "book.xlsx"},
		{name: "unsupported extension", parser: NewParser(), data: validXLSX, filename: "book.csv"},
		{name: "mismatched extension", parser: NewParser(), data: validXLSX, filename: "book.xls"},
		{name: "invalid xlsx archive", parser: NewParser(), data: []byte("PK\x03\x04broken"), filename: "book.xlsx"},
		{
			name:     "invalid table format",
			parser:   NewParser(WithTableFormat(TableFormat("xml"))),
			data:     validXLSX,
			filename: "book.xlsx",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tt.parser.Parse(context.Background(), tt.data, tt.filename)
			if err == nil {
				t.Fatal("Parse should return an error")
			}
			if tt.name != "invalid xlsx archive" && !errors.Is(err, rag.ErrInvalidInput) {
				t.Fatalf("expected ErrInvalidInput, got %v", err)
			}
		})
	}
}

func TestParserRespectsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := NewParser().Parse(ctx, simpleXLSX(t), "book.xlsx")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestParserNilReceiverUsesDefaults(t *testing.T) {
	var parser *Parser
	sections, err := parser.Parse(context.Background(), simpleXLSX(t), "book.xlsx")
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if len(sections) != 1 || !strings.HasPrefix(textContent(t, sections[0]), "Sheet: Sheet1\n") {
		t.Fatalf("nil receiver did not use defaults: %#v", sections)
	}
}

func TestParserSupportedTypesAndExtensions(t *testing.T) {
	parser := NewParser()
	mediaTypes := parser.SupportedMediaTypes()
	extensions := parser.SupportedExtensions()
	if !reflect.DeepEqual(mediaTypes, []string{
		"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
		"application/vnd.ms-excel",
	}) {
		t.Fatalf("supported media types mismatch: %#v", mediaTypes)
	}
	if !reflect.DeepEqual(extensions, []string{".xls", ".xlsx"}) {
		t.Fatalf("supported extensions mismatch: %#v", extensions)
	}
	mediaTypes[0] = "changed"
	extensions[0] = ".changed"
	if parser.SupportedMediaTypes()[0] == "changed" || parser.SupportedExtensions()[0] == ".changed" {
		t.Fatal("supported values should be defensive copies")
	}
}

func TestParserHelpers(t *testing.T) {
	columns := map[int]string{0: "A", 25: "Z", 26: "AA", 51: "AZ", 52: "BA", 701: "ZZ", 702: "AAA"}
	for index, expected := range columns {
		if got := excelColumnName(index); got != expected {
			t.Fatalf("excelColumnName(%d) = %q, want %q", index, got, expected)
		}
	}

	table := normalizeTable([][]string{
		{" Header ", ""},
		{" value\r\nnext ", " second ", ""},
		{"", "", ""},
	})
	expectedTable := [][]string{{"Header", ""}, {"value\nnext", "second"}}
	if !reflect.DeepEqual(table, expectedTable) {
		t.Fatalf("normalizeTable mismatch: %#v", table)
	}
	if got := normalizeTable([][]string{{"Header"}}); got != nil {
		t.Fatalf("header-only table should be empty, got %#v", got)
	}
	if hasAnyPrefix([]byte("value"), []byte("no"), []byte("match")) {
		t.Fatal("hasAnyPrefix should reject unrelated prefixes")
	}

	markdown := NewParser(WithCellCoordinates(true), WithSheetNames(false)).renderMarkdown(
		[][]string{{"A|B"}, {"value"}},
		"ignored",
	)
	if markdown != "| [A1] A\\|B |\n| --- |\n| [A2] value |\n" {
		t.Fatalf("coordinate Markdown mismatch: %q", markdown)
	}
	jsonText := NewParser(WithTableFormat(TableFormatJSON)).renderJSON(
		[][]string{{"<名称>", "A&B"}, {"值", "\"quoted\""}},
		"Data",
	)
	expectedJSON := "Sheet: Data\n" +
		"<system-info>A table loaded as a JSON array:</system-info>\n" +
		`["<名称>", "A&B"]` + "\n" +
		`["值", "\"quoted\""]`
	if jsonText != expectedJSON {
		t.Fatalf("JSON array mismatch: %q", jsonText)
	}

	images := []struct {
		name string
		data []byte
		want string
	}{
		{name: "png", data: []byte("\x89PNG\r\n\x1a\n"), want: "image/png"},
		{name: "jpeg", data: []byte("\xff\xd8"), want: "image/jpeg"},
		{name: "gif", data: []byte("GIF89a"), want: "image/gif"},
		{name: "bmp", data: []byte("BM"), want: "image/bmp"},
		{name: "webp", data: []byte("RIFFxxxxWEBPdata"), want: "image/webp"},
		{name: "fallback", data: []byte("unknown"), want: "image/jpeg"},
	}
	for _, tt := range images {
		t.Run(tt.name, func(t *testing.T) {
			if got := guessImageMediaType(tt.data); got != tt.want {
				t.Fatalf("guessImageMediaType = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestXLSSafetyBoundaries(t *testing.T) {
	if _, err := openXLS([]byte("not an ole workbook")); err == nil {
		t.Fatal("openXLS should reject invalid data")
	}
	if sheet, err := getXLSSheet(nil, 0); err == nil || sheet != nil {
		t.Fatalf("nil workbook should be recovered, got sheet=%v err=%v", sheet, err)
	}
	if row, ok := readXLSRow(nil, 0); ok || row != nil {
		t.Fatalf("nil sheet should be recovered, got row=%#v ok=%v", row, ok)
	}

	workbook, err := openXLS(testXLS(t))
	if err != nil {
		t.Fatalf("openXLS returned error: %v", err)
	}
	if sheet, err := getXLSSheet(workbook, workbook.NumSheets()); err != nil || sheet != nil {
		t.Fatalf("out-of-range sheet mismatch: sheet=%v err=%v", sheet, err)
	}
	sheet, err := getXLSSheet(workbook, 0)
	if err != nil {
		t.Fatalf("getXLSSheet returned error: %v", err)
	}
	if row, ok := readXLSRow(sheet, int(sheet.MaxRow)+1); ok || row != nil {
		t.Fatalf("sparse XLS row should be omitted, got row=%#v ok=%v", row, ok)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := readXLSSheet(ctx, sheet); !errors.Is(err, context.Canceled) {
		t.Fatalf("readXLSSheet should return context.Canceled, got %v", err)
	}
}

func simpleXLSX(t *testing.T) []byte {
	t.Helper()
	workbook := excelize.NewFile()
	mustSetCell(t, workbook, "Sheet1", "A1", "Header")
	mustSetCell(t, workbook, "Sheet1", "A2", "Value")
	return workbookBytes(t, workbook)
}

func workbookBytes(t *testing.T, workbook *excelize.File) []byte {
	t.Helper()
	var buffer bytes.Buffer
	if _, err := workbook.WriteTo(&buffer); err != nil {
		t.Fatalf("WriteTo returned error: %v", err)
	}
	if err := workbook.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
	return buffer.Bytes()
}

func mustSetSheetName(t *testing.T, workbook *excelize.File, source, target string) {
	t.Helper()
	if err := workbook.SetSheetName(source, target); err != nil {
		t.Fatalf("SetSheetName returned error: %v", err)
	}
}

func mustNewSheet(t *testing.T, workbook *excelize.File, name string) {
	t.Helper()
	if _, err := workbook.NewSheet(name); err != nil {
		t.Fatalf("NewSheet returned error: %v", err)
	}
}

func mustSetCell(t *testing.T, workbook *excelize.File, sheet, cell, value string) {
	t.Helper()
	if err := workbook.SetCellStr(sheet, cell, value); err != nil {
		t.Fatalf("SetCellStr returned error: %v", err)
	}
}

func mustAddPicture(t *testing.T, workbook *excelize.File, sheet, cell string, data []byte) {
	t.Helper()
	if err := workbook.AddPictureFromBytes(sheet, cell, &excelize.Picture{
		Extension: ".png",
		File:      data,
	}); err != nil {
		t.Fatalf("AddPictureFromBytes returned error: %v", err)
	}
}

func solidPNG(t *testing.T, fill color.RGBA) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.SetRGBA(0, 0, fill)
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, img); err != nil {
		t.Fatalf("Encode returned error: %v", err)
	}
	return buffer.Bytes()
}

func textContent(t *testing.T, section rag.Section) string {
	t.Helper()
	block, ok := section.Content.(*message.TextBlock)
	if !ok {
		t.Fatalf("expected TextBlock, got %T", section.Content)
	}
	return block.Text
}

func assertImageSection(t *testing.T, section rag.Section, filename string, expected []byte) {
	t.Helper()
	block, ok := section.Content.(*message.DataBlock)
	if !ok {
		t.Fatalf("expected DataBlock, got %T", section.Content)
	}
	source, ok := block.Source.(*message.Base64Source)
	if !ok {
		t.Fatalf("expected Base64Source, got %T", block.Source)
	}
	if source.MediaType != "image/png" || source.Data != base64.StdEncoding.EncodeToString(expected) {
		t.Fatalf("image source mismatch: %#v", source)
	}
	if block.Name == nil || *block.Name != filename {
		t.Fatalf("image name mismatch: %#v", block.Name)
	}
	if section.Source != filename || section.Metadata["sheet"] != "Sheet1" ||
		section.Metadata["media_type"] != "image/png" {
		t.Fatalf("image metadata mismatch: %#v", section)
	}
}

func testXLS(t *testing.T) []byte {
	t.Helper()
	encoded := strings.Map(func(value rune) rune {
		if value == '\n' || value == '\r' || value == ' ' || value == '\t' {
			return -1
		}
		return value
	}, testXLSGzipBase64)
	compressed, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("DecodeString returned error: %v", err)
	}
	reader, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		t.Fatalf("NewReader returned error: %v", err)
	}
	data, readErr := io.ReadAll(reader)
	closeErr := reader.Close()
	if readErr != nil {
		t.Fatalf("ReadAll returned error: %v", readErr)
	}
	if closeErr != nil {
		t.Fatalf("Close returned error: %v", closeErr)
	}
	return data
}
