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

package builtin

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"mvdan.cc/sh/v3/syntax"

	"github.com/yuluo-yx/agentscope-go/permission"
	astate "github.com/yuluo-yx/agentscope-go/state"
	astool "github.com/yuluo-yx/agentscope-go/tool"
)

const (
	defaultBashTimeoutMS = 120000
	maxBashTimeoutMS     = 600000
)

// Bash executes bash commands and returns stdout/stderr.
type Bash struct {
	baseTool
}

// NewBash creates the Bash tool.
func NewBash() *Bash {
	return &Bash{baseTool: baseTool{
		name:            "Bash",
		description:     "Executes a bash command and returns its combined output.",
		concurrencySafe: false,
		readOnly:        false,
		stateInjected:   false,
		schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"command":     map[string]any{"type": "string", "description": "The bash command to execute."},
				"description": map[string]any{"type": "string", "description": "Clear description of what the command does."},
				"timeout_ms":  map[string]any{"type": "integer", "description": "Timeout in milliseconds.", "default": defaultBashTimeoutMS},
			},
			"required": []string{"command"},
		},
	}}
}

// CheckPermissions performs safety checks for Bash command execution.
func (b *Bash) CheckPermissions(_ context.Context, input map[string]any, ctx *permission.Context) (*permission.Decision, error) {
	command := strings.TrimSpace(stringValue(input, "command"))
	if command == "" {
		return &permission.Decision{
			Behavior:       permission.BehaviorDeny,
			Message:        "Empty command",
			DecisionReason: "Bash command is empty",
		}, nil
	}
	file, err := parseBash(command)
	if err != nil {
		return &permission.Decision{
			Behavior:       permission.BehaviorAsk,
			Message:        "Permission required: Bash command has invalid shell syntax",
			DecisionReason: "Safety check: invalid shell syntax: " + err.Error(),
		}, nil
	}
	if reason := injectionRisk(file); reason != "" {
		return &permission.Decision{
			Behavior:       permission.BehaviorAsk,
			Message:        "Permission required: Bash command contains dynamic shell constructs",
			DecisionReason: "Safety check: " + reason,
		}, nil
	}
	if isReadOnlyCommand(file) {
		return &permission.Decision{
			Behavior:       permission.BehaviorAllow,
			Message:        "Permission granted for read-only command",
			DecisionReason: "Read-only command is allowed",
		}, nil
	}
	if pattern := dangerousCommand(file); pattern != "" {
		return &permission.Decision{
			Behavior:       permission.BehaviorAsk,
			Message:        fmt.Sprintf("Permission required: Bash command matches dangerous pattern: %s", pattern),
			DecisionReason: "Safety check: dangerous command pattern " + pattern,
		}, nil
	}
	if reason := sedSafetyViolation(file); reason != "" {
		return &permission.Decision{
			Behavior:       permission.BehaviorAsk,
			Message:        "Permission required: " + reason,
			DecisionReason: "Safety check: sed command requires explicit review",
		}, nil
	}
	if path := dangerousBashPath(file); path != "" {
		return &permission.Decision{
			Behavior:       permission.BehaviorAsk,
			Message:        "Permission required: Bash command operates on sensitive path " + path,
			DecisionReason: "Safety check: dangerous file or directory in bash command",
		}, nil
	}
	if ctx != nil && ctx.Mode == permission.ModeAcceptEdits {
		if base := baseCommand(file); isFilesystemCommand(base) {
			return &permission.Decision{
				Behavior:       permission.BehaviorAllow,
				Message:        fmt.Sprintf("Permission granted for %q command (accept edits mode - filesystem command)", base),
				DecisionReason: fmt.Sprintf("Filesystem command %q allowed in accept edits mode", base),
			}, nil
		}
	}
	return &permission.Decision{
		Behavior: permission.BehaviorPassthrough,
		Message:  "Execute bash command: " + command,
	}, nil
}

// MatchRule matches permission rules against the command string.
func (b *Bash) MatchRule(ruleContent string, input map[string]any) bool {
	command := stringValue(input, "command")
	if strings.HasSuffix(ruleContent, ":*") {
		prefix := strings.TrimSpace(strings.TrimSuffix(ruleContent, ":*"))
		return matchBashPrefixRule(prefix, command)
	}
	return permission.MatchPattern(ruleContent, command)
}

