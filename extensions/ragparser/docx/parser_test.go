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

package docx

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/xml"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/yuluo-yx/agentscope-go/pkg/message"
	"github.com/yuluo-yx/agentscope-go/pkg/rag"
)

func TestParserExtractsDocumentBlocksInOrder(t *testing.T) {
	parser := NewParser()

	sections, err := parser.Parse(context.Background(), testDOCX(t), "document.docx")
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}

	if len(sections) != 2 {
		t.Fatalf("expected merged text and image sections, got %#v", sections)
	}
	firstText := sections[0].Content.(*message.TextBlock).Text
	wantText := "Intro text with taband linebreak\n" +
		"| Header A | Header B |\n" +
		"| --- | --- |\n" +
		"| Cell A1 | Cell B1 |\n\n" +
		"After table"
	if firstText != wantText {
		t.Fatalf("merged text mismatch:\ngot:  %q\nwant: %q", firstText, wantText)
	}
	if sections[0].Source != "document.docx" || !reflect.DeepEqual(sections[0].Metadata, map[string]any{}) {
		t.Fatalf("first section metadata mismatch: %#v", sections[0])
	}

	imageBlock, ok := sections[1].Content.(*message.DataBlock)
	if !ok {
		t.Fatalf("second section should be image DataBlock, got %T", sections[1].Content)
	}
	imageSource := imageBlock.Source.(*message.Base64Source)
	if imageSource.MediaType != "image/png" || imageSource.Data != base64.StdEncoding.EncodeToString(testPNG) {
		t.Fatalf("image source mismatch: %#v", imageSource)
	}
	if imageBlock.Name == nil || *imageBlock.Name != "document.docx" {
		t.Fatalf("image name mismatch: %#v", imageBlock.Name)
	}
	if !reflect.DeepEqual(sections[1].Metadata, map[string]any{"media_type": "image/png"}) {
		t.Fatalf("image metadata mismatch: %#v", sections[1])
	}
}

func TestParserCanDisableImagesAndTables(t *testing.T) {
	parser := NewParser(WithImages(false), WithTables(false))

	sections, err := parser.Parse(context.Background(), testDOCX(t), "document.docx")
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}

	if len(sections) != 1 {
		t.Fatalf("expected one merged paragraph section, got %#v", sections)
	}
	want := "Intro text with taband linebreak\nAfter table"
	if text := sections[0].Content.(*message.TextBlock).Text; text != want {
		t.Fatalf("text mismatch: got %q, want %q", text, want)
	}
	if !reflect.DeepEqual(sections[0].Metadata, map[string]any{}) {
		t.Fatalf("text metadata mismatch: %#v", sections[0].Metadata)
	}
}

func TestParserSeparatesMarkdownTables(t *testing.T) {
	sections, err := NewParser(WithSeparateTables(true)).Parse(
		context.Background(),
		testDOCX(t),
		"document.docx",
	)
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if len(sections) != 4 {
		t.Fatalf("expected paragraph, table, paragraph, and image sections, got %#v", sections)
	}
	if text := sections[0].Content.(*message.TextBlock).Text; text != "Intro text with taband linebreak" {
		t.Fatalf("first text mismatch: %q", text)
	}
	wantTable := "| Header A | Header B |\n| --- | --- |\n| Cell A1 | Cell B1 |\n"
	if text := sections[1].Content.(*message.TextBlock).Text; text != wantTable {
		t.Fatalf("table text mismatch: got %q, want %q", text, wantTable)
	}
	if text := sections[2].Content.(*message.TextBlock).Text; text != "After table" {
		t.Fatalf("last text mismatch: %q", text)
	}
	for _, section := range sections[:3] {
		if !reflect.DeepEqual(section.Metadata, map[string]any{}) {
			t.Fatalf("text metadata mismatch: %#v", section.Metadata)
		}
	}
}

