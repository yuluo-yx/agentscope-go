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

package sandboxed

import (
	"context"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/yuluo-yx/agentscope-go/pkg/message"
	"github.com/yuluo-yx/agentscope-go/pkg/tool/skill"
)

func TestListSkillsExecutableSpec(t *testing.T) {
	t.Parallel()

	if skills, err := (*remoteSkillLoader)(nil).ListSkills(context.Background()); err != nil || len(skills) != 0 {
		t.Fatalf("nil loader ListSkills = %#v, %v", skills, err)
	}
	if skills, err := (&remoteSkillLoader{}).ListSkills(context.Background()); err != nil || len(skills) != 0 {
		t.Fatalf("empty loader ListSkills = %#v, %v", skills, err)
	}
	var nilWorkspace *Workspace
	if _, err := nilWorkspace.ListSkills(context.Background()); err == nil {
		t.Fatal("nil workspace ListSkills should fail")
	}
	w, _, _, _, _ := newWorkspaceFixture(t)
	requireErrorContains(t, firstError(w.ListSkills(context.Background())), "not initialized")
	w, backend, _, _, _ := readyWorkspace(t)
	if _, err := w.ListSkills(canceledContext()); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled ListSkills error = %v", err)
	}

	backend.files["/work/skills/b/SKILL.md"] = []byte("---\nname: Beta\ndescription: second\n---\n# Beta\n")
	backend.files["/work/skills/a/SKILL.md"] = []byte("---\nname: Alpha\ndescription: first\n---\n# Alpha\n")
	backend.files["/work/skills/bad/SKILL.md"] = []byte("invalid")
	backend.files["/work/skills/unreadable/SKILL.md"] = []byte("---\nname: Hidden\ndescription: hidden\n---\n")
	backend.readHook = func(_ context.Context, filename string) ([]byte, error, bool) {
		if strings.Contains(filename, "unreadable") {
			return nil, errors.New("read denied"), true
		}
		return nil, nil, false
	}
	loaded, err := w.ListSkills(context.Background())
	if err != nil {
		t.Fatalf("ListSkills returned error: %v", err)
	}
	if len(loaded) != 2 || loaded[0].Name != "Alpha" || loaded[1].Name != "Beta" || loaded[0].Dir != "/work/skills/a" || !strings.Contains(loaded[0].Markdown, "# Alpha") {
		t.Fatalf("unexpected skills: %#v", loaded)
	}

	backend.execHook = func(context.Context, []string, ExecOptions) (ExecResult, error, bool) {
		return ExecResult{}, errors.New("find failed"), true
	}
	requireErrorContains(t, firstError(w.ListSkills(context.Background())), "scan skills")
	backend.execHook = func(context.Context, []string, ExecOptions) (ExecResult, error, bool) {
		return ExecResult{ExitCode: 2, Stderr: []byte("find denied")}, nil, true
	}
	requireErrorContains(t, firstError(w.ListSkills(context.Background())), "exit code 2")
}

func TestListSkillsCancellation(t *testing.T) {
	t.Parallel()

	w2, backend2, _, _, _ := readyWorkspace(t)
	backend2.files["/work/skills/a/SKILL.md"] = []byte("---\nname: a\ndescription: a\n---\n")
	backend2.files["/work/skills/b/SKILL.md"] = []byte("---\nname: b\ndescription: b\n---\n")
	ctx, cancel := context.WithCancel(context.Background())
	backend2.readHook = func(_ context.Context, filename string) ([]byte, error, bool) {
		if strings.Contains(filename, "/a/") {
			cancel()
		}
		return nil, nil, false
	}
	if _, err := w2.ListSkills(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("mid-scan cancellation error = %v", err)
	}
}