// GenerateSuggestions generates suggested permission rules from command prefixes.
func (b *Bash) GenerateSuggestions(input map[string]any) []permission.Rule {
	command := strings.TrimSpace(stringValue(input, "command"))
	if command == "" {
		return nil
	}
	prefix := commandPrefix(command)
	return []permission.Rule{{
		ToolName:    b.Name(),
		RuleContent: prefix + ":*",
		Behavior:    permission.BehaviorAllow,
		Source:      sourceSuggested,
	}}
}

// Execute runs the Bash command and returns combined output.
func (b *Bash) Execute(ctx context.Context, input map[string]any, _ *astate.AgentState) (<-chan astool.ToolChunk, error) {
	command := strings.TrimSpace(stringValue(input, "command"))
	if command == "" {
		return errorText("Error: command is required"), nil
	}
	timeoutMS := intValue(input, "timeout_ms", defaultBashTimeoutMS)
	if timeoutMS <= 0 {
		timeoutMS = defaultBashTimeoutMS
	}
	if timeoutMS > maxBashTimeoutMS {
		timeoutMS = maxBashTimeoutMS
	}
	execCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutMS)*time.Millisecond)
	defer cancel()
	cmd := exec.CommandContext(execCtx, "/bin/bash", "-lc", command)
	output, err := cmd.CombinedOutput()
	text := string(output)
	if execCtx.Err() == context.DeadlineExceeded {
		return errorText(fmt.Sprintf("Command timed out after %dms: %s", timeoutMS, command)), nil
	}
	if err != nil {
		if text == "" {
			text = err.Error()
		}
		return errorText(fmt.Sprintf("Command failed: %s\n%s", command, text)), nil
	}
	return successText(text), nil
}

func parseBash(command string) (*syntax.File, error) {
	return syntax.NewParser(syntax.Variant(syntax.LangBash)).Parse(strings.NewReader(command), "")
}

func injectionRisk(file *syntax.File) string {
	reason := ""
	syntax.Walk(file, func(node syntax.Node) bool {
		if reason != "" || node == nil {
			return false
		}
		switch node.(type) {
		case *syntax.CmdSubst:
			reason = "command substitution cannot be statically analyzed"
		case *syntax.ProcSubst:
			reason = "process substitution cannot be statically analyzed"
		case *syntax.Subshell:
			reason = "subshell cannot be statically analyzed"
		case *syntax.IfClause, *syntax.WhileClause, *syntax.ForClause, *syntax.CaseClause, *syntax.FuncDecl, *syntax.TestClause:
			reason = "control flow cannot be statically analyzed"
		}
		return reason == ""
	})
	return reason
}

func dangerousCommand(file *syntax.File) string {
	if pattern := dangerousRedirection(file); pattern != "" {
		return pattern
	}
	for _, call := range commandCalls(file) {
		if pattern := dangerousCall(call); pattern != "" {
			return pattern
		}
	}
	return ""
}

func dangerousCall(call *syntax.CallExpr) string {
	words := callLiteralWords(call)
	return dangerousWords(words)
}

func dangerousWords(words []string) string {
	if len(words) == 0 {
		return ""
	}
	base := words[0]
	switch base {
	case "rm":
		return dangerousRemovePattern(words[1:])
	case "rmdir":
		return dangerousRemovePathPattern(words[1:])
	case "sudo":
		if pattern := dangerousWords(words[1:]); pattern != "" {
			return "sudo " + pattern
		}
	case "dd":
		return "dd"
	case "mkfs", "fdisk", "format":
		return base
	case "chmod":
		return dangerousChmodPattern(words[1:])
	case "chown":
		if containsArg(words[1:], "-R") {
			return "chown -R"
		}
	case "kill":
		if containsArg(words[1:], "-9") {
			return "kill -9"
		}
	}
	return ""
}

func dangerousRemovePattern(args []string) string {
	if path := dangerousRemovePathPattern(args); path != "" {
		return path
	}
	if hasShortFlag(args, "r") && hasShortFlag(args, "f") {
		return "rm -rf"
	}
	return ""
}

func dangerousRemovePathPattern(args []string) string {
	for _, arg := range args {
		if arg == "" || strings.HasPrefix(arg, "-") {
			continue
		}
		if isBroadRemovalPattern(arg) {
			return "dangerous removal path " + arg
		}
		if isRootOrRootChild(arg) {
			return "dangerous removal path " + arg
		}
	}
	return ""
}

func dangerousChmodPattern(args []string) string {
	if containsArg(args, "777") {
		if containsArg(args, "-R") {
			return "chmod -R 777"
		}
		return "chmod 777"
	}
	return ""
}

