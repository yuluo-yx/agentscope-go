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

package pptx

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

func TestParserExtractsSlidesInPresentationOrder(t *testing.T) {
	parser := NewParser()
	data := testPPTX(t)

	sections, err := parser.Parse(context.Background(), data, "deck.pptx")
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}

	if len(sections) != 4 {
		t.Fatalf("expected four sections, got %#v", sections)
	}
	firstText := sections[0].Content.(*message.TextBlock).Text
	if !strings.Contains(firstText, "<slide index=1>") || !strings.Contains(firstText, "Slide two before image") {
		t.Fatalf("first section text mismatch: %q", firstText)
	}
	if sections[0].Metadata["slide"] != 1 {
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
	if sections[1].Metadata["slide"] != 1 || sections[1].Metadata["media_type"] != "image/png" {
		t.Fatalf("image metadata mismatch: %#v", sections[1])
	}

	afterImage := sections[2].Content.(*message.TextBlock).Text
	if !strings.Contains(afterImage, "Slide two after image") || !strings.Contains(afterImage, "</slide>") {
		t.Fatalf("after-image text mismatch: %q", afterImage)
	}

	lastText := sections[3].Content.(*message.TextBlock).Text
	if !strings.Contains(lastText, "<slide index=2>") || !strings.Contains(lastText, "Slide one text") {
		t.Fatalf("last section text mismatch: %q", lastText)
	}
	if sections[3].Metadata["slide"] != 2 {
		t.Fatalf("last section metadata mismatch: %#v", sections[3])
	}
}

func TestParserCanDisableImagesAndSlideWrappers(t *testing.T) {
	parser := NewParser(WithImages(false), WithoutSlidePrefix(), WithoutSlideSuffix())

	sections, err := parser.Parse(context.Background(), testPPTX(t), "deck.pptx")
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}

	if len(sections) != 2 {
		t.Fatalf("expected two text sections, got %#v", sections)
	}
	for _, section := range sections {
		if _, ok := section.Content.(*message.TextBlock); !ok {
			t.Fatalf("expected only text sections, got %T", section.Content)
		}
	}
	if strings.Contains(sections[0].Content.(*message.TextBlock).Text, "<slide") {
		t.Fatalf("slide wrapper should be disabled: %q", sections[0].Content.(*message.TextBlock).Text)
	}
}

func TestParserExtractsTablesAsMarkdownByDefault(t *testing.T) {
	parser := NewParser(WithoutSlidePrefix(), WithoutSlideSuffix())

	sections, err := parser.Parse(context.Background(), testPPTXWithTable(t), "deck.pptx")
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}

	if len(sections) != 1 {
		t.Fatalf("expected one merged text section, got %#v", sections)
	}
	text := sections[0].Content.(*message.TextBlock).Text
	want := "Intro\n| Metric | Value |\n| --- | --- |\n| Coverage | 90% |\nOutro"
	if text != want {
		t.Fatalf("merged table text mismatch:\ngot:  %q\nwant: %q", text, want)
	}
	if sections[0].Metadata["slide"] != 1 {
		t.Fatalf("section metadata mismatch: %#v", sections[0])
	}
}

func TestParserSeparatesTablesAsJSON(t *testing.T) {
	parser := NewParser(
		WithoutSlidePrefix(),
		WithoutSlideSuffix(),
		WithSeparateTables(true),
		WithTableFormat(TableFormatJSON),
	)

	sections, err := parser.Parse(context.Background(), testPPTXWithTable(t), "deck.pptx")
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}

	if len(sections) != 3 {
		t.Fatalf("expected intro, table, and outro sections, got %#v", sections)
	}
	table := sections[1]
	text := table.Content.(*message.TextBlock).Text
	want := `<system-info>A table loaded as a JSON array:</system-info>
[["Metric","Value"],["Coverage","90%"]]`
	if text != want {
		t.Fatalf("JSON table text mismatch:\ngot:  %q\nwant: %q", text, want)
	}
	if table.Metadata["slide"] != 1 || table.Metadata["type"] != "table" || table.Metadata["index"] != 1 {
		t.Fatalf("table metadata mismatch: %#v", table)
	}
}

func TestParserRejectsUnsupportedTableFormat(t *testing.T) {
	parser := NewParser(WithTableFormat(TableFormat("yaml")))

	_, err := parser.Parse(context.Background(), testPPTXWithTable(t), "deck.pptx")
	if !errors.Is(err, rag.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput, got %v", err)
	}
}