func TestAddSkillSuccessExistingAndRollback(t *testing.T) {
	t.Parallel()

	source := writeLocalSkill(t, "My Skill", "does work", map[string]string{
		"references/guide.md": "guide",
	})
	w, backend, _, _, _ := readyWorkspace(t)
	if err := w.AddSkill(context.Background(), source); err != nil {
		t.Fatalf("AddSkill returned error: %v", err)
	}
	destination := "/work/skills/my-skill"
	if len(backend.file(destination+"/SKILL.md")) == 0 || string(backend.file(destination+"/references/guide.md")) != "guide" {
		t.Fatal("AddSkill did not copy all files")
	}
	manifestPath := destination + "/" + skillManifestFile
	if manifest := strings.TrimSpace(string(backend.file(manifestPath))); len(manifest) != 64 || backend.writeCalls[len(backend.writeCalls)-1] != manifestPath {
		t.Fatalf("skill manifest = %q writes=%#v", manifest, backend.writeCalls)
	}
	writes := len(backend.writeCalls)
	if err := w.AddSkill(context.Background(), source); err != nil {
		t.Fatalf("identical AddSkill should be idempotent: %v", err)
	}
	if len(backend.writeCalls) != writes {
		t.Fatalf("identical skill was uploaded again: %#v", backend.writeCalls)
	}
	if err := os.WriteFile(filepath.Join(source, "references", "guide.md"), []byte("changed"), 0o600); err != nil {
		t.Fatalf("change local skill asset: %v", err)
	}
	requireErrorContains(t, w.AddSkill(context.Background(), source), "already exists")

	var nilWorkspace *Workspace
	requireErrorContains(t, nilWorkspace.AddSkill(context.Background(), source), "nil workspace")
	w2, _, _, _, _ := newWorkspaceFixture(t)
	requireErrorContains(t, w2.AddSkill(context.Background(), source), "not initialized")
	if err := w.AddSkill(canceledContext(), source); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled AddSkill error = %v", err)
	}
	requireErrorContains(t, w.AddSkill(context.Background(), t.TempDir()), "SKILL.md")

	t.Run("write failure removes partial directory", func(t *testing.T) {
		w, backend, _, _, _ := readyWorkspace(t)
		backend.writeHook = func(_ context.Context, filename string, _ []byte) (error, bool) {
			if strings.HasSuffix(filename, "guide.md") {
				return errors.New("write failed"), true
			}
			return nil, false
		}
		requireErrorContains(t, w.AddSkill(context.Background(), source), "write failed")
		if backend.existsLocked(destination) {
			t.Fatal("partial skill directory was not rolled back")
		}
	})

	t.Run("manifest write failure removes complete tree", func(t *testing.T) {
		w, backend, _, _, _ := readyWorkspace(t)
		backend.writeHook = func(_ context.Context, filename string, _ []byte) (error, bool) {
			if strings.HasSuffix(filename, "/"+skillManifestFile) {
				return errors.New("manifest failed"), true
			}
			return nil, false
		}
		requireErrorContains(t, w.AddSkill(context.Background(), source), "manifest failed")
		if backend.existsLocked(destination) {
			t.Fatal("manifest failure did not roll back the skill tree")
		}
		if len(backend.writeCalls) == 0 || backend.writeCalls[len(backend.writeCalls)-1] != destination+"/"+skillManifestFile {
			t.Fatalf("manifest was not written last: %#v", backend.writeCalls)
		}
	})

	t.Run("reserved manifest rejected", func(t *testing.T) {
		source := writeLocalSkill(t, "Reserved Skill", "reserved", map[string]string{
			skillManifestFile: "user data",
		})
		w, backend, _, _, _ := readyWorkspace(t)
		requireErrorContains(t, w.AddSkill(context.Background(), source), "reserved file")
		if backend.existsLocked("/work/skills/reserved-skill") {
			t.Fatal("reserved manifest failure created a remote skill")
		}
	})

	t.Run("symlink rejected and rolled back", func(t *testing.T) {
		source := writeLocalSkill(t, "Symlink Skill", "has link", nil)
		if err := os.Symlink("SKILL.md", filepath.Join(source, "link.md")); err != nil {
			t.Fatalf("create symlink: %v", err)
		}
		w, backend, _, _, _ := readyWorkspace(t)
		requireErrorContains(t, w.AddSkill(context.Background(), source), "contains symlink")
		if backend.existsLocked("/work/skills/symlink-skill") {
			t.Fatal("symlink failure was not rolled back")
		}
	})

	t.Run("hashed directory fallback", func(t *testing.T) {
		source := writeLocalSkill(t, "!!!", "punctuation", nil)
		w, backend, _, _, _ := readyWorkspace(t)
		if err := w.AddSkill(context.Background(), source); err != nil {
			t.Fatalf("AddSkill returned error: %v", err)
		}
		found := false
		for filename := range backend.files {
			if strings.HasPrefix(filename, "/work/skills/skill-") && strings.HasSuffix(filename, "/SKILL.md") {
				found = true
			}
		}
		if !found {
			t.Fatal("empty sanitized name did not use hashed directory")
		}
	})
}