func dangerousRedirection(file *syntax.File) string {
	for _, redir := range commandRedirs(file) {
		if !isOutputRedirection(redir.Op) || redir.Word == nil {
			continue
		}
		target := wordLiteral(redir.Word)
		if strings.HasPrefix(target, "/dev/") {
			return "> /dev/"
		}
	}
	return ""
}

func sedSafetyViolation(file *syntax.File) string {
	for _, call := range commandCalls(file) {
		words := callLiteralWords(call)
		if len(words) == 0 || commandName(words[0]) != "sed" {
			continue
		}
		if reason := checkSedArgs(words[1:]); reason != "" {
			return reason
		}
	}
	return ""
}

func checkSedArgs(args []string) string {
	flags, expressions, fileArgs := splitSedArgs(args)
	if len(expressions) == 0 {
		return "sed command missing expression"
	}
	if flag := disallowedSedFlag(flags); flag != "" {
		return "sed flag -" + flag + " not allowed"
	}
	hasNFlag := containsArg(flags, "n")
	hasIFlag := containsArg(flags, "i")
	for _, expr := range expressions {
		if reason := checkSedExpression(expr, hasNFlag); reason != "" {
			return reason
		}
	}
	if hasIFlag {
		for _, filePath := range fileArgs {
			if isDangerousPath(filePath) {
				return "sed -i modifying dangerous file: " + filePath
			}
		}
	}
	return ""
}

func splitSedArgs(args []string) ([]string, []string, []string) {
	flags := make([]string, 0)
	expressions := make([]string, 0)
	fileArgs := make([]string, 0)
	foundFirstExpr := false
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--in-place":
			flags = append(flags, "i")
		case arg == "-e" || arg == "--expression":
			flags = append(flags, "e")
			if i+1 < len(args) {
				expressions = append(expressions, args[i+1])
				i++
			}
		case strings.HasPrefix(arg, "-") && !strings.HasPrefix(arg, "--"):
			flags = append(flags, splitSedShortFlags(strings.TrimPrefix(arg, "-"))...)
		case !foundFirstExpr:
			expressions = append(expressions, arg)
			foundFirstExpr = true
		default:
			fileArgs = append(fileArgs, arg)
		}
	}
	return flags, expressions, fileArgs
}

func splitSedShortFlags(flags string) []string {
	result := make([]string, 0, len(flags))
	for _, flag := range flags {
		result = append(result, string(flag))
	}
	return result
}

func disallowedSedFlag(flags []string) string {
	for _, flag := range flags {
		switch flag {
		case "n", "E", "e", "i":
		default:
			return flag
		}
	}
	return ""
}

func checkSedExpression(expr string, hasNFlag bool) string {
	if sedExpressionWrites(expr) {
		return "sed write operation (w/W) not allowed"
	}
	if sedExpressionExecutes(expr) {
		return "sed execute operation (e/E) not allowed"
	}
	if strings.ContainsAny(expr, "{}") {
		return "sed curly braces not allowed"
	}
	if strings.HasPrefix(expr, "!") {
		return "sed negation (!) not allowed"
	}
	if strings.Contains(expr, "#") && !strings.HasPrefix(expr, "s#") {
		return "sed comments not allowed"
	}
	if hasNFlag && isSedPrintExpression(expr) {
		return ""
	}
	if isSedSubstitutionExpression(expr) {
		return ""
	}
	return fmt.Sprintf("sed expression %q not in allowlist", expr)
}

func sedExpressionWrites(expr string) bool {
	return strings.HasSuffix(expr, "/w") || strings.HasSuffix(expr, "/W") ||
		strings.Contains(expr, "/w ") || strings.Contains(expr, "/W ")
}

func sedExpressionExecutes(expr string) bool {
	return strings.HasSuffix(expr, "/e") || strings.HasSuffix(expr, "/E") ||
		strings.Contains(expr, "/e ") || strings.Contains(expr, "/E ")
}

func isSedPrintExpression(expr string) bool {
	if !strings.HasSuffix(expr, "p") {
		return false
	}
	body := strings.TrimSuffix(expr, "p")
	if body == "" {
		return false
	}
	parts := strings.Split(body, ",")
	if len(parts) > 2 {
		return false
	}
	for _, part := range parts {
		if !isDecimalDigits(part) {
			return false
		}
	}
	return true
}

func isSedSubstitutionExpression(expr string) bool {
	if len(expr) < 3 || expr[0] != 's' {
		return false
	}
	delimiter := expr[1]
	if delimiter != '/' && delimiter != '|' && delimiter != '#' {
		return false
	}
	parts := strings.Split(expr[2:], string(delimiter))
	if len(parts) < 2 {
		return false
	}
	if len(parts) == 2 {
		return true
	}
	for _, flag := range parts[2] {
		if !strings.ContainsRune("gp0123456789", flag) {
			return false
		}
	}
	return true
}

