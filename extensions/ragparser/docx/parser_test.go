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

	if len(sections) != 4 {
		t.Fatalf("expected four sections, got %#v", sections)
	}
	firstText := sections[0].Content.(*message.TextBlock).Text
	if firstText != "Intro text with tab\tand line\nbreak" {
		t.Fatalf("first paragraph mismatch: %q", firstText)
	}
	if sections[0].Source != "document.docx" || sections[0].Metadata["type"] != "paragraph" || sections[0].Metadata["index"] != 1 {
		t.Fatalf("first section metadata mismatch: %#v", sections[0])
	}

	tableText := sections[1].Content.(*message.TextBlock).Text
	if tableText != "| Header A | Header B |\n| Cell A1 | Cell B1 |" {
		t.Fatalf("table text mismatch: %q", tableText)
	}
	if sections[1].Metadata["type"] != "table" || sections[1].Metadata["index"] != 1 {
		t.Fatalf("table metadata mismatch: %#v", sections[1])
	}

	secondText := sections[2].Content.(*message.TextBlock).Text
	if secondText != "After table" {
		t.Fatalf("second paragraph mismatch: %q", secondText)
	}

	imageBlock, ok := sections[3].Content.(*message.DataBlock)
	if !ok {
		t.Fatalf("fourth section should be image DataBlock, got %T", sections[3].Content)
	}
	imageSource := imageBlock.Source.(*message.Base64Source)
	if imageSource.MediaType != "image/png" || imageSource.Data != base64.StdEncoding.EncodeToString(testPNG) {
		t.Fatalf("image source mismatch: %#v", imageSource)
	}
	if imageBlock.Name == nil || *imageBlock.Name != "image1.png" {
		t.Fatalf("image name mismatch: %#v", imageBlock.Name)
	}
	if sections[3].Metadata["type"] != "image" ||
		sections[3].Metadata["index"] != 1 ||
		sections[3].Metadata["media_type"] != "image/png" {
		t.Fatalf("image metadata mismatch: %#v", sections[3])
	}
}

func TestParserCanDisableImagesAndTables(t *testing.T) {
	parser := NewParser(WithImages(false), WithTables(false))

	sections, err := parser.Parse(context.Background(), testDOCX(t), "document.docx")
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}

	if len(sections) != 2 {
		t.Fatalf("expected two paragraph sections, got %#v", sections)
	}
	for _, section := range sections {
		if section.Metadata["type"] != "paragraph" {
			t.Fatalf("expected only paragraph sections, got %#v", section)
		}
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
	if _, err := parser.Parse(context.Background(), testDOCXWithRelationships(t, `<Relationships><Relationship`), "document.docx"); err == nil {
		t.Fatal("invalid relationships XML should return error")
	}
	if _, err := parser.Parse(context.Background(), testDOCXWithDocument(t, malformedParagraphXML), "document.docx"); err == nil {
		t.Fatal("invalid paragraph XML should return error")
	}
	if _, err := parser.Parse(context.Background(), testDOCXWithDocument(t, malformedTableXML), "document.docx"); err == nil {
		t.Fatal("invalid table XML should return error")
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
	if len(sections) != 4 {
		t.Fatalf("nil receiver should use default parser, got %#v", sections)
	}
}

func TestHelperBranches(t *testing.T) {
	if got := formatTable([][]string{{}, {"", ""}, {"A|B", "C"}}); got != `| A\|B | C |` {
		t.Fatalf("formatTable = %q", got)
	}
	if got := normalizeTableCell(" a \n\t b "); got != "a b" {
		t.Fatalf("normalizeTableCell = %q", got)
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
	rels, err := parseRelationships(files["word/_rels/document.xml.rels"], "word")
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
  <w:r><w:cr/></w:r>
  <w:r><v:imagedata xmlns:v="vml" r:id="rImg2"/></w:r>
</w:p>`))
	paragraphStart := nextStartElement(t, paragraphDecoder, "p")
	paragraph, err := collectParagraph(paragraphDecoder, paragraphStart)
	if err != nil {
		t.Fatalf("collectParagraph returned error: %v", err)
	}
	if paragraph.text != "deleted\n" || !reflect.DeepEqual(paragraph.imageIDs, []string{"rImg2"}) {
		t.Fatalf("paragraph content mismatch: %#v", paragraph)
	}

	tableDecoder := xml.NewDecoder(strings.NewReader(`
<w:tbl xmlns:w="word" xmlns:r="relationships">
  <w:tr>
    <w:tc><w:p><w:r><w:t>first</w:t></w:r></w:p><w:p><w:r><w:delText>second</w:delText></w:r></w:p></w:tc>
    <w:tc><w:p><w:r><w:tab/></w:r><w:r><w:br/></w:r><w:r><w:cr/></w:r></w:p></w:tc>
  </w:tr>
  <w:tr><w:tc><w:p><w:r><a:blip xmlns:a="drawing" r:embed="rImg3"/></w:r></w:p></w:tc></w:tr>
</w:tbl>`))
	tableStart := nextStartElement(t, tableDecoder, "tbl")
	table, err := collectTable(tableDecoder, tableStart)
	if err != nil {
		t.Fatalf("collectTable returned error: %v", err)
	}
	if !reflect.DeepEqual(table.rows, [][]string{{"first second", ""}, {""}}) ||
		!reflect.DeepEqual(table.imageIDs, []string{"rImg3"}) {
		t.Fatalf("table content mismatch: %#v", table)
	}
}

func TestAppendImagesSkipsMissingRelationshipsAndFiles(t *testing.T) {
	files := docxZipFiles(t, map[string]string{
		"word/media/image1.gif": "GIF89aimage",
	})
	state := &documentParseState{
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
	return indexZipFiles(reader.File)
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
