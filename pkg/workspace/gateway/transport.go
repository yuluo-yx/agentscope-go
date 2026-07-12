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

package gateway

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"path"
	"strings"
	"unicode/utf8"
)

const (
	// DefaultMaxResponseBytes is the default response body limit for gateway clients.
	DefaultMaxResponseBytes int64 = 16 * 1024 * 1024

	maxResponseHeaderCount = 100
	maxResponseHeaderBytes = 64 * 1024
	maxBodyFilePathBytes   = 4096
	maxLoopbackErrorBytes  = 1024
)

// Request describes a gateway request independent of a concrete workspace backend.
type Request struct {
	Method           string
	Path             string
	Header           http.Header
	Body             []byte
	MaxResponseBytes int64
}

// Response describes a materialized gateway response returned by a Transport.
type Response struct {
	StatusCode int
	Header     http.Header
	Body       []byte
}

// Transport abstracts request execution across host HTTP and remote workspace loopback.
type Transport interface {
	RoundTrip(context.Context, *Request) (*Response, error)
}

// TransportFunc adapts a function to Transport.
type TransportFunc func(context.Context, *Request) (*Response, error)

// RoundTrip invokes the underlying function.
func (f TransportFunc) RoundTrip(ctx context.Context, request *Request) (*Response, error) {
	if f == nil {
		return nil, fmt.Errorf("workspace/gateway: nil transport function")
	}

	return f(ctx, request)
}

// BodyFileReader reads a response body spilled by the Python loopback shim.
// maxBytes is the read limit; returned data is checked again by the decoder.
type BodyFileReader func(ctx context.Context, filePath string, maxBytes int64) ([]byte, error)

type loopbackEnvelope struct {
	Status   int         `json:"status"`
	Header   http.Header `json:"headers,omitempty"`
	Body     *string     `json:"body,omitempty"`
	BodyFile *string     `json:"body_file,omitempty"`
	Error    string      `json:"error,omitempty"`
}