func isDecimalDigits(text string) bool {
	if text == "" {
		return false
	}
	for _, char := range text {
		if char < '0' || char > '9' {
			return false
		}
	}
	return true
}

func dangerousBashPath(file *syntax.File) string {
	for _, path := range bashFilePaths(file) {
		if isDangerousPath(path) {
			return path
		}
	}
	return ""
}

func bashFilePaths(file *syntax.File) []string {
	paths := make([]string, 0)
	for _, redir := range commandRedirs(file) {
		if isOutputRedirection(redir.Op) && redir.Word != nil {
			paths = append(paths, wordLiteral(redir.Word))
		}
	}
	for _, call := range commandCalls(file) {
		words := callLiteralWords(call)
		if len(words) == 0 || !isFileManipulatingCommand(words[0]) {
			continue
		}
		for _, arg := range words[1:] {
			if arg == "" || strings.HasPrefix(arg, "-") {
				continue
			}
			paths = append(paths, arg)
		}
	}
	return paths
}

func isFileManipulatingCommand(command string) bool {
	switch commandName(command) {
	case "rm", "mv", "cp", "chmod", "chown", "chgrp", "touch", "ln", "sed":
		return true
	default:
		return false
	}
}

func isReadOnlyCommand(file *syntax.File) bool {
	if hasOutputRedirection(file) {
		return false
	}
	calls := commandCalls(file)
	if len(calls) == 0 {
		return false
	}
	for _, call := range calls {
		if !isSingleCallReadOnly(call) {
			return false
		}
	}
	return true
}

func hasOutputRedirection(file *syntax.File) bool {
	for _, redir := range commandRedirs(file) {
		if isOutputRedirection(redir.Op) {
			return true
		}
	}
	return false
}

func isOutputRedirection(op syntax.RedirOperator) bool {
	switch op {
	case syntax.RdrOut, syntax.AppOut, syntax.RdrInOut, syntax.DplOut,
		syntax.RdrClob, syntax.AppClob, syntax.RdrAll, syntax.RdrAllClob,
		syntax.AppAll, syntax.AppAllClob:
		return true
	default:
		return false
	}
}

func isSingleCallReadOnly(call *syntax.CallExpr) bool {
	words := callLiteralWords(call)
	if len(words) == 0 {
		return false
	}
	base := words[0]
	safeCommands := map[string]bool{
		"pwd": true, "ls": true, "cat": true, "head": true, "tail": true,
		"awk": true, "grep": true, "rg": true, "find": true, "wc": true,
		"sort": true, "uniq": true, "echo": true, "printf": true, "which": true,
	}
	if safeCommands[base] {
		return true
	}
	if base == "git" && len(words) >= 2 {
		switch words[1] {
		case "status", "diff", "log", "show", "branch", "remote", "rev-parse":
			return true
		}
	}
	if base == "docker" && len(words) >= 2 {
		switch words[1] {
		case "ps", "images", "inspect", "logs", "version", "info":
			return true
		}
	}
	return false
}

func baseCommand(file *syntax.File) string {
	call := firstCommandCall(file)
	if call == nil {
		return ""
	}
	words := callLiteralWords(call)
	if len(words) == 0 {
		return ""
	}
	return words[0]
}

func isFilesystemCommand(command string) bool {
	switch command {
	case "mkdir", "touch", "cp", "mv", "rm", "rmdir", "chmod", "chown":
		return true
	default:
		return false
	}
}

func commandPrefix(command string) string {
	file, err := parseBash(command)
	if err != nil {
		return fallbackCommandPrefix(command)
	}
	call := firstCommandCall(file)
	if call == nil {
		return ""
	}
	words := callLiteralWords(call)
	if len(words) == 0 {
		return ""
	}
	if len(words) == 1 || strings.HasPrefix(words[1], "-") {
		return words[0]
	}
	return words[0] + " " + words[1]
}

func matchBashPrefixRule(prefix, command string) bool {
	if prefix == "" {
		return true
	}
	file, err := parseBash(command)
	if err != nil {
		return permission.MatchPattern(prefix+":*", command)
	}
	calls := commandCalls(file)
	if len(calls) == 0 {
		return false
	}
	for _, call := range calls {
		if !callMatchesPrefix(callLiteralWords(call), prefix) {
			return false
		}
	}
	return true
}