func TestParserFormatsTablesAsJSON(t *testing.T) {
	parser := NewParser(
		WithImages(false),
		WithSeparateTables(true),
		WithTableFormat(TableFormatJSON),
	)
	sections, err := parser.Parse(context.Background(), testDOCX(t), "document.docx")
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if len(sections) != 3 {
		t.Fatalf("expected three text sections, got %#v", sections)
	}
	want := "<system-info>A table loaded as a JSON array:</system-info>\n" +
		`[["Header A", "Header B"], ["Cell A1", "Cell B1"]]`
	if text := sections[1].Content.(*message.TextBlock).Text; text != want {
		t.Fatalf("json table mismatch: got %q, want %q", text, want)
	}
}

func TestParserSupportedTypesAndExtensions(t *testing.T) {
	parser := NewParser()

	if !reflect.DeepEqual(parser.SupportedMediaTypes(), []string{
		"application/vnd.openxmlformats-officedocument.wordprocessingml.document",
	}) {
		t.Fatalf("supported media types mismatch: %#v", parser.SupportedMediaTypes())
	}
	if !reflect.DeepEqual(parser.SupportedExtensions(), []string{".docx"}) {
		t.Fatalf("supported extensions mismatch: %#v", parser.SupportedExtensions())
	}
}

func TestRelationshipTargetPath(t *testing.T) {
	tests := map[string]struct {
		baseDir string
		target  string
		want    string
	}{
		"relative": {
			baseDir: "word",
			target:  "media/image1.png",
			want:    "word/media/image1.png",
		},
		"absolute package path": {
			baseDir: "word",
			target:  "/word/media/image1.png",
			want:    "word/media/image1.png",
		},
		"relative parent": {
			baseDir: "word/charts",
			target:  "../media/image1.png",
			want:    "word/media/image1.png",
		},
		"windows separators": {
			baseDir: "word",
			target:  `media\image1.png`,
			want:    "word/media/image1.png",
		},
		"archive escape": {
			baseDir: "word",
			target:  "../../../etc/passwd",
			want:    "",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			if got := relationshipTargetPath(tt.baseDir, tt.target); got != tt.want {
				t.Fatalf("relationshipTargetPath() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParserRejectsInvalidInput(t *testing.T) {
	parser := NewParser()

	if _, err := parser.Parse(context.Background(), []byte("content"), " "); !errors.Is(err, rag.ErrInvalidInput) {
		t.Fatalf("blank filename should return ErrInvalidInput, got %v", err)
	}
	if _, err := parser.Parse(context.Background(), nil, "document.docx"); !errors.Is(err, rag.ErrInvalidInput) {
		t.Fatalf("empty data should return ErrInvalidInput, got %v", err)
	}
	if _, err := parser.Parse(context.Background(), []byte("not zip"), "document.docx"); err == nil {
		t.Fatal("invalid zip should return error")
	}
	if _, err := parser.Parse(context.Background(), testDOCXWithoutDocument(t), "document.docx"); !errors.Is(err, rag.ErrInvalidInput) {
		t.Fatalf("missing document should return ErrInvalidInput, got %v", err)
	}
	if _, err := parser.Parse(context.Background(), testDOCXWithDocument(t, "<w:document>"), "document.docx"); err == nil {
		t.Fatal("invalid document XML should return error")
	}
	if _, err := parser.Parse(
		context.Background(),
		testDOCXWithDocument(t, `<w:document xmlns:w="word"/>`),
		"document.docx",
	); !errors.Is(err, rag.ErrInvalidInput) {
		t.Fatalf("missing body should return ErrInvalidInput, got %v", err)
	}
	if _, err := parser.Parse(context.Background(), testDOCXWithRelationships(t, `<Relationships><Relationship`), "document.docx"); err == nil {
		t.Fatal("invalid relationships XML should return error")
	}
	if _, err := parser.Parse(context.Background(), testDOCXWithDocument(t, malformedParagraphXML), "document.docx"); err == nil {
		t.Fatal("invalid paragraph XML should return error")
	}
	if _, err := parser.Parse(context.Background(), testDOCXWithDocument(t, malformedTableXML), "document.docx"); err == nil {
		t.Fatal("invalid table XML should return error")
	}
	if _, err := parser.Parse(context.Background(), testDOCXWithDocument(t, invalidGridSpanXML), "document.docx"); !errors.Is(err, rag.ErrInvalidInput) {
		t.Fatalf("invalid gridSpan should return ErrInvalidInput, got %v", err)
	}
	if _, err := NewParser(WithTableFormat(TableFormat("csv"))).Parse(
		context.Background(),
		testDOCX(t),
		"document.docx",
	); !errors.Is(err, rag.ErrInvalidInput) {
		t.Fatalf("invalid table format should return ErrInvalidInput, got %v", err)
	}
	if _, err := parser.Parse(context.Background(), testDOCXWithUnsafePath(t), "document.docx"); !errors.Is(err, rag.ErrInvalidInput) {
		t.Fatalf("unsafe archive path should return ErrInvalidInput, got %v", err)
	}
	if _, err := parser.Parse(context.Background(), testDOCXWithDuplicateDocument(t), "document.docx"); !errors.Is(err, rag.ErrInvalidInput) {
		t.Fatalf("duplicate archive path should return ErrInvalidInput, got %v", err)
	}
}

func TestParserRespectsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := NewParser().Parse(ctx, testDOCX(t), "document.docx")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestParserNilReceiverUsesDefaults(t *testing.T) {
	var parser *Parser

	sections, err := parser.Parse(context.Background(), testDOCX(t), "document.docx")
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if len(sections) != 2 {
		t.Fatalf("nil receiver should use default parser, got %#v", sections)
	}
}

func TestHelperBranches(t *testing.T) {
	if got, err := formatTable(nil, TableFormatMarkdown); err != nil || got != "" {
		t.Fatalf("empty markdown table = %q, err=%v", got, err)
	}
	if got, err := formatTable([][]string{{"A|B"}, {"C"}}, TableFormatMarkdown); err != nil || got != "| A|B |\n| --- |\n| C |\n" {
		t.Fatalf("markdown table = %q, err=%v", got, err)
	}
	wantJSON := "<system-info>A table loaded as a JSON array:</system-info>\n" + `[["你好", "<x>"]]`
	if got, err := formatTable([][]string{{"你好", "<x>"}}, TableFormatJSON); err != nil || got != wantJSON {
		t.Fatalf("json table = %q, err=%v", got, err)
	}
	if _, err := formatTable(nil, TableFormat("csv")); !errors.Is(err, rag.ErrInvalidInput) {
		t.Fatalf("invalid format should return ErrInvalidInput, got %v", err)
	}
	gridDecoder := xml.NewDecoder(strings.NewReader(`<w:tcPr xmlns:w="word"><w:gridSpan/></w:tcPr>`))
	gridStart := nextStartElement(t, gridDecoder, "tcPr")
	if span, err := collectGridSpan(context.Background(), gridDecoder, gridStart); err != nil || span != 1 {
		t.Fatalf("gridSpan without val = %d, err=%v", span, err)
	}
	for _, tt := range []struct {
		name string
		data []byte
		want string
	}{
		{name: "jpeg", data: []byte("\xff\xd8jpeg"), want: "image/jpeg"},
		{name: "gif", data: []byte("GIF87aimage"), want: "image/gif"},
		{name: "bmp", data: []byte("BMimage"), want: "image/bmp"},
		{name: "webp", data: []byte("RIFFxxxxWEBPimage"), want: "image/webp"},
		{name: "fallback", data: []byte("unknown"), want: "image/jpeg"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := guessImageMediaType(tt.data); got != tt.want {
				t.Fatalf("guessImageMediaType = %q, want %q", got, tt.want)
			}
		})
	}
	if got := relationshipID([]xml.Attr{{Name: xml.Name{Local: "id", Space: "relationships"}, Value: "rId1"}}); got != "rId1" {
		t.Fatalf("relationship id attr mismatch: %q", got)
	}
	if got := relationshipID([]xml.Attr{{Name: xml.Name{Local: "link"}, Value: "rLink"}}); got != "rLink" {
		t.Fatalf("relationship link attr mismatch: %q", got)
	}
	if got := relationshipID(nil); got != "" {
		t.Fatalf("empty relationship id = %q", got)
	}

	files := docxZipFiles(t, map[string]string{
		"word/_rels/document.xml.rels": `<?xml version="1.0" encoding="UTF-8"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rGood" Target="media/image.png"/>
  <Relationship Id="rExternal" Target="https://example.test/image.png"/>
  <Relationship Id="rMode" TargetMode="External" Target="media/external.png"/>
  <Relationship Target="media/missing-id.png"/>
</Relationships>`,
	})
	rels, err := parseRelationships(context.Background(), files["word/_rels/document.xml.rels"], "word")
	if err != nil {
		t.Fatalf("parseRelationships returned error: %v", err)
	}
	if !reflect.DeepEqual(rels, map[string]string{"rGood": "word/media/image.png"}) {
		t.Fatalf("relationships mismatch: %#v", rels)
	}
}

func TestCollectorBranches(t *testing.T) {
	paragraphDecoder := xml.NewDecoder(strings.NewReader(`
<w:p xmlns:w="word" xmlns:r="relationships">
  <w:r><w:delText>deleted</w:delText></w:r>
	<w:r><w:t>live</w:t></w:r>
  <w:r><w:cr/></w:r>
  <w:r><v:imagedata xmlns:v="vml" r:id="rImg2"/></w:r>
</w:p>`))
	paragraphStart := nextStartElement(t, paragraphDecoder, "p")
	paragraph, err := collectParagraph(context.Background(), paragraphDecoder, paragraphStart)
	if err != nil {
		t.Fatalf("collectParagraph returned error: %v", err)
	}
	if paragraph.text != "live" || !reflect.DeepEqual(paragraph.imageIDs, []string{"rImg2"}) {
		t.Fatalf("paragraph content mismatch: %#v", paragraph)
	}

	tableDecoder := xml.NewDecoder(strings.NewReader(`
<w:tbl xmlns:w="word" xmlns:r="relationships">
  <w:tr>
	<w:tc><w:tcPr><w:gridSpan w:val="2"/></w:tcPr><w:p><w:r><w:t>first</w:t></w:r></w:p><w:p><w:r><w:t>second</w:t></w:r></w:p></w:tc>
	<w:tc><w:p><w:r><w:t>third</w:t></w:r></w:p></w:tc>
  </w:tr>
  <w:tr><w:tc><w:p><w:r><a:blip xmlns:a="drawing" r:embed="rImg3"/></w:r></w:p></w:tc></w:tr>
</w:tbl>`))
	tableStart := nextStartElement(t, tableDecoder, "tbl")
	table, err := collectTable(context.Background(), tableDecoder, tableStart)
	if err != nil {
		t.Fatalf("collectTable returned error: %v", err)
	}
	if !reflect.DeepEqual(table.rows, [][]string{{"first\nsecond", "", "third"}, {""}}) {
		t.Fatalf("table content mismatch: %#v", table)
	}
}

func TestAppendImagesSkipsMissingRelationshipsAndFiles(t *testing.T) {
	files := docxZipFiles(t, map[string]string{
		"word/media/image1.gif": "GIF89aimage",
	})
	state := &documentParseState{
		ctx:    context.Background(),
		parser: NewParser(),
		files:  files,
		relationships: map[string]string{
			"rMissingFile": "word/media/missing.png",
			"rImage":       "word/media/image1.gif",
		},
		filename: "document.docx",
	}

	if err := state.appendImages([]string{"rUnknown", "rMissingFile", "rImage"}); err != nil {
		t.Fatalf("appendImages returned error: %v", err)
	}
	if len(state.sections) != 1 {
		t.Fatalf("expected one image section, got %#v", state.sections)
	}
	if state.sections[0].Metadata["media_type"] != "image/gif" {
		t.Fatalf("image metadata mismatch: %#v", state.sections[0])
	}
	block := state.sections[0].Content.(*message.DataBlock)
	if block.Name == nil || *block.Name != "document.docx" {
		t.Fatalf("image name mismatch: %#v", block.Name)
	}
}

func TestCollectorsRespectCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	tests := []struct {
		name    string
		xmlData string
		local   string
		collect func(*xml.Decoder, xml.StartElement) error
	}{
		{
			name:    "paragraph",
			xmlData: `<w:p xmlns:w="word"><w:r><w:t>text</w:t></w:r></w:p>`,
			local:   "p",
			collect: func(decoder *xml.Decoder, start xml.StartElement) error {
				_, err := collectParagraph(ctx, decoder, start)
				return err
			},
		},
		{
			name:    "table",
			xmlData: `<w:tbl xmlns:w="word"><w:tr/></w:tbl>`,
			local:   "tbl",
			collect: func(decoder *xml.Decoder, start xml.StartElement) error {
				_, err := collectTable(ctx, decoder, start)
				return err
			},
		},
		{
			name:    "row",
			xmlData: `<w:tr xmlns:w="word"><w:tc/></w:tr>`,
			local:   "tr",
			collect: func(decoder *xml.Decoder, start xml.StartElement) error {
				_, err := collectTableRow(ctx, decoder, start)
				return err
			},
		},
		{
			name:    "cell",
			xmlData: `<w:tc xmlns:w="word"><w:p/></w:tc>`,
			local:   "tc",
			collect: func(decoder *xml.Decoder, start xml.StartElement) error {
				_, _, err := collectTableCell(ctx, decoder, start)
				return err
			},
		},
		{
			name:    "grid span",
			xmlData: `<w:tcPr xmlns:w="word"><w:gridSpan w:val="2"/></w:tcPr>`,
			local:   "tcPr",
			collect: func(decoder *xml.Decoder, start xml.StartElement) error {
				_, err := collectGridSpan(ctx, decoder, start)
				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decoder := xml.NewDecoder(strings.NewReader(tt.xmlData))
			start := nextStartElement(t, decoder, tt.local)
			if err := tt.collect(decoder, start); !errors.Is(err, context.Canceled) {
				t.Fatalf("collector should return context.Canceled, got %v", err)
			}
		})
	}

	files := docxZipFiles(t, map[string]string{
		"word/_rels/document.xml.rels": `<Relationships/>`,
	})
	if _, err := parseRelationships(ctx, files["word/_rels/document.xml.rels"], "word"); !errors.Is(err, context.Canceled) {
		t.Fatalf("parseRelationships should return context.Canceled, got %v", err)
	}
}

func TestTableColumnLimit(t *testing.T) {
	decoder := xml.NewDecoder(strings.NewReader(`
<w:tbl xmlns:w="word">
  <w:tr>
	<w:tc><w:tcPr><w:gridSpan w:val="16384"/></w:tcPr><w:p><w:r><w:t>wide</w:t></w:r></w:p></w:tc>
	<w:tc><w:p><w:r><w:t>overflow</w:t></w:r></w:p></w:tc>
  </w:tr>
</w:tbl>`))
	start := nextStartElement(t, decoder, "tbl")
	if _, err := collectTable(context.Background(), decoder, start); !errors.Is(err, rag.ErrInvalidInput) {
		t.Fatalf("column overflow should return ErrInvalidInput, got %v", err)
	}
}

func TestArchiveLimitsAndStateBranches(t *testing.T) {
	if _, err := readZipFile(nil); !errors.Is(err, rag.ErrInvalidInput) {
		t.Fatalf("nil archive part should return ErrInvalidInput, got %v", err)
	}
	largePart := &zip.File{FileHeader: zip.FileHeader{
		Name:               "word/large.bin",
		UncompressedSize64: maxPartSize + 1,
	}}
	if _, err := readZipFile(largePart); !errors.Is(err, rag.ErrInvalidInput) {
		t.Fatalf("large archive part should return ErrInvalidInput, got %v", err)
	}
	if _, err := indexZipFiles(make([]*zip.File, maxArchiveEntries+1)); !errors.Is(err, rag.ErrInvalidInput) {
		t.Fatalf("too many entries should return ErrInvalidInput, got %v", err)
	}
	largeArchive := &zip.File{FileHeader: zip.FileHeader{
		Name:               "word/large.bin",
		UncompressedSize64: maxArchiveUncompressedSize + 1,
	}}
	if _, err := indexZipFiles([]*zip.File{largeArchive}); !errors.Is(err, rag.ErrInvalidInput) {
		t.Fatalf("large archive should return ErrInvalidInput, got %v", err)
	}
	for _, name := range []string{"", "/absolute", ".", "../escape"} {
		if _, ok := cleanArchivePath(name); ok {
			t.Fatalf("cleanArchivePath(%q) should reject the path", name)
		}
	}

	state := &documentParseState{
		ctx:      context.Background(),
		parser:   NewParser(WithImages(false)),
		filename: "document.docx",
	}
	if err := state.appendImages([]string{"rImage"}); err != nil {
		t.Fatalf("disabled images should not fail: %v", err)
	}
	if err := state.appendTable(tableContent{}); err != nil || len(state.sections) != 0 {
		t.Fatalf("empty table should not emit a section: sections=%#v err=%v", state.sections, err)
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	state.ctx = canceled
	state.parser = NewParser()
	if err := state.appendImages([]string{"rImage"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("image extraction should return context.Canceled, got %v", err)
	}
}

var testPNG = []byte("\x89PNG\r\n\x1a\nimage")

func nextStartElement(t *testing.T, decoder *xml.Decoder, local string) xml.StartElement {
	t.Helper()
	for {
		token, err := decoder.Token()
		if err != nil {
			t.Fatalf("Token returned error: %v", err)
		}
		start, ok := token.(xml.StartElement)
		if ok && start.Name.Local == local {
			return start
		}
	}
}

func testDOCX(t *testing.T) []byte {
	t.Helper()

	var buf bytes.Buffer
	writer := zip.NewWriter(&buf)
	writeZipFile(t, writer, "[Content_Types].xml", contentTypesXML)
	writeZipFile(t, writer, "_rels/.rels", rootRelationshipsXML)
	writeZipFile(t, writer, "word/document.xml", documentXML)
	writeZipFile(t, writer, "word/_rels/document.xml.rels", documentRelationshipsXML)
	writeZipBytes(t, writer, "word/media/image1.png", testPNG)
	if err := writer.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
	return buf.Bytes()
}

func testDOCXWithoutDocument(t *testing.T) []byte {
	t.Helper()

	var buf bytes.Buffer
	writer := zip.NewWriter(&buf)
	writeZipFile(t, writer, "[Content_Types].xml", contentTypesXML)
	if err := writer.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
	return buf.Bytes()
}

func testDOCXWithDocument(t *testing.T, document string) []byte {
	t.Helper()

	var buf bytes.Buffer
	writer := zip.NewWriter(&buf)
	writeZipFile(t, writer, "word/document.xml", document)
	if err := writer.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
	return buf.Bytes()
}

func testDOCXWithRelationships(t *testing.T, rels string) []byte {
	t.Helper()

	var buf bytes.Buffer
	writer := zip.NewWriter(&buf)
	writeZipFile(t, writer, "word/document.xml", documentXML)
	writeZipFile(t, writer, "word/_rels/document.xml.rels", rels)
	if err := writer.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
	return buf.Bytes()
}

func testDOCXWithUnsafePath(t *testing.T) []byte {
	t.Helper()

	var buf bytes.Buffer
	writer := zip.NewWriter(&buf)
	writeZipFile(t, writer, "word/document.xml", documentXML)
	writeZipFile(t, writer, "../outside.xml", "<outside/>")
	if err := writer.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
	return buf.Bytes()
}

func testDOCXWithDuplicateDocument(t *testing.T) []byte {
	t.Helper()

	var buf bytes.Buffer
	writer := zip.NewWriter(&buf)
	writeZipFile(t, writer, "word/document.xml", documentXML)
	writeZipFile(t, writer, "word/document.xml", documentXML)
	if err := writer.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
	return buf.Bytes()
}

func docxZipFiles(t *testing.T, files map[string]string) map[string]*zip.File {
	t.Helper()

	var buf bytes.Buffer
	writer := zip.NewWriter(&buf)
	for name, content := range files {
		writeZipFile(t, writer, name, content)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
	reader, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatalf("NewReader returned error: %v", err)
	}
	indexed, err := indexZipFiles(reader.File)
	if err != nil {
		t.Fatalf("indexZipFiles returned error: %v", err)
	}
	return indexed
}

const contentTypesXML = `<?xml version="1.0" encoding="UTF-8"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
  <Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
  <Default Extension="xml" ContentType="application/xml"/>
  <Default Extension="png" ContentType="image/png"/>
  <Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>
</Types>`

const rootRelationshipsXML = `<?xml version="1.0" encoding="UTF-8"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1" Type="officeDocument" Target="word/document.xml"/>
</Relationships>`

const documentRelationshipsXML = `<?xml version="1.0" encoding="UTF-8"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rImg1" Type="image" Target="media/image1.png"/>
</Relationships>`

const documentXML = `<?xml version="1.0" encoding="UTF-8"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">
  <w:body>
    <w:p>
      <w:r><w:t>Intro text with tab</w:t></w:r>
      <w:r><w:tab/></w:r>
      <w:r><w:t>and line</w:t></w:r>
      <w:r><w:br/></w:r>
      <w:r><w:t>break</w:t></w:r>
    </w:p>
    <w:tbl>
      <w:tr>
        <w:tc><w:p><w:r><w:t>Header A</w:t></w:r></w:p></w:tc>
        <w:tc><w:p><w:r><w:t>Header B</w:t></w:r></w:p></w:tc>
      </w:tr>
      <w:tr>
        <w:tc><w:p><w:r><w:t>Cell A1</w:t></w:r></w:p></w:tc>
        <w:tc><w:p><w:r><w:t>Cell B1</w:t></w:r></w:p></w:tc>
      </w:tr>
    </w:tbl>
    <w:p>
      <w:r><w:t>After table</w:t></w:r>
      <w:r><w:drawing><wp:inline xmlns:wp="http://schemas.openxmlformats.org/drawingml/2006/wordprocessingDrawing"><a:graphic><a:graphicData><pic:pic xmlns:pic="http://schemas.openxmlformats.org/drawingml/2006/picture"><pic:blipFill><a:blip r:embed="rImg1"/></pic:blipFill></pic:pic></a:graphicData></a:graphic></wp:inline></w:drawing></w:r>
    </w:p>
  </w:body>
</w:document>`

const malformedParagraphXML = `<?xml version="1.0" encoding="UTF-8"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
  <w:body><w:p><w:r><w:t>broken</w:t></w:r></w:body>
</w:document>`

const malformedTableXML = `<?xml version="1.0" encoding="UTF-8"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
  <w:body><w:tbl><w:tr><w:tc><w:p><w:r><w:t>broken</w:t></w:r></w:p></w:tr></w:body>
</w:document>`

const invalidGridSpanXML = `<?xml version="1.0" encoding="UTF-8"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
  <w:body><w:tbl><w:tr><w:tc><w:tcPr><w:gridSpan w:val="999999"/></w:tcPr><w:p><w:r><w:t>broken</w:t></w:r></w:p></w:tc></w:tr></w:tbl></w:body>
</w:document>`

func writeZipFile(t *testing.T, writer *zip.Writer, name string, content string) {
	t.Helper()
	writeZipBytes(t, writer, name, []byte(strings.TrimSpace(content)))
}

func writeZipBytes(t *testing.T, writer *zip.Writer, name string, content []byte) {
	t.Helper()
	file, err := writer.Create(name)
	if err != nil {
		t.Fatalf("Create(%q) returned error: %v", name, err)
	}
	if _, err := file.Write(content); err != nil {
		t.Fatalf("Write(%q) returned error: %v", name, err)
	}
}