// DecodeLoopbackResponse strictly parses an AgentScope Python shim JSON envelope.
// Inline bodies use standard Base64. Spilled body_file content is loaded through
// readBodyFile so this package does not depend on a concrete workspace Backend.
func DecodeLoopbackResponse(
	ctx context.Context,
	payload []byte,
	maxBytes int64,
	readBodyFile BodyFileReader,
) (*Response, error) {
	if ctx == nil {
		return nil, fmt.Errorf("workspace/gateway: nil context")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if maxBytes <= 0 {
		return nil, fmt.Errorf("workspace/gateway: loopback response limit must be positive")
	}
	if int64(len(payload)) > loopbackEnvelopeLimit(maxBytes) {
		return nil, fmt.Errorf("workspace/gateway: loopback envelope exceeds its size limit")
	}

	envelope, err := parseLoopbackEnvelope(payload)
	if err != nil {
		return nil, err
	}
	if err := validateLoopbackEnvelope(envelope); err != nil {
		return nil, err
	}
	body, err := readLoopbackBody(ctx, envelope, maxBytes, readBodyFile)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if int64(len(body)) > maxBytes {
		return nil, fmt.Errorf("workspace/gateway: loopback response body exceeds %d bytes", maxBytes)
	}

	return &Response{
		StatusCode: envelope.Status,
		Header:     envelope.Header.Clone(),
		Body:       bytes.Clone(body),
	}, nil
}

func parseLoopbackEnvelope(payload []byte) (*loopbackEnvelope, error) {

	var envelope loopbackEnvelope
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&envelope); err != nil {
		return nil, fmt.Errorf("workspace/gateway: decode loopback envelope: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != nil && err != io.EOF {
		return nil, fmt.Errorf("workspace/gateway: decode loopback envelope: %w", err)
	} else if err == nil {
		return nil, fmt.Errorf("workspace/gateway: loopback envelope contains trailing JSON")
	}

	return &envelope, nil
}

func validateLoopbackEnvelope(envelope *loopbackEnvelope) error {

	if envelope.Status == -1 {
		if envelope.Body != nil || envelope.BodyFile != nil || len(envelope.Header) != 0 {
			return fmt.Errorf("workspace/gateway: invalid loopback error envelope")
		}
		message := sanitizeLoopbackError(envelope.Error)
		if message == "" {
			message = "unknown error"
		}
		return fmt.Errorf("workspace/gateway: loopback request failed: %s", message)
	}
	if envelope.Status < 100 || envelope.Status > 599 {
		return fmt.Errorf("workspace/gateway: loopback returned an invalid status")
	}
	if envelope.Error != "" {
		return fmt.Errorf("workspace/gateway: successful loopback envelope contains an error")
	}
	if (envelope.Body == nil) == (envelope.BodyFile == nil) {
		return fmt.Errorf("workspace/gateway: loopback response must contain exactly one body source")
	}

	return validateHTTPHeader(envelope.Header)
}

func readLoopbackBody(
	ctx context.Context,
	envelope *loopbackEnvelope,
	maxBytes int64,
	readBodyFile BodyFileReader,
) ([]byte, error) {
	if envelope.Body != nil {
		encoded := *envelope.Body
		decodedLength := base64.StdEncoding.DecodedLen(len(encoded))
		if strings.HasSuffix(encoded, "=") {
			decodedLength--
		}
		if strings.HasSuffix(encoded, "==") {
			decodedLength--
		}
		if int64(decodedLength) > maxBytes {
			return nil, fmt.Errorf("workspace/gateway: loopback response body exceeds %d bytes", maxBytes)
		}
		body, err := base64.StdEncoding.Strict().DecodeString(encoded)
		if err != nil {
			return nil, fmt.Errorf("workspace/gateway: loopback body is not valid base64")
		}

		return body, nil
	}

	filePath, err := validateBodyFilePath(*envelope.BodyFile)
	if err != nil {
		return nil, err
	}
	if readBodyFile == nil {
		return nil, fmt.Errorf("workspace/gateway: loopback response requires a body-file reader")
	}
	body, err := readBodyFile(ctx, filePath, maxBytes)
	if err != nil {
		return nil, fmt.Errorf("workspace/gateway: read loopback body file: %w", err)
	}

	return body, nil
}

func loopbackEnvelopeLimit(maxBytes int64) int64 {

	// Base64 is at most about 4/3 of the raw body; reserve fixed JSON and header overhead too.
	if maxBytes > (math.MaxInt64-maxResponseHeaderBytes)/(2) {
		return math.MaxInt64
	}

	return maxBytes*2 + maxResponseHeaderBytes
}

func validateBodyFilePath(filePath string) (string, error) {

	if filePath == "" || len(filePath) > maxBodyFilePathBytes || !utf8.ValidString(filePath) || strings.ContainsRune(filePath, 0) {
		return "", fmt.Errorf("workspace/gateway: invalid loopback body-file path")
	}
	if !path.IsAbs(filePath) || path.Clean(filePath) != filePath {
		return "", fmt.Errorf("workspace/gateway: loopback body-file path must be clean and absolute")
	}

	return filePath, nil
}

func validateHTTPHeader(header http.Header) error {
	return validateHeader(header, "response")
}

func validateRequestHeader(header http.Header) error {
	return validateHeader(header, "request")
}

func validateHeader(header http.Header, kind string) error {

	if len(header) > maxResponseHeaderCount {
		return fmt.Errorf("workspace/gateway: %s has too many headers", kind)
	}

	total := 0
	for key, values := range header {
		if !validHeaderName(key) {
			return fmt.Errorf("workspace/gateway: %s contains an invalid header name", kind)
		}
		if len(values) == 0 {
			return fmt.Errorf("workspace/gateway: %s contains an invalid header value", kind)
		}
		total += len(key)
		for _, value := range values {
			if !validHeaderValue(value) {
				return fmt.Errorf("workspace/gateway: %s contains an invalid header value", kind)
			}
			total += len(value)
			if total > maxResponseHeaderBytes {
				return fmt.Errorf("workspace/gateway: %s headers exceed their size limit", kind)
			}
		}
	}

	return nil
}

func validHeaderName(name string) bool {

	if name == "" {
		return false
	}
	for i := 0; i < len(name); i++ {
		char := name[i]
		if !isHTTPToken(char) {
			return false
		}
	}

	return true
}

func isHTTPToken(char byte) bool {

	if char >= '0' && char <= '9' || char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' {
		return true
	}

	return strings.ContainsRune("!#$%&'*+-.^_`|~", rune(char))
}

func validHeaderValue(value string) bool {

	for i := 0; i < len(value); i++ {
		char := value[i]
		if char == '\t' {
			continue
		}
		if char < 0x20 || char == 0x7f {
			return false
		}
	}

	return true
}

func sanitizeLoopbackError(message string) string {

	message = strings.TrimSpace(message)
	if len(message) > maxLoopbackErrorBytes {
		message = message[:maxLoopbackErrorBytes]
	}
	return strings.Map(func(char rune) rune {
		if char < 0x20 || char == 0x7f {
			return ' '
		}
		return char
	}, message)
}