func TestCollectLocalSkillFilesStableAndCancelable(t *testing.T) {
	t.Parallel()

	source := writeLocalSkill(t, "Collected Skill", "collected", map[string]string{
		"z.txt":             "last",
		"assets/a.txt":      "first",
		"scripts/run.sh":    "run",
		"references/ref.md": "reference",
	})
	first, firstManifest, err := collectLocalSkillFiles(context.Background(), source)
	if err != nil {
		t.Fatalf("first collectLocalSkillFiles returned error: %v", err)
	}
	second, secondManifest, err := collectLocalSkillFiles(context.Background(), source)
	if err != nil {
		t.Fatalf("second collectLocalSkillFiles returned error: %v", err)
	}
	if firstManifest != secondManifest || len(first) != len(second) {
		t.Fatalf("skill collection is unstable: %q/%q %#v/%#v", firstManifest, secondManifest, first, second)
	}
	for index := range first {
		if first[index].relative != second[index].relative || string(first[index].data) != string(second[index].data) {
			t.Fatalf("skill collection differs at %d: %#v/%#v", index, first[index], second[index])
		}
		if index > 0 && first[index-1].relative >= first[index].relative {
			t.Fatalf("skill files are not sorted: %#v", first)
		}
	}
	if _, _, err := collectLocalSkillFiles(canceledContext(), source); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled collectLocalSkillFiles error = %v", err)
	}
}

func TestRemoveSkillExecutableSpec(t *testing.T) {
	t.Parallel()

	var nilWorkspace *Workspace
	if err := nilWorkspace.RemoveSkill(context.Background(), "x"); err != nil {
		t.Fatalf("nil RemoveSkill returned error: %v", err)
	}
	w, _, _, _, _ := newWorkspaceFixture(t)
	requireErrorContains(t, w.RemoveSkill(context.Background(), "x"), "not initialized")
	w, backend, _, _, _ := readyWorkspace(t)
	if err := w.RemoveSkill(canceledContext(), "x"); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled RemoveSkill error = %v", err)
	}
	requireErrorContains(t, w.RemoveSkill(context.Background(), " "), "name is empty")
	backend.files["/work/skills/one/SKILL.md"] = []byte("---\nname: Same\ndescription: one\n---\n")
	backend.files["/work/skills/two/SKILL.md"] = []byte("---\nname: Same\ndescription: two\n---\n")
	if err := w.RemoveSkill(context.Background(), "Same"); err != nil {
		t.Fatalf("RemoveSkill returned error: %v", err)
	}
	if backend.existsLocked("/work/skills/one") || backend.existsLocked("/work/skills/two") {
		t.Fatal("RemoveSkill did not delete all matching skills")
	}
	if err := w.RemoveSkill(context.Background(), "missing"); err != nil {
		t.Fatalf("missing RemoveSkill returned error: %v", err)
	}

	backend.files["/work/skills/one/SKILL.md"] = []byte("---\nname: Same\ndescription: one\n---\n")
	backend.execHook = func(_ context.Context, argv []string, _ ExecOptions) (ExecResult, error, bool) {
		if len(argv) > 2 && argv[0] == "sh" && argv[2] == deleteTreeScript {
			return ExecResult{}, errors.New("delete failed"), true
		}
		return ExecResult{}, nil, false
	}
	requireErrorContains(t, w.RemoveSkill(context.Background(), "Same"), "clear workspace state")
}

