// Package app wires and runs the voice gateway.
package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/TissyBoxC/sprout-platform/packages/go/observability"
	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/audio/frame"
	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/audio/preprocess"
	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/audio/reference"
	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/audio/vad"
	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/config"
	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/conversation"
	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/pipeline"
	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/platform/cache"
	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/session"
	gatewayhttp "github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/transport/http"
	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/transport/websocket"
	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/usage"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Run starts the voice gateway and waits for a shutdown signal.
func Run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	logger := slog.New(observability.NewRedactingHandler(slog.NewJSONHandler(
		os.Stdout,
		&slog.HandlerOptions{
			Level: cfg.Log.SlogLevel(),
		},
	)))
	slog.SetDefault(logger)

	startupCtx, startupCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer startupCancel()

	redisCache, err := cache.Open(startupCtx, cfg.Redis.Address, cfg.Redis.Password)
	if err != nil {
		return fmt.Errorf("open redis: %w", err)
	}
	defer redisCache.Close()

	var usageRecorder usage.Recorder
	if cfg.Database.DSN != "" {
		databaseStore, err := pgxpool.New(startupCtx, cfg.Database.DSN)
		if err != nil {
			return fmt.Errorf("open usage database: %w", err)
		}
		defer databaseStore.Close()
		usageRecorder = usage.NewPostgresRecorder(databaseStore)
	}

	realtimeHandler, realtimeShutdown, err := buildRealtimeTransport(cfg, logger)
	if err != nil {
		return err
	}
	defer realtimeShutdown()

	routerOptions := gatewayhttp.RouterOptions{
		Logger:            logger,
		InternalAPIConfig: cfg.Internal,
		UsageRecorder:     usageRecorder,
	}
	if realtimeHandler != nil {
		routerOptions.RealtimeHandler = realtimeHandler
		routerOptions.RealtimePath = cfg.WebSocket.Path
	}

	server := &http.Server{
		Addr:              cfg.HTTP.Address(),
		Handler:           gatewayhttp.NewRouter(routerOptions),
		ReadHeaderTimeout: 5 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("voice gateway started", "address", server.Addr)
		if serveErr := server.ListenAndServe(); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			errCh <- serveErr
		}
	}()

	stopCh := make(chan os.Signal, 1)
	signal.Notify(stopCh, os.Interrupt, syscall.SIGTERM)

	select {
	case err := <-errCh:
		return fmt.Errorf("serve: %w", err)
	case sig := <-stopCh:
		logger.Info("shutdown requested", "signal", sig.String())
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return server.Shutdown(shutdownCtx)
}

// buildRealtimeTransport creates the authenticated device WebSocket endpoint
// and the speaker-reference bus shared by its sessions.
//
// The endpoint is disabled unless explicitly enabled and given a strong token
// secret, so a misconfigured deployment cannot expose unauthenticated child
// audio. A disabled endpoint returns a nil handler and a no-op shutdown.
func buildRealtimeTransport(
	cfg config.Config,
	logger *slog.Logger,
) (http.Handler, func(), error) {
	if !cfg.WebSocket.Enabled {
		logger.Info("device realtime endpoint is disabled")
		return nil, func() {}, nil
	}

	verifier, err := websocket.NewHMACDeviceTokenVerifier(cfg.WebSocket.AuthTokenSecret, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("create device token verifier: %w", err)
	}

	adapters, err := pipeline.Build(cfg.Audio)
	if err != nil {
		return nil, nil, fmt.Errorf("build audio adapters: %w", err)
	}

	references := reference.NewBus()

	runner, err := conversation.New(conversation.Config{
		ASR:      adapters.ASR,
		LLM:      adapters.LLM,
		TTS:      adapters.TTS,
		Model:    cfg.Audio.LLMModel,
		Voice:    cfg.Audio.TTSConfig.Voice,
		Language: "zh",
		ReferencePublisher: func(deviceID string, pcm []int16) {
			references.Publish(deviceID, pcm)
		},
	})
	if err != nil {
		return nil, nil, fmt.Errorf("build conversation pipeline: %w", err)
	}

	manager := session.NewManager(session.ManagerConfig{
		MaxSessions:          cfg.WebSocket.MaxTotalSessions,
		MaxSessionsPerDevice: cfg.WebSocket.MaxSessionsPerDevice,
	})

	preprocessOptions := preprocess.Options{
		EchoCancellation: cfg.Audio.EchoCancellationEnabled,
		NoiseSuppression: cfg.Audio.NoiseSuppressionEnabled,
		GainCalibration:  cfg.Audio.GainCalibrationEnabled,
		TargetRmsDbfs:    cfg.Audio.TargetRmsDbfs,
	}
	detectorConfig := vad.Config{
		SpeechThresholdRatio: cfg.Audio.VadSpeechThreshold,
		HangoverFrames:       cfg.Audio.VadHangoverFrames,
		ActivationFrames:     cfg.Audio.VadMinSpeechFrames,
	}

	realtimeServer, err := websocket.NewServer(websocket.ServerConfig{
		ReadLimitBytes:     int64(cfg.WebSocket.MaxFrameBytes),
		WriteQueueDepth:    cfg.WebSocket.WriteQueueDepth,
		SegmentQueueDepth:  cfg.WebSocket.SegmentQueueDepth,
		PongWait:           seconds(cfg.WebSocket.PongWaitSeconds),
		PingInterval:       seconds(cfg.WebSocket.PingIntervalSeconds),
		WriteTimeout:       seconds(cfg.WebSocket.WriteTimeoutSeconds),
		MaxFramesPerSecond: cfg.WebSocket.MaxFramesPerSecond,
		MaxBytesPerSecond:  cfg.WebSocket.MaxBytesPerSecond,
		SessionTTL:         seconds(cfg.WebSocket.SessionTokenTTLSeconds),
		Manager:            manager,
		Verifier:           verifier,
		DetectorFactory: func() vad.Detector {
			return vad.NewEnergyDetector(detectorConfig)
		},
		PreprocessFactory: func() (preprocess.Processor, error) {
			return preprocess.NewProcessor(preprocessOptions, frame.SamplesPerFrame)
		},
		ReferenceAudio: func(deviceID string) []int16 {
			return references.Latest(deviceID)
		},
		OnSessionClosed: func(sessionID string, cause error) {
			runner.ClearSession(sessionID)
		},
		SegmentHandler: func(segment session.AudioSegment, sink websocket.AudioSink) {
			// The conversation handler runs per completed utterance; a provider
			// failure ends that turn only and is logged without child content.
			if turnErr := runner.Turn(context.Background(), segment.SessionID, segment.DeviceID, segment.PCM, sink); turnErr != nil {
				logger.Warn("voice conversation turn failed",
					"session_id", segment.SessionID,
					"device_id", segment.DeviceID,
					"error", turnErr.Error(),
				)
			}
		},
	}, logger)
	if err != nil {
		return nil, nil, fmt.Errorf("create realtime transport: %w", err)
	}

	shutdown := func() {
		manager.CloseAll()
	}
	logger.Info("device realtime endpoint enabled", "path", cfg.WebSocket.Path)
	return realtimeServer.Handler(), shutdown, nil
}

func seconds(value int) time.Duration {
	return time.Duration(value) * time.Second
}
