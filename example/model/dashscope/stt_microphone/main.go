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
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/gen2brain/malgo"

	"github.com/yuluo-yx/agentscope-go/audio/stt"
	"github.com/yuluo-yx/agentscope-go/audio/stt/dashscope"
	"github.com/yuluo-yx/agentscope-go/credential"
)

const (
	defaultLanguage     = "zh"
	defaultSampleRate   = 16000
	defaultChunkMS      = 100
	defaultVADThreshold = 0.0
	defaultSilenceMS    = 400
	defaultQueueSize    = 32
	finishTimeout       = 10 * time.Second
)

type appConfig struct {
	apiKey       string
	language     string
	sampleRate   int
	chunkMS      int
	vadThreshold float64
	silenceMS    int
	queueSize    int
}

type microphone struct {
	context *malgo.AllocatedContext
	device  *malgo.Device
}

func main() {
	if err := run(context.Background(), os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "stt_microphone: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	cfg, err := parseConfig(args, stderr)
	if err != nil {
		return err
	}
	cfg, err = normalizeConfig(cfg)
	if err != nil {
		return err
	}

	runCtx, stop := signal.NotifyContext(ctx, os.Interrupt)
	defer stop()

	fmt.Fprintf(
		stderr,
		"listening language=%s sample_rate=%d chunk_ms=%d; press Ctrl+C to stop\n",
		cfg.language,
		cfg.sampleRate,
		cfg.chunkMS,
	)
	return runMicrophoneRecognition(runCtx, cfg, stdout, stderr)
}

func parseConfig(args []string, stderr io.Writer) (appConfig, error) {
	cfg := appConfig{
		apiKey: os.Getenv("AI_DASHSCOPE_API_KEY"),
	}
	fs := flag.NewFlagSet("stt_microphone", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.StringVar(&cfg.language, "language", defaultLanguage, "recognition language hint")
	fs.IntVar(&cfg.sampleRate, "sample-rate", defaultSampleRate, "capture sample rate: 8000 or 16000")
	fs.IntVar(&cfg.chunkMS, "chunk-ms", defaultChunkMS, "microphone audio chunk duration in milliseconds")
	fs.Float64Var(&cfg.vadThreshold, "vad-threshold", defaultVADThreshold, "server VAD threshold between -1 and 1")
	fs.IntVar(&cfg.silenceMS, "silence-ms", defaultSilenceMS, "server VAD silence duration in milliseconds")
	fs.IntVar(&cfg.queueSize, "queue-size", defaultQueueSize, "audio chunks buffered between microphone and network")
	if err := fs.Parse(args); err != nil {
		return appConfig{}, err
	}
	return cfg, nil
}

func normalizeConfig(cfg appConfig) (appConfig, error) {
	cfg.apiKey = strings.TrimSpace(cfg.apiKey)
	cfg.language = strings.TrimSpace(cfg.language)
	if cfg.language == "" {
		cfg.language = defaultLanguage
	}
	if cfg.sampleRate == 0 {
		cfg.sampleRate = defaultSampleRate
	}
	if cfg.chunkMS == 0 {
		cfg.chunkMS = defaultChunkMS
	}
	if cfg.silenceMS == 0 {
		cfg.silenceMS = defaultSilenceMS
	}
	if cfg.queueSize == 0 {
		cfg.queueSize = defaultQueueSize
	}

	if cfg.apiKey == "" {
		return appConfig{}, fmt.Errorf("AI_DASHSCOPE_API_KEY is required")
	}
	switch cfg.sampleRate {
	case 8000, 16000:
	default:
		return appConfig{}, fmt.Errorf("sample-rate must be 8000 or 16000")
	}
	if cfg.chunkMS < 20 || cfg.chunkMS > 1000 {
		return appConfig{}, fmt.Errorf("chunk-ms must be between 20 and 1000")
	}
	if cfg.vadThreshold < -1 || cfg.vadThreshold > 1 {
		return appConfig{}, fmt.Errorf("vad-threshold must be between -1 and 1")
	}
	if cfg.silenceMS < 200 || cfg.silenceMS > 6000 {
		return appConfig{}, fmt.Errorf("silence-ms must be between 200 and 6000")
	}
	if cfg.queueSize < 1 {
		return appConfig{}, fmt.Errorf("queue-size must be greater than 0")
	}
	return cfg, nil
}

func runMicrophoneRecognition(ctx context.Context, cfg appConfig, stdout, stderr io.Writer) error {
	model, err := dashscope.NewRealtimeModel(
		credential.NewDashScope(cfg.apiKey).STTCredential(),
		"qwen3-asr-flash-realtime",
		dashscope.WithRealtimeParameters(dashscope.RealtimeParameters{
			Language:           cfg.language,
			SampleRate:         cfg.sampleRate,
			Mode:               dashscope.RealtimeModeVAD,
			VADThreshold:       cfg.vadThreshold,
			VADSilenceDuration: time.Duration(cfg.silenceMS) * time.Millisecond,
		}),
	)
	if err != nil {
		return err
	}

	session, err := model.NewSession(ctx, stt.SessionRequest{})
	if err != nil {
		return err
	}
	defer func() { _ = session.Close(context.WithoutCancel(ctx)) }()

	sessionCtx, cancelSession := context.WithCancel(ctx)
	defer cancelSession()

	audioChunks := make(chan []byte, cfg.queueSize)
	senderDone := startAudioSender(sessionCtx, session, audioChunks)
	responseDone := startResponsePrinter(session, stdout)

	mic, err := startMicrophone(cfg, audioChunks)
	if err != nil {
		cancelSession()
		close(audioChunks)
		_ = session.Close(context.WithoutCancel(ctx))
		return err
	}

	var runErr error
	var senderStopped bool
	var responseStopped bool
	select {
	case <-ctx.Done():
		fmt.Fprintln(stderr, "\nstopping microphone and finishing session")
	case err := <-senderDone:
		senderStopped = true
		if err != nil {
			runErr = fmt.Errorf("push microphone audio: %w", err)
		} else {
			runErr = fmt.Errorf("audio sender stopped before microphone stopped")
		}
	case err := <-responseDone:
		responseStopped = true
		if err != nil {
			runErr = fmt.Errorf("receive transcript: %w", err)
		} else {
			runErr = fmt.Errorf("recognition session ended before microphone stopped")
		}
	}

	if err := mic.Close(); err != nil && runErr == nil {
		runErr = fmt.Errorf("stop microphone: %w", err)
	}
	close(audioChunks)

	if !senderStopped {
		waitCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), finishTimeout)
		err := waitForDone(waitCtx, senderDone, "flush microphone audio")
		cancel()
		if err != nil && runErr == nil {
			runErr = err
		}
	}

	if runErr != nil {
		cancelSession()
		_ = session.Close(context.WithoutCancel(ctx))
		return runErr
	}

	finishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), finishTimeout)
	defer cancel()
	if !responseStopped {
		if err := session.Finish(finishCtx); err != nil {
			_ = session.Close(context.WithoutCancel(ctx))
			return err
		}
		if err := waitForDone(finishCtx, responseDone, "receive final transcript"); err != nil {
			_ = session.Close(context.WithoutCancel(ctx))
			return err
		}
	}
	return nil
}