func TestOffloadDataBlockExecutableSpec(t *testing.T) {
	t.Parallel()

	var nilWorkspace *Workspace
	if _, err := nilWorkspace.OffloadDataBlock(context.Background(), nil); err == nil {
		t.Fatal("nil workspace OffloadDataBlock should fail")
	}
	w, _, _, _, _ := newWorkspaceFixture(t)
	if _, err := w.OffloadDataBlock(context.Background(), nil); err == nil {
		t.Fatal("nil block should fail")
	}
	block := message.NewDataBlock(message.NewBase64Source(base64.StdEncoding.EncodeToString([]byte("image")), "image/png"), message.WithDataBlockID("data-id"), message.WithDataBlockName("photo"))
	if _, err := w.OffloadDataBlock(context.Background(), block); err == nil {
		t.Fatal("uninitialized OffloadDataBlock should fail")
	}
	w, backend, _, _, _ := readyWorkspace(t)
	if _, err := w.OffloadDataBlock(canceledContext(), block); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled OffloadDataBlock error = %v", err)
	}
	stored, err := w.OffloadDataBlock(context.Background(), block)
	if err != nil {
		t.Fatalf("OffloadDataBlock returned error: %v", err)
	}
	source, ok := stored.Source.(*message.URLSource)
	if !ok || !strings.HasPrefix(source.URL, "file:///work/data/") || !strings.HasSuffix(source.URL, ".png") || source.MediaType != "image/png" || stored.ID != "data-id" || stored.Name == nil || *stored.Name != "photo" {
		t.Fatalf("unexpected stored block: %#v", stored)
	}
	filename := strings.TrimPrefix(source.URL, "file://")
	if string(backend.file(filename)) != "image" {
		t.Fatalf("stored data = %q", backend.file(filename))
	}
	writes := len(backend.writeCalls)
	if _, err := w.OffloadDataBlock(context.Background(), block); err != nil || len(backend.writeCalls) != writes {
		t.Fatalf("duplicate OffloadDataBlock wrote again: writes=%#v err=%v", backend.writeCalls, err)
	}

	urlBlock := message.NewDataBlock(message.NewURLSource("https://example.test/a", "text/plain"))
	cloned, err := w.OffloadDataBlock(context.Background(), urlBlock)
	if err != nil || cloned == urlBlock || cloned.Source.(*message.URLSource).URL != "https://example.test/a" {
		t.Fatalf("URL block clone = %#v, %v", cloned, err)
	}
	invalid := message.NewDataBlock(message.NewBase64Source("***", "text/plain"))
	if _, err := w.OffloadDataBlock(context.Background(), invalid); err == nil {
		t.Fatal("invalid base64 should fail")
	}

	t.Run("write failure", func(t *testing.T) {
		w, backend, _, _, _ := readyWorkspace(t)
		backend.writeHook = func(context.Context, string, []byte) (error, bool) { return errors.New("write failed"), true }
		_, err := w.OffloadDataBlock(context.Background(), block)
		requireErrorContains(t, err, "write failed")
	})

	t.Run("existence failure", func(t *testing.T) {
		w, backend, _, _, _ := readyWorkspace(t)
		backend.execHook = func(_ context.Context, argv []string, _ ExecOptions) (ExecResult, error, bool) {
			if len(argv) > 0 && argv[0] == "test" {
				return ExecResult{}, errors.New("stat failed"), true
			}
			return ExecResult{}, nil, false
		}
		_, err := w.OffloadDataBlock(context.Background(), block)
		requireErrorContains(t, err, "test file")
	})
}

