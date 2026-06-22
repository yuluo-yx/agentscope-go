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

package stt

import (
	"encoding/base64"

	"github.com/yuluo-yx/agentscope-go/pkg/message"
)

// NewAudioBlock wraps binary audio as a base64 data block for recognition.
func NewAudioBlock(data []byte, mediaType string) *message.DataBlock {
	if mediaType == "" {
		mediaType = "application/octet-stream"
	}
	return message.NewDataBlock(message.NewBase64Source(base64.StdEncoding.EncodeToString(data), mediaType))
}