func startAudioSender(
	ctx context.Context,
	session stt.Session,
	audioChunks <-chan []byte,
) <-chan error {
	done := make(chan error, 1)
	go func() {
		for chunk := range audioChunks {
			if err := session.Push(ctx, stt.NewAudioBlock(chunk, "audio/pcm")); err != nil {
				done <- err
				return
			}
		}
		done <- nil
	}()
	return done
}

func startResponsePrinter(session stt.Session, stdout io.Writer) <-chan error {
	done := make(chan error, 1)
	go func() {
		for response := range session.Responses() {
			if response.Error != nil {
				done <- response.Error
				return
			}
			line, ok := formatResponseLine(response)
			if !ok {
				continue
			}
			if _, err := fmt.Fprint(stdout, line); err != nil {
				done <- err
				return
			}
		}
		done <- nil
	}()
	return done
}

func startMicrophone(cfg appConfig, audioChunks chan<- []byte) (*microphone, error) {
	audioContext, err := malgo.InitContext(nil, malgo.ContextConfig{}, nil)
	if err != nil {
		return nil, fmt.Errorf("initialize audio context: %w", err)
	}

	deviceConfig := malgo.DefaultDeviceConfig(malgo.Capture)
	deviceConfig.Capture.Format = malgo.FormatS16
	deviceConfig.Capture.Channels = 1
	deviceConfig.SampleRate = uint32(cfg.sampleRate)
	deviceConfig.PeriodSizeInFrames = uint32(cfg.sampleRate * cfg.chunkMS / 1000)
	deviceConfig.Alsa.NoMMap = 1

	callbacks := malgo.DeviceCallbacks{
		Data: func(_, inputSamples []byte, _ uint32) {
			if len(inputSamples) == 0 {
				return
			}
			chunk := append([]byte(nil), inputSamples...)
			select {
			case audioChunks <- chunk:
			default:
			}
		},
	}
	device, err := malgo.InitDevice(audioContext.Context, deviceConfig, callbacks)
	if err != nil {
		_ = audioContext.Uninit()
		audioContext.Free()
		return nil, fmt.Errorf("initialize microphone: %w", err)
	}
	if err := device.Start(); err != nil {
		device.Uninit()
		_ = audioContext.Uninit()
		audioContext.Free()
		return nil, fmt.Errorf("start microphone: %w", err)
	}
	return &microphone{context: audioContext, device: device}, nil
}

func (m *microphone) Close() error {
	if m == nil {
		return nil
	}
	var err error
	if m.device != nil {
		err = errors.Join(err, m.device.Stop())
		m.device.Uninit()
		m.device = nil
	}
	if m.context != nil {
		err = errors.Join(err, m.context.Uninit())
		m.context.Free()
		m.context = nil
	}
	return err
}

func waitForDone(ctx context.Context, done <-chan error, action string) error {
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return fmt.Errorf("%s: %w", action, ctx.Err())
	}
}

func formatResponseLine(response stt.Response) (string, bool) {
	text := strings.TrimSpace(response.Text)
	if text == "" {
		return "", false
	}
	if response.IsLast {
		return fmt.Sprintf("\rfinal: %s\n", text), true
	}
	return fmt.Sprintf("\rpartial: %s", text), true
}