func TestOffloadContextExecutableSpec(t *testing.T) {
	t.Parallel()

	w, backend, _, _, _ := readyWorkspace(t)
	data := message.NewDataBlock(message.NewBase64Source(base64.StdEncoding.EncodeToString([]byte("payload")), "application/pdf"), message.WithDataBlockID("data"))
	nested := message.NewToolResultBlock("call", "tool", message.ToolResultOutput{Blocks: message.ContentBlockList{
		message.NewDataBlock(message.NewBase64Source(base64.StdEncoding.EncodeToString([]byte("nested")), "text/plain")),
	}})
	msg, err := message.NewMessage("assistant", message.RoleAssistant, []message.ContentBlock{message.NewTextBlock("hello"), data, nested})
	if err != nil {
		t.Fatal(err)
	}
	backend.files["/work/sessions/session/context.jsonl"] = []byte("existing\n")
	filename, err := w.OffloadContext(context.Background(), "session", []*message.Message{nil, msg})
	if err != nil {
		t.Fatalf("OffloadContext returned error: %v", err)
	}
	if filename != "/work/sessions/session/context.jsonl" || !strings.HasPrefix(string(backend.file(filename)), "existing\n") || !strings.Contains(string(backend.file(filename)), "file:///work/data/") {
		t.Fatalf("unexpected context file %q: %s", filename, backend.file(filename))
	}
	if _, ok := data.Source.(*message.Base64Source); !ok {
		t.Fatal("OffloadContext mutated its input message")
	}

	for _, sessionID := range []string{"", ".", "./", "a/..", "/absolute", "../../escape", "bad\x00id"} {
		if _, err := w.OffloadContext(context.Background(), sessionID, nil); err == nil {
			t.Fatalf("session id %q should fail", sessionID)
		}
	}
	if _, err := w.OffloadContext(canceledContext(), "session", nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled OffloadContext error = %v", err)
	}
	var nilWorkspace *Workspace
	if _, err := nilWorkspace.OffloadContext(context.Background(), "session", nil); err == nil {
		t.Fatal("nil workspace OffloadContext should fail")
	}
	w2, _, _, _, _ := newWorkspaceFixture(t)
	if _, err := w2.OffloadContext(context.Background(), "session", nil); err == nil {
		t.Fatal("uninitialized OffloadContext should fail")
	}

	t.Run("existing read error", func(t *testing.T) {
		w, backend, _, _, _ := readyWorkspace(t)
		filename := "/work/sessions/session/context.jsonl"
		backend.files[filename] = []byte("exists")
		backend.readHook = func(context.Context, string) ([]byte, error, bool) { return nil, errors.New("read failed"), true }
		_, err := w.OffloadContext(context.Background(), "session", nil)
		requireErrorContains(t, err, "read failed")
	})

	t.Run("write error", func(t *testing.T) {
		w, backend, _, _, _ := readyWorkspace(t)
		backend.writeHook = func(context.Context, string, []byte) (error, bool) { return errors.New("write failed"), true }
		_, err := w.OffloadContext(context.Background(), "session", nil)
		requireErrorContains(t, err, "write failed")
	})

	t.Run("marshal error", func(t *testing.T) {
		w, _, _, _, _ := readyWorkspace(t)
		bad := &message.Message{Role: message.RoleAssistant, Metadata: map[string]any{"bad": make(chan int)}}
		_, err := w.OffloadContext(context.Background(), "session", []*message.Message{bad})
		if err == nil {
			t.Fatal("unencodable message should fail")
		}
	})
}

