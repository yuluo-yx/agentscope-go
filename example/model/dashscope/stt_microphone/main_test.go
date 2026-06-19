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

package main

import (
	"testing"

	"github.com/yuluo-yx/agentscope-go/audio/stt"
)

func TestNormalizeConfigDefaults(t *testing.T) {
	t.Parallel()

	cfg, err := normalizeConfig(appConfig{
		apiKey: "test-key",
	})
	if err != nil {
		t.Fatalf("normalize config: %v", err)
	}
	if cfg.language != "zh" {
		t.Fatalf("language = %q, want zh", cfg.language)
	}
	if cfg.sampleRate != 16000 {
		t.Fatalf("sampleRate = %d, want 16000", cfg.sampleRate)
	}
	if cfg.chunkMS != 100 {
		t.Fatalf("chunkMS = %d, want 100", cfg.chunkMS)
	}
	if cfg.silenceMS != 400 {
		t.Fatalf("silenceMS = %d, want 400", cfg.silenceMS)
	}
	if cfg.queueSize != 32 {
		t.Fatalf("queueSize = %d, want 32", cfg.queueSize)
	}
}

func TestNormalizeConfigRejectsInvalidValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		cfg  appConfig
	}{
		{name: "missing API key", cfg: appConfig{language: "zh", sampleRate: 16000, chunkMS: 100, silenceMS: 400}},
		{name: "unsupported sample rate", cfg: appConfig{apiKey: "test-key", language: "zh", sampleRate: 44100, chunkMS: 100, silenceMS: 400}},
		{name: "negative chunk", cfg: appConfig{apiKey: "test-key", language: "zh", sampleRate: 16000, chunkMS: -1, silenceMS: 400}},
		{name: "negative silence", cfg: appConfig{apiKey: "test-key", language: "zh", sampleRate: 16000, chunkMS: 100, silenceMS: -1}},
		{name: "negative queue", cfg: appConfig{apiKey: "test-key", language: "zh", sampleRate: 16000, chunkMS: 100, silenceMS: 400, queueSize: -1}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if _, err := normalizeConfig(tt.cfg); err == nil {
				t.Fatal("normalize config succeeded, want error")
			}
		})
	}
}

func TestFormatResponseLine(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		resp stt.Response
		want string
		ok   bool
	}{
		{
			name: "partial transcript",
			resp: stt.Response{Text: " 你好 ", IsLast: false},
			want: "\rpartial: 你好",
			ok:   true,
		},
		{
			name: "final transcript",
			resp: stt.Response{Text: "你好世界", IsLast: true},
			want: "\rfinal: 你好世界\n",
			ok:   true,
		},
		{
			name: "empty transcript",
			resp: stt.Response{Text: "   "},
			ok:   false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok := formatResponseLine(tt.resp)
			if ok != tt.ok {
				t.Fatalf("ok = %v, want %v", ok, tt.ok)
			}
			if got != tt.want {
				t.Fatalf("line = %q, want %q", got, tt.want)
			}
		})
	}
}
