// Package pipeline builds the configured speech and conversation adapters.
//
// It is the single place that maps environment configuration to a concrete
// provider so the composition root stays free of provider switch statements.
// An unknown or empty provider is reported explicitly rather than silently
// falling back to a different vendor, which keeps billing and content policy
// predictable.
package pipeline

import (
	"fmt"
	"strings"

	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/adapter/asr"
	asraliyun "github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/adapter/asr/aliyun"
	asrwhisper "github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/adapter/asr/local_whisper"
	asropenai "github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/adapter/asr/openai"
	asrtencent "github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/adapter/asr/tencent"
	asrvolcano "github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/adapter/asr/volcano"
	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/adapter/realtime"
	realtimeopenai "github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/adapter/realtime/openai_realtime"
	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/adapter/tts"
	ttsaliyun "github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/adapter/tts/aliyun"
	ttspiper "github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/adapter/tts/local_piper"
	ttsopenai "github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/adapter/tts/openai"
	ttstencent "github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/adapter/tts/tencent"
	ttsvolcano "github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/adapter/tts/volcano"
	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/config"
	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/llm"
	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/llm/sub2api_client"
)

// Supported provider identifiers. They are lower-case so configuration is
// case-insensitive and matches the deployment docs.
const (
	ProviderOpenAI         = "openai"
	ProviderLocalWhisper   = "local_whisper"
	ProviderLocalPiper     = "local_piper"
	ProviderAliyun         = "aliyun"
	ProviderTencent        = "tencent"
	ProviderVolcano        = "volcano"
	ProviderOpenAIRealtime = "openai_realtime"
	ProviderSub2API        = "sub2api"
)

// Adapters groups the optional provider clients built from configuration.
//
// A nil field means that stage is not configured and must not be called. The
// composition root inspects these fields to decide which endpoints are exposed.
type Adapters struct {
	ASR      asr.Recognizer
	TTS      tts.Synthesizer
	Realtime realtime.Connector
	LLM      llm.Client
}

// Build creates the adapters selected by cfg. A provider that is named but
// unknown is an error so a typo cannot silently disable a stage.
func Build(cfg config.AudioConfig) (Adapters, error) {
	var adapters Adapters

	if provider := strings.TrimSpace(cfg.ASRProvider); provider != "" {
		recognizer, err := buildASR(provider, cfg.ASRConfig)
		if err != nil {
			return Adapters{}, err
		}
		adapters.ASR = recognizer
	}
	if provider := strings.TrimSpace(cfg.TTSProvider); provider != "" {
		synthesizer, err := buildTTS(provider, cfg.TTSConfig)
		if err != nil {
			return Adapters{}, err
		}
		adapters.TTS = synthesizer
	}
	if provider := strings.TrimSpace(cfg.RealtimeProvider); provider != "" {
		connector, err := buildRealtime(provider, cfg.TTSConfig)
		if err != nil {
			return Adapters{}, err
		}
		adapters.Realtime = connector
	}
	if provider := strings.TrimSpace(cfg.LLMProvider); provider != "" {
		client, err := buildLLM(provider, cfg.LLMConfig)
		if err != nil {
			return Adapters{}, err
		}
		adapters.LLM = client
	}
	return adapters, nil
}

func buildASR(provider string, providerConfig config.ASRProviderConfig) (asr.Recognizer, error) {
	switch provider {
	case ProviderOpenAI:
		return asropenai.New(asropenai.Config{
			BaseURL: providerConfig.Endpoint,
			APIKey:  providerConfig.APIKey,
			Model:   providerConfig.Model,
		}), nil
	case ProviderLocalWhisper:
		return asrwhisper.New(asrwhisper.Config{
			BaseURL: providerConfig.Endpoint,
			APIKey:  providerConfig.APIKey,
			Model:   providerConfig.Model,
		}), nil
	case ProviderAliyun:
		return asraliyun.New(asraliyun.Config{
			Endpoint: providerConfig.Endpoint,
			AppKey:   providerConfig.AppKey,
			Token:    providerConfig.Token,
		}), nil
	case ProviderTencent:
		return asrtencent.New(asrtencent.Config{
			Endpoint:  providerConfig.Endpoint,
			Region:    providerConfig.Region,
			SecretID:  providerConfig.SecretID,
			SecretKey: providerConfig.SecretKey,
		}), nil
	case ProviderVolcano:
		return asrvolcano.New(asrvolcano.Config{
			Endpoint:    providerConfig.Endpoint,
			AppID:       providerConfig.AppID,
			AccessToken: providerConfig.AccessToken,
			Cluster:     providerConfig.Cluster,
		}), nil
	default:
		return nil, fmt.Errorf("unsupported asr provider: %q", provider)
	}
}

func buildTTS(provider string, providerConfig config.TTSProviderConfig) (tts.Synthesizer, error) {
	switch provider {
	case ProviderOpenAI:
		return ttsopenai.New(ttsopenai.Config{
			BaseURL: providerConfig.Endpoint,
			APIKey:  providerConfig.APIKey,
			Model:   providerConfig.Model,
		}), nil
	case ProviderLocalPiper:
		return ttspiper.New(ttspiper.Config{
			BaseURL: providerConfig.Endpoint,
			APIKey:  providerConfig.APIKey,
		}), nil
	case ProviderAliyun:
		return ttsaliyun.New(ttsaliyun.Config{
			Endpoint: providerConfig.Endpoint,
			AppKey:   providerConfig.AppKey,
			Token:    providerConfig.Token,
		}), nil
	case ProviderTencent:
		return ttstencent.New(ttstencent.Config{
			Endpoint:  providerConfig.Endpoint,
			Region:    providerConfig.Region,
			SecretID:  providerConfig.SecretID,
			SecretKey: providerConfig.SecretKey,
		}), nil
	case ProviderVolcano:
		return ttsvolcano.New(ttsvolcano.Config{
			Endpoint:    providerConfig.Endpoint,
			AppID:       providerConfig.AppID,
			AccessToken: providerConfig.AccessToken,
			Cluster:     providerConfig.Cluster,
		}), nil
	default:
		return nil, fmt.Errorf("unsupported tts provider: %q", provider)
	}
}

func buildRealtime(provider string, providerConfig config.TTSProviderConfig) (realtime.Connector, error) {
	switch provider {
	case ProviderOpenAIRealtime:
		return realtimeopenai.New(realtimeopenai.Config{
			BaseURL: providerConfig.Endpoint,
			APIKey:  providerConfig.APIKey,
			Model:   providerConfig.Model,
			Voice:   providerConfig.Voice,
		}), nil
	default:
		return nil, fmt.Errorf("unsupported realtime provider: %q", provider)
	}
}

func buildLLM(provider string, providerConfig config.LLMProviderConfig) (llm.Client, error) {
	switch provider {
	case ProviderSub2API:
		return sub2api_client.New(providerConfig.BaseURL, providerConfig.APIKey), nil
	default:
		return nil, fmt.Errorf("unsupported llm provider: %q", provider)
	}
}