func TestOffloadToolResultExecutableSpec(t *testing.T) {
	t.Parallel()

	w, backend, _, _, _ := readyWorkspace(t)
	raw := message.NewToolResultBlock("call/id", "tool", message.ToolResultOutput{Raw: "raw output"})
	backend.files["/work/sessions/session/tool_result-callid.txt"] = []byte("existing")
	filename, err := w.OffloadToolResult(context.Background(), "session", raw)
	if err != nil {
		t.Fatalf("OffloadToolResult raw returned error: %v", err)
	}
	if filename != "/work/sessions/session/tool_result-callid-1.txt" || string(backend.file(filename)) != "raw output" {
		t.Fatalf("raw tool result file %q = %q", filename, backend.file(filename))
	}

	data := message.NewDataBlock(
		message.NewBase64Source(base64.StdEncoding.EncodeToString([]byte("image")), "image/jpeg"),
		message.WithDataBlockName("photo"),
	)
	blocks := message.NewToolResultBlock("blocks", "tool", message.ToolResultOutput{Blocks: message.ContentBlockList{
		message.NewTextBlock("text"), data,
	}})
	filename, err = w.OffloadToolResult(context.Background(), "session", blocks)
	if err != nil {
		t.Fatalf("OffloadToolResult blocks returned error: %v", err)
	}
	if content := string(backend.file(filename)); !strings.Contains(content, "text") || !strings.Contains(content, "<data url='file:///work/data/") || !strings.Contains(content, "name='photo'") {
		t.Fatalf("block tool result = %q", content)
	}
	escaped := message.NewToolResultBlock("escaped", "tool", message.ToolResultOutput{Blocks: message.ContentBlockList{
		message.NewDataBlock(
			message.NewURLSource("file:///tmp/a?x='&<tag>", "text/'&<>"),
			message.WithDataBlockName("n'&<>"),
		),
	}})
	escapedFile, err := w.OffloadToolResult(context.Background(), "session", escaped)
	if err != nil {
		t.Fatalf("OffloadToolResult escaped metadata returned error: %v", err)
	}
	wantEscaped := "<data url='file:///tmp/a?x=&#39;&amp;&lt;tag&gt;' name='n&#39;&amp;&lt;&gt;' media_type='text/&#39;&amp;&lt;&gt;'/>"
	if content := string(backend.file(escapedFile)); content != wantEscaped {
		t.Fatalf("escaped data reference = %q, want %q", content, wantEscaped)
	}

	for _, sessionID := range []string{"", ".", "a/..", "/absolute", "../escape"} {
		if _, err := w.OffloadToolResult(context.Background(), sessionID, raw); err == nil {
			t.Fatalf("session id %q should fail", sessionID)
		}
	}
	if _, err := w.OffloadToolResult(context.Background(), "session", nil); err == nil {
		t.Fatal("nil tool result should fail")
	}
	if _, err := w.OffloadToolResult(canceledContext(), "session", raw); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled OffloadToolResult error = %v", err)
	}
	var nilWorkspace *Workspace
	if _, err := nilWorkspace.OffloadToolResult(context.Background(), "session", raw); err == nil {
		t.Fatal("nil workspace OffloadToolResult should fail")
	}
	w2, _, _, _, _ := newWorkspaceFixture(t)
	if _, err := w2.OffloadToolResult(context.Background(), "session", raw); err == nil {
		t.Fatal("uninitialized OffloadToolResult should fail")
	}

	t.Run("write failure", func(t *testing.T) {
		w, backend, _, _, _ := readyWorkspace(t)
		backend.writeHook = func(context.Context, string, []byte) (error, bool) { return errors.New("write failed"), true }
		_, err := w.OffloadToolResult(context.Background(), "session", raw)
		requireErrorContains(t, err, "write failed")
	})
}

func TestStoragePathAndParsingHelpers(t *testing.T) {
	t.Parallel()

	valid := []byte("---\r\nname: Example\r\ndescription: Does work\r\n---\r\n# Body\r\n")
	loaded, err := parseRemoteSkill("/work/skills/example/SKILL.md", valid)
	if err != nil || loaded.Name != "Example" || loaded.Description != "Does work" || loaded.Dir != "/work/skills/example" || !strings.Contains(loaded.Markdown, "# Body") || !loaded.UpdatedAt.IsZero() {
		t.Fatalf("parseRemoteSkill = %#v, %v", loaded, err)
	}
	for _, data := range [][]byte{
		[]byte("missing"),
		[]byte("---\nname: x"),
		[]byte("---\nname: [\ndescription: x\n---\n"),
		[]byte("---\nname: \ndescription: x\n---\n"),
	} {
		if _, err := parseRemoteSkill("/x/SKILL.md", data); err == nil {
			t.Fatalf("parseRemoteSkill(%q) should fail", data)
		}
	}

	for _, test := range []struct {
		root     string
		relative string
		want     string
		err      bool
	}{
		{root: "/sessions", relative: "one/two", want: "/sessions/one/two"},
		{root: "/sessions", relative: "", err: true},
		{root: "/sessions", relative: ".", err: true},
		{root: "/sessions", relative: "./", err: true},
		{root: "/sessions", relative: "a/..", err: true},
		{root: "/sessions", relative: "/absolute", err: true},
		{root: "/sessions", relative: "../escape", err: true},
		{root: "/sessions", relative: "bad\x00path", err: true},
	} {
		got, err := remoteChildPath(test.root, test.relative)
		if test.err && err == nil || !test.err && (err != nil || got != test.want) {
			t.Fatalf("remoteChildPath(%q) = %q, %v", test.relative, got, err)
		}
	}
	if !insideRemoteDir("/root", "/root") || !insideRemoteDir("/root", "/root/a") || insideRemoteDir("/root", "/rooted/a") {
		t.Fatal("insideRemoteDir returned unexpected result")
	}
	for input, want := range map[string]string{
		" My Skill ": "my-skill", "A__B": "a-b", "---": "", "中文 技能": "中文-技能",
	} {
		if got := sanitizeSkillDir(input); got != want {
			t.Fatalf("sanitizeSkillDir(%q) = %q, want %q", input, got, want)
		}
	}
	for input, want := range map[string]string{
		"": "result", " / ": "result", "call/id_1": "callid_1",
	} {
		if got := safeFileSegment(input); got != want {
			t.Fatalf("safeFileSegment(%q) = %q, want %q", input, got, want)
		}
	}

	media := map[string]string{
		"image/jpeg": ".jpg", "image/png": ".png", "image/gif": ".gif", "image/webp": ".webp", "image/svg+xml": ".svg",
		"audio/mpeg": ".mp3", "audio/wav": ".wav", "audio/x-wav": ".wav", "audio/ogg": ".ogg",
		"video/mp4": ".mp4", "video/webm": ".webm", "application/pdf": ".pdf", "text/plain": ".txt", "unknown/type": ".bin",
	}
	for mediaType, want := range media {
		if got := mediaExtension(mediaType); got != want {
			t.Fatalf("mediaExtension(%q) = %q, want %q", mediaType, got, want)
		}
	}
	name := "name"
	block := message.NewDataBlock(message.NewURLSource("file:///x", "text/plain"))
	dataBlockNameOption(&name)(block)
	if block.Name == nil || *block.Name != "name" {
		t.Fatal("dataBlockNameOption did not copy name")
	}
	dataBlockNameOption(nil)(block)
	if got := nonEmptyLines([]byte(" a \n\n b\n")); len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("nonEmptyLines = %#v", got)
	}
}