func TestParserRejectsInvalidInputs(t *testing.T) {
	parser := NewParser()
	canceled, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := parser.Parse(canceled, []byte("pptx"), "deck.pptx"); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled Parse should fail with context.Canceled, got %v", err)
	}
	if _, err := parser.Parse(context.Background(), []byte("pptx"), " "); !errors.Is(err, rag.ErrInvalidInput) {
		t.Fatalf("blank filename should fail, got %v", err)
	}
	if _, err := parser.Parse(context.Background(), nil, "deck.pptx"); !errors.Is(err, rag.ErrInvalidInput) {
		t.Fatalf("empty data should fail, got %v", err)
	}
	if _, err := parser.Parse(context.Background(), []byte("not zip"), "deck.pptx"); err == nil {
		t.Fatal("invalid zip should fail")
	}
	if _, err := parser.Parse(context.Background(), testPPTXWithSlide(t, "<p:sld>"), "deck.pptx"); err == nil {
		t.Fatal("invalid slide XML should fail")
	}
	if _, err := parser.Parse(context.Background(), testPPTXWithSlideRelationships(t, `<Relationships><Relationship`), "deck.pptx"); err == nil {
		t.Fatal("invalid slide relationships XML should fail")
	}
}

func TestParserNilReceiverUsesDefaults(t *testing.T) {
	var parser *Parser

	sections, err := parser.Parse(context.Background(), testPPTX(t), "deck.pptx")
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if len(sections) != 4 {
		t.Fatalf("nil receiver should use default parser, got %#v", sections)
	}
}

func TestParserUsesCustomSlideWrappersAndFallbackSlideOrder(t *testing.T) {
	parser := NewParser(WithSlidePrefix("slide-{index}-start"), WithSlideSuffix("slide-{index}-end"))

	sections, err := parser.Parse(context.Background(), testPPTXWithoutPresentation(t), "deck.pptx")
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}

	if len(sections) != 2 {
		t.Fatalf("expected two sections, got %#v", sections)
	}
	first := sections[0].Content.(*message.TextBlock).Text
	second := sections[1].Content.(*message.TextBlock).Text
	if !strings.Contains(first, "slide-1-start") || !strings.Contains(first, "Slide two") ||
		!strings.Contains(first, "slide-1-end") {
		t.Fatalf("first fallback slide mismatch: %q", first)
	}
	if !strings.Contains(second, "slide-2-start") || !strings.Contains(second, "Slide ten") ||
		!strings.Contains(second, "slide-2-end") {
		t.Fatalf("second fallback slide mismatch: %q", second)
	}
}

func TestParserAddsSuffixAroundSeparateTable(t *testing.T) {
	parser := NewParser(WithSeparateTables(true))

	sections, err := parser.Parse(context.Background(), testPPTXWithTable(t), "deck.pptx")
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if len(sections) != 3 {
		t.Fatalf("expected prefix text, table, and suffix text, got %#v", sections)
	}
	if sections[1].Metadata["type"] != "table" {
		t.Fatalf("middle section should be table, got %#v", sections[1])
	}
	if text := sections[2].Content.(*message.TextBlock).Text; !strings.Contains(text, "Outro") ||
		!strings.Contains(text, "</slide>") {
		t.Fatalf("suffix text mismatch: %q", text)
	}
}

func TestParserSkipsMissingImageRelationships(t *testing.T) {
	parser := NewParser(WithoutSlidePrefix(), WithoutSlideSuffix())

	sections, err := parser.Parse(context.Background(), testPPTXWithMissingImage(t), "deck.pptx")
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if len(sections) != 1 {
		t.Fatalf("missing images should not create data sections, got %#v", sections)
	}
	text := sections[0].Content.(*message.TextBlock).Text
	if text != "Before\nAfter" {
		t.Fatalf("text around skipped images mismatch: %q", text)
	}
}

func TestParseSlideHandlesLooseTextAndImageOnlySuffix(t *testing.T) {
	files := pptxZipFiles(t, map[string]string{
		"ppt/media/image1.gif": "GIF89aimage",
	})
	parser := NewParser(WithoutSlidePrefix())
	relationships := map[string]string{"rImage": "ppt/media/image1.gif"}

	sections, err := parser.parseSlide(
		files,
		[]byte(`<p:sld xmlns:p="presentation" xmlns:a="drawing" xmlns:r="relationships"><a:t>Loose text</a:t><a:blip r:embed="rImage"/></p:sld>`),
		relationships,
		"deck.pptx",
		1,
	)
	if err != nil {
		t.Fatalf("parseSlide returned error: %v", err)
	}
	if len(sections) != 2 {
		t.Fatalf("expected loose text and image sections, got %#v", sections)
	}
	if text := sections[0].Content.(*message.TextBlock).Text; text != "Loose text\n</slide>" {
		t.Fatalf("loose text mismatch: %q", text)
	}

	sections, err = parser.parseSlide(
		files,
		[]byte(`<p:sld xmlns:p="presentation" xmlns:a="drawing" xmlns:r="relationships"><a:blip r:embed="rImage"/></p:sld>`),
		relationships,
		"deck.pptx",
		2,
	)
	if err != nil {
		t.Fatalf("image-only parseSlide returned error: %v", err)
	}
	if len(sections) != 1 {
		t.Fatalf("expected one image section, got %#v", sections)
	}
}