func callMatchesPrefix(words []string, prefix string) bool {
	prefixWords := strings.Fields(prefix)
	if len(prefixWords) == 0 || len(words) < len(prefixWords) {
		return false
	}
	for i, prefixWord := range prefixWords {
		if words[i] != prefixWord {
			return false
		}
	}
	return true
}

func fallbackCommandPrefix(command string) string {
	fields := strings.Fields(command)
	for len(fields) > 0 && strings.Contains(fields[0], "=") {
		fields = fields[1:]
	}
	if len(fields) == 0 {
		return ""
	}
	if len(fields) == 1 || strings.HasPrefix(fields[1], "-") {
		return fields[0]
	}
	return fields[0] + " " + fields[1]
}

func firstCommandCall(file *syntax.File) *syntax.CallExpr {
	calls := commandCalls(file)
	if len(calls) == 0 {
		return nil
	}
	return calls[0]
}

func commandCalls(file *syntax.File) []*syntax.CallExpr {
	calls := make([]*syntax.CallExpr, 0)
	syntax.Walk(file, func(node syntax.Node) bool {
		if node == nil {
			return false
		}
		call, ok := node.(*syntax.CallExpr)
		if ok {
			calls = append(calls, call)
		}
		return true
	})
	return calls
}

func commandRedirs(file *syntax.File) []*syntax.Redirect {
	redirs := make([]*syntax.Redirect, 0)
	syntax.Walk(file, func(node syntax.Node) bool {
		if node == nil {
			return false
		}
		stmt, ok := node.(*syntax.Stmt)
		if ok {
			redirs = append(redirs, stmt.Redirs...)
		}
		return true
	})
	return redirs
}

func callLiteralWords(call *syntax.CallExpr) []string {
	words := make([]string, 0, len(call.Args))
	for _, arg := range call.Args {
		words = append(words, wordLiteral(arg))
	}
	return words
}

func wordLiteral(word *syntax.Word) string {
	if word == nil {
		return ""
	}
	var builder strings.Builder
	for _, part := range word.Parts {
		text, ok := wordPartLiteral(part)
		if !ok {
			return ""
		}
		builder.WriteString(text)
	}
	return builder.String()
}

func wordPartLiteral(part syntax.WordPart) (string, bool) {
	switch typed := part.(type) {
	case *syntax.Lit:
		return typed.Value, true
	case *syntax.SglQuoted:
		return typed.Value, true
	case *syntax.DblQuoted:
		var builder strings.Builder
		for _, nested := range typed.Parts {
			text, ok := wordPartLiteral(nested)
			if !ok {
				return "", false
			}
			builder.WriteString(text)
		}
		return builder.String(), true
	case *syntax.ParamExp:
		if isSimpleParamExpansion(typed) {
			return "$" + typed.Param.Value, true
		}
	}
	return "", false
}

func isSimpleParamExpansion(exp *syntax.ParamExp) bool {
	return exp != nil &&
		exp.Param != nil &&
		exp.NestedParam == nil &&
		exp.Index == nil &&
		exp.Exp == nil &&
		exp.Repl == nil &&
		exp.Slice == nil &&
		exp.Names == 0 &&
		exp.Flags == nil &&
		!exp.Excl &&
		!exp.Length &&
		!exp.Width &&
		!exp.IsSet
}

func containsArg(args []string, want string) bool {
	for _, arg := range args {
		if arg == want {
			return true
		}
	}
	return false
}

func hasShortFlag(args []string, flag string) bool {
	for _, arg := range args {
		if strings.HasPrefix(arg, "--") || !strings.HasPrefix(arg, "-") {
			continue
		}
		if strings.Contains(strings.TrimPrefix(arg, "-"), flag) {
			return true
		}
	}
	return false
}

func commandName(command string) string {
	return filepath.Base(command)
}

func isBroadRemovalPattern(path string) bool {
	return path == "*" || path == "./*" || strings.HasSuffix(path, "/*") || strings.HasSuffix(path, `\*`)
}

func isRootOrRootChild(rawPath string) bool {
	path := rawPath
	if path == "~" {
		home, err := os.UserHomeDir()
		if err == nil {
			path = home
		}
	}
	cleaned := strings.TrimRight(path, "/")
	if cleaned == "" {
		cleaned = "/"
	}
	if cleaned == "/" {
		return true
	}
	if !strings.HasPrefix(cleaned, "/") {
		return false
	}
	return strings.Count(strings.Trim(cleaned, "/"), "/") == 0
}