func TestPathExistsAndUniqueRemoteFile(t *testing.T) {
	t.Parallel()

	w, backend, _, _, _ := readyWorkspace(t)
	for _, exitCode := range []int{0, 1, 2} {
		backend.execHook = func(_ context.Context, argv []string, _ ExecOptions) (ExecResult, error, bool) {
			if len(argv) > 0 && argv[0] == "test" {
				return ExecResult{ExitCode: exitCode, Stderr: []byte("bad test")}, nil, true
			}
			return ExecResult{}, nil, false
		}
		exists, err := w.pathExists(context.Background(), "/x")
		if exitCode == 0 && (!exists || err != nil) || exitCode == 1 && (exists || err != nil) || exitCode == 2 && err == nil {
			t.Fatalf("pathExists exit %d = %t, %v", exitCode, exists, err)
		}
	}
	backend.execHook = func(context.Context, []string, ExecOptions) (ExecResult, error, bool) {
		return ExecResult{}, errors.New("exec failed"), true
	}
	if _, err := w.pathExists(context.Background(), "/x"); err == nil {
		t.Fatal("pathExists should wrap exec error")
	}
	backend.execHook = nil
	backend.files["/dir/result.txt"] = []byte("one")
	backend.files["/dir/result-1.txt"] = []byte("two")
	filename, err := w.uniqueRemoteFile(context.Background(), "/dir", "result.txt")
	if err != nil || filename != "/dir/result-2.txt" {
		t.Fatalf("uniqueRemoteFile = %q, %v", filename, err)
	}
}

func writeLocalSkill(t *testing.T, name, description string, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	content := "---\nname: " + strconv.Quote(name) + "\ndescription: " + strconv.Quote(description) + "\n---\n# " + name + "\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o600); err != nil {
		t.Fatalf("write local SKILL.md: %v", err)
	}
	for filename, data := range files {
		fullPath := filepath.Join(dir, filepath.FromSlash(filename))
		if err := os.MkdirAll(filepath.Dir(fullPath), 0o700); err != nil {
			t.Fatalf("create local skill directory: %v", err)
		}
		if err := os.WriteFile(fullPath, []byte(data), 0o600); err != nil {
			t.Fatalf("write local skill file: %v", err)
		}
	}
	return dir
}

func firstError[T any](value T, err error) error {
	return err
}

var _ skill.Loader = (*remoteSkillLoader)(nil)