func TestHelperBranches(t *testing.T) {
	if text, err := formatTable(nil, TableFormatMarkdown); err != nil || text != "" {
		t.Fatalf("empty table should format empty, got %q err=%v", text, err)
	}
	if text, err := formatTable([][]string{{"A|B"}}, TableFormatMarkdown); err != nil || text != "| A\\|B |\n| --- |" {
		t.Fatalf("markdown table escaping mismatch: %q err=%v", text, err)
	}
	if _, err := formatTable([][]string{{"A"}}, TableFormat("yaml")); !errors.Is(err, rag.ErrInvalidInput) {
		t.Fatalf("unsupported table format should fail, got %v", err)
	}
	if rows := normalizeTableRows([][]string{{"", ""}, {"A"}}); !reflect.DeepEqual(rows, [][]string{{"A"}}) {
		t.Fatalf("normalizeTableRows mismatch: %#v", rows)
	}
	for _, tt := range []struct {
		name string
		data []byte
		want string
	}{
		{name: "jpeg", data: []byte("\xff\xd8jpeg"), want: "image/jpeg"},
		{name: "gif", data: []byte("GIF89aimage"), want: "image/gif"},
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
	if got := relationshipID([]xml.Attr{{Name: xml.Name{Local: "link"}, Value: "rLink"}}); got != "rLink" {
		t.Fatalf("relationship link attr mismatch: %q", got)
	}
	if got := relationshipID(nil); got != "" {
		t.Fatalf("empty relationship id = %q", got)
	}
	if got := attrValue(nil, "missing"); got != "" {
		t.Fatalf("missing attr value = %q", got)
	}

	files := pptxZipFiles(t, map[string]string{
		"ppt/_rels/presentation.xml.rels": `<?xml version="1.0" encoding="UTF-8"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rGood" Target="slides/slide1.xml"/>
  <Relationship Id="rExternal" Target="https://example.test/slide.xml"/>
  <Relationship Target="slides/missing-id.xml"/>
</Relationships>`,
	})
	rels, err := parseRelationships(files["ppt/_rels/presentation.xml.rels"], "ppt")
	if err != nil {
		t.Fatalf("parseRelationships returned error: %v", err)
	}
	if !reflect.DeepEqual(rels, map[string]string{"rGood": "ppt/slides/slide1.xml"}) {
		t.Fatalf("relationships mismatch: %#v", rels)
	}
}

func TestParserSupportedTypesAndExtensions(t *testing.T) {
	parser := NewParser()

	if !reflect.DeepEqual(parser.SupportedMediaTypes(), []string{
		"application/vnd.openxmlformats-officedocument.presentationml.presentation",
	}) {
		t.Fatalf("supported media types mismatch: %#v", parser.SupportedMediaTypes())
	}
	if !reflect.DeepEqual(parser.SupportedExtensions(), []string{".pptx"}) {
		t.Fatalf("supported extensions mismatch: %#v", parser.SupportedExtensions())
	}
}

var testPNG = []byte("\x89PNG\r\n\x1a\nimage")

func testPPTXWithoutPresentation(t *testing.T) []byte {
	t.Helper()

	var buf bytes.Buffer
	writer := zip.NewWriter(&buf)
	writeZipFile(t, writer, "ppt/slides/slide10.xml", slideXML("Slide ten", "", ""))
	writeZipFile(t, writer, "ppt/slides/slide2.xml", slideXML("Slide two", "", ""))
	if err := writer.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
	return buf.Bytes()
}

func testPPTXWithTable(t *testing.T) []byte {
	t.Helper()

	var buf bytes.Buffer
	writer := zip.NewWriter(&buf)
	writeZipFile(t, writer, "ppt/presentation.xml", `<?xml version="1.0" encoding="UTF-8"?>
<p:presentation xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">
  <p:sldIdLst>
    <p:sldId id="256" r:id="rId1"/>
  </p:sldIdLst>
</p:presentation>`)
	writeZipFile(t, writer, "ppt/_rels/presentation.xml.rels", `<?xml version="1.0" encoding="UTF-8"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1" Type="slide" Target="slides/slide1.xml"/>
</Relationships>`)
	writeZipFile(t, writer, "ppt/slides/slide1.xml", tableSlideXML())
	if err := writer.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
	return buf.Bytes()
}

func testPPTXWithSlide(t *testing.T, slide string) []byte {
	t.Helper()

	var buf bytes.Buffer
	writer := zip.NewWriter(&buf)
	writeZipFile(t, writer, "ppt/slides/slide1.xml", slide)
	if err := writer.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
	return buf.Bytes()
}

func testPPTXWithSlideRelationships(t *testing.T, rels string) []byte {
	t.Helper()

	var buf bytes.Buffer
	writer := zip.NewWriter(&buf)
	writeZipFile(t, writer, "ppt/slides/slide1.xml", slideXML("Slide one", "", ""))
	writeZipFile(t, writer, "ppt/slides/_rels/slide1.xml.rels", rels)
	if err := writer.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
	return buf.Bytes()
}

func testPPTXWithMissingImage(t *testing.T) []byte {
	t.Helper()

	var buf bytes.Buffer
	writer := zip.NewWriter(&buf)
	writeZipFile(t, writer, "ppt/slides/slide1.xml", slideXML("Before", "rMissing", "After"))
	writeZipFile(t, writer, "ppt/slides/_rels/slide1.xml.rels", `<?xml version="1.0" encoding="UTF-8"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rMissing" Type="image" Target="../media/missing.png"/>
</Relationships>`)
	if err := writer.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
	return buf.Bytes()
}

func testPPTX(t *testing.T) []byte {
	t.Helper()

	var buf bytes.Buffer
	writer := zip.NewWriter(&buf)
	writeZipFile(t, writer, "ppt/presentation.xml", `<?xml version="1.0" encoding="UTF-8"?>
<p:presentation xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">
  <p:sldIdLst>
    <p:sldId id="256" r:id="rId2"/>
    <p:sldId id="257" r:id="rId1"/>
  </p:sldIdLst>
</p:presentation>`)
	writeZipFile(t, writer, "ppt/_rels/presentation.xml.rels", `<?xml version="1.0" encoding="UTF-8"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1" Type="slide" Target="slides/slide1.xml"/>
  <Relationship Id="rId2" Type="slide" Target="slides/slide2.xml"/>
</Relationships>`)
	writeZipFile(t, writer, "ppt/slides/slide1.xml", slideXML("Slide one text", "", ""))
	writeZipFile(t, writer, "ppt/slides/slide2.xml", slideXML("Slide two before image", "rImg1", "Slide two after image"))
	writeZipFile(t, writer, "ppt/slides/_rels/slide2.xml.rels", `<?xml version="1.0" encoding="UTF-8"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rImg1" Type="image" Target="../media/image1.png"/>
</Relationships>`)
	writeZipBytes(t, writer, "ppt/media/image1.png", testPNG)
	if err := writer.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
	return buf.Bytes()
}

func pptxZipFiles(t *testing.T, files map[string]string) map[string]*zip.File {
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

func tableSlideXML() string {
	return `<?xml version="1.0" encoding="UTF-8"?>
<p:sld xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main" xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">
  <p:cSld><p:spTree>
    <p:sp><p:txBody><a:p><a:r><a:t>Intro</a:t></a:r></a:p></p:txBody></p:sp>
    <a:tbl>
      <a:tr>
        <a:tc><a:txBody><a:p><a:r><a:t>Metric</a:t></a:r></a:p></a:txBody></a:tc>
        <a:tc><a:txBody><a:p><a:r><a:t>Value</a:t></a:r></a:p></a:txBody></a:tc>
      </a:tr>
      <a:tr>
        <a:tc><a:txBody><a:p><a:r><a:t>Coverage</a:t></a:r></a:p></a:txBody></a:tc>
        <a:tc><a:txBody><a:p><a:r><a:t>90%</a:t></a:r></a:p></a:txBody></a:tc>
      </a:tr>
    </a:tbl>
    <p:sp><p:txBody><a:p><a:r><a:t>Outro</a:t></a:r></a:p></p:txBody></p:sp>
  </p:spTree></p:cSld>
</p:sld>`
}

func slideXML(before string, imageRel string, after string) string {
	var image string
	if imageRel != "" {
		image = `<p:pic><p:blipFill><a:blip r:embed="` + imageRel + `"/></p:blipFill></p:pic>`
	}
	var afterText string
	if after != "" {
		afterText = `<p:sp><p:txBody><a:p><a:r><a:t>` + after + `</a:t></a:r></a:p></p:txBody></p:sp>`
	}
	return `<?xml version="1.0" encoding="UTF-8"?>
<p:sld xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main" xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">
  <p:cSld><p:spTree>
    <p:sp><p:txBody><a:p><a:r><a:t>` + before + `</a:t></a:r></a:p></p:txBody></p:sp>
    ` + image + `
    ` + afterText + `
  </p:spTree></p:cSld>
</p:sld>`
}

func writeZipFile(t *testing.T, writer *zip.Writer, name string, content string) {
	t.Helper()
	writeZipBytes(t, writer, name, []byte(content))
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
