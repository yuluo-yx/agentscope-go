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

package tts

import (
	"encoding/base64"
	"encoding/binary"

	"github.com/yuluo-yx/agentscope-go/message"
)

const (
	defaultSampleRate    = 24000
	defaultChannels      = 1
	defaultBitsPerSample = 16
)

// NewAudioBlock wraps binary audio as a base64 data block.
func NewAudioBlock(data []byte, mediaType string) *message.DataBlock {
	if mediaType == "" {
		mediaType = "application/octet-stream"
	}
	return message.NewDataBlock(message.NewBase64Source(base64.StdEncoding.EncodeToString(data), mediaType))
}

// StreamingWAVHeader returns a RIFF/WAVE header for PCM streams whose final data size is not yet known.
func StreamingWAVHeader(sampleRate, channels, bitsPerSample int) []byte {
	header := wavHeader(sampleRate, channels, bitsPerSample, 0)
	binary.LittleEndian.PutUint32(header[4:8], 0xffffffff)
	binary.LittleEndian.PutUint32(header[40:44], 0xffffffff)
	return header
}

// WrapPCMAsWAV returns a complete WAV payload for PCM bytes.
func WrapPCMAsWAV(pcm []byte, sampleRate, channels, bitsPerSample int) []byte {
	header := wavHeader(sampleRate, channels, bitsPerSample, len(pcm))
	out := make([]byte, 0, len(header)+len(pcm))
	out = append(out, header...)
	out = append(out, pcm...)
	return out
}

func wavHeader(sampleRate, channels, bitsPerSample, dataSize int) []byte {
	sampleRate, channels, bitsPerSample = normalizeAudioShape(sampleRate, channels, bitsPerSample)
	byteRate := sampleRate * channels * bitsPerSample / 8
	blockAlign := channels * bitsPerSample / 8
	chunkSize := 36 + dataSize
	if dataSize > 0x7fffffff-36 {
		chunkSize = 0x7fffffff
	}

	sr := uint32(sampleRate) //nolint:gosec // G115: value bounded by normalizeAudioShape
	br := uint32(byteRate)   //nolint:gosec // G115: derived from normalized audio parameters
	ds := uint32(dataSize)   //nolint:gosec // G115: WAV data size fits in uint32 in practice

	cs := uint32(chunkSize) //nolint:gosec // G115: chunkSize clamped to 0x7fffffff above

	header := make([]byte, 44)
	copy(header[0:4], "RIFF")
	binary.LittleEndian.PutUint32(header[4:8], cs)
	copy(header[8:12], "WAVE")
	copy(header[12:16], "fmt ")
	binary.LittleEndian.PutUint32(header[16:20], 16)
	binary.LittleEndian.PutUint16(header[20:22], 1)
	binary.LittleEndian.PutUint16(header[22:24], uint16(channels&0xffff))
	binary.LittleEndian.PutUint32(header[24:28], sr)
	binary.LittleEndian.PutUint32(header[28:32], br)
	binary.LittleEndian.PutUint16(header[32:34], uint16(blockAlign&0xffff))
	binary.LittleEndian.PutUint16(header[34:36], uint16(bitsPerSample&0xffff))
	copy(header[36:40], "data")
	binary.LittleEndian.PutUint32(header[40:44], ds)
	return header
}

func normalizeAudioShape(sampleRate, channels, bitsPerSample int) (int, int, int) {
	if sampleRate <= 0 {
		sampleRate = defaultSampleRate
	}
	if channels <= 0 {
		channels = defaultChannels
	}
	if bitsPerSample <= 0 {
		bitsPerSample = defaultBitsPerSample
	}
	return sampleRate, channels, bitsPerSample
}
