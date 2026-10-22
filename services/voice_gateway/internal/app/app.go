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

	sharedobservability "github.com/TissyBoxC/sprout-platform/packages/go/observability"
	visionsub2api "github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/adapter/vision/sub2api"
	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/audio/frame"
	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/audio/preprocess"
	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/audio/reference"
	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/audio/vad"
	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/buildinfo"
	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/careview"
	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/config"
	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/conversation"
	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/intercom"
	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/pipeline"
	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/platform/cache"
	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/security/audit"
	securityauth "github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/security/authorization"
	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/security/content_policy"
	securitysettings "github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/security/settings"
	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/session"
	gatewayhttp "github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/transport/http"
	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/transport/websocket"
	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/usage"
	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/vision"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

// Run starts the voice gateway and waits for a shutdown signal.
func Run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	logger := slog.New(sharedobservability.NewRedactingHandler(slog.NewJSONHandler(
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
	var policyResolver content_policy.Resolver
	var deviceAuthorizer websocket.DeviceAuthorizer
	var safetyReader securitysettings.Reader
	if cfg.Database.DSN != "" {
		databaseStore, err := pgxpool.New(startupCtx, cfg.Database.DSN)
		if err != nil {
			return fmt.Errorf("open usage database: %w", err)
		}
		defer databaseStore.Close()
		usageRecorder = usage.NewPostgresRecorder(databaseStore)
		policyResolver = content_policy.NewPostgresResolver(databaseStore)
		deviceAuthorizer = securityauth.NewPostgresDeviceAuthorizer(databaseStore)
		safetyReader = securitysettings.NewPostgresReader(databaseStore)
	}

	// The registry is the only rendezvous between the device conversation
	// transport and every guardian relay; it holds live device frame sinks and
	// never audio or image bytes.
	deviceConnections := careview.NewDeviceConnectionRegistry()

	realtimeHandler, realtimeManager, realtimeShutdown, err := buildRealtimeTransport(
		cfg,
		logger,
		policyResolver,
		deviceAuthorizer,
		safetyReader,
		redisCache.Client(),
		deviceConnections,
	)
	if err != nil {
		return err
	}
	defer realtimeShutdown()

	intercomRelays := intercom.NewRelayManager(cfg.Intercom.MaxRelays)
	defer intercomRelays.CloseAll()

	careViewRelays := careview.NewRelayManager(cfg.CareView.MaxRelays)
	defer careViewRelays.CloseAll()

	routerOptions := gatewayhttp.RouterOptions{
		Logger:            logger,
		InternalAPIConfig: cfg.Internal,
		UsageRecorder:     usageRecorder,
		SessionMetrics:    realtimeManager,
		RealtimeEnabled:   realtimeHandler != nil,
		IntercomEnabled:   cfg.Intercom.Enabled,
		IntercomRelays:    intercomRelays,
		CareViewEnabled:   cfg.CareView.Enabled,
		CareViewRelays:    careViewRelays,
	}
	if cfg.Intercom.Enabled {
		resolver, err := intercom.NewPlatformSessionResolver(intercom.PlatformResolverConfig{
			BaseURL:      cfg.AI.DevicePlatformBaseURL,
			ServiceToken: cfg.AI.DevicePlatformServiceToken,
		})
		if err != nil {
			return fmt.Errorf("build intercom session resolver: %w", err)
		}
		routerOptions.IntercomResolver = resolver
		routerOptions.IntercomConnector = intercom.NewRealtimeDeviceConnector(
			intercom.RealtimeConnectorConfig{
				// The intercom relay keeps its own source interface; the shared
				// registry is adapted once so both relays write through the same
				// device writer.
				Source:        intercomDeviceSource{connections: deviceConnections},
				MaxFrameBytes: cfg.Intercom.MaxFrameBytes,
			},
		)
		routerOptions.IntercomTransport = gatewayhttp.IntercomTransportConfig{
			Path:                 cfg.Intercom.Path,
			MaxCallDuration:      cfg.Intercom.MaxCallDuration(),
			MaxFrameBytes:        int64(cfg.Intercom.MaxFrameBytes),
			MaxFramesPerSecond:   cfg.Intercom.MaxFramesPerSecond,
			MaxBytesPerSecond:    cfg.Intercom.MaxBytesPerSecond,
			WriteQueueDepth:      cfg.Intercom.WriteQueueDepth,
			WriteTimeout:         seconds(cfg.Intercom.WriteTimeoutSeconds),
			PongWait:             seconds(cfg.Intercom.PongWaitSeconds),
			PingInterval:         seconds(cfg.Intercom.PingIntervalSeconds),
			RelayWriteQueueDepth: cfg.Intercom.WriteQueueDepth,
			AllowedOrigins:       cfg.WebSocket.AllowedOrigins,
		}
		logger.Info("guardian intercom relay enabled", "path", cfg.Intercom.Path)
	}
	if cfg.CareView.Enabled {
		resolver, err := careview.NewPlatformSessionResolver(careview.PlatformResolverConfig{
			BaseURL:      cfg.AI.DevicePlatformBaseURL,
			ServiceToken: cfg.AI.DevicePlatformServiceToken,
		})
		if err != nil {
			return fmt.Errorf("build care view session resolver: %w", err)
		}
		routerOptions.CareViewResolver = resolver
		routerOptions.CareViewConnector = careview.NewRealtimeDeviceConnector(
			careview.RealtimeConnectorConfig{
				Source:        deviceConnections,
				MaxFrameBytes: cfg.CareView.MaxFrameBytes,
			},
		)
		routerOptions.CareViewTransport = gatewayhttp.CareViewTransportConfig{
			Path:                 cfg.CareView.Path,
			MaxViewDuration:      cfg.CareView.MaxViewDuration(),
			MaxFrameBytes:        int64(cfg.CareView.MaxFrameBytes),
			MaxFramesPerSecond:   cfg.CareView.MaxFramesPerSecond,
			WriteQueueDepth:      cfg.CareView.WriteQueueDepth,
			WriteTimeout:         seconds(cfg.CareView.WriteTimeoutSeconds),
			PongWait:             seconds(cfg.CareView.PongWaitSeconds),
			PingInterval:         seconds(cfg.CareView.PingIntervalSeconds),
			StateRecheckInterval: seconds(cfg.CareView.StateRecheckSeconds),
			AllowedOrigins:       cfg.WebSocket.AllowedOrigins,
		}
		logger.Info("guardian care view relay enabled", "path", cfg.CareView.Path)
	}
	if cfg.Vision.Enabled {
		visionService, err := vision.New(vision.Options{
			Client: visionsub2api.New(visionsub2api.Config{
				BaseURL:      cfg.AI.DevicePlatformBaseURL,
				ServiceToken: cfg.AI.DevicePlatformServiceToken,
				Model:        cfg.Vision.Model,
			}),
			// Vision reuses the device authorizer so a caller-supplied device
			// identifier is checked against the guardian's live consent.
			Authorizer: deviceAuthorizer,
			AuditSink:  audit.NewSlogSink(logger),
		})
		if err != nil {
			return fmt.Errorf("build vision pipeline: %w", err)
		}
		routerOptions.VisionService = visionService
		logger.Info("camera vision pipeline enabled")
	}
	if realtimeHandler != nil {
		routerOptions.RealtimeHandler = realtimeHandler
		routerOptions.RealtimePath = cfg.WebSocket.Path
	}
	routerOptions.BuildVersion = buildinfo.Current()

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
	policyResolver content_policy.Resolver,
	deviceAuthorizer websocket.DeviceAuthorizer,
	safetyReader securitysettings.Reader,
	redisClient *redis.Client,
	deviceConnections *careview.DeviceConnectionRegistry,
) (http.Handler, *session.Manager, func(), error) {
	if !cfg.WebSocket.Enabled {
		logger.Info("device realtime endpoint is disabled")
		return nil, nil, func() {}, nil
	}

	// Consent withdrawal must be detectable while a connection is open, which
	// requires the database-backed authorizer. Configuration already refuses an
	// enabled endpoint without a DSN; this second guard keeps the composition
	// root from silently exposing the audio endpoint if that wiring changes.
	if deviceAuthorizer == nil {
		return nil, nil, nil, errors.New("realtime endpoint requires a device authorizer")
	}

	verifier, err := websocket.NewHMACDeviceTokenVerifierWithStore(
		cfg.WebSocket.AuthTokenSecret,
		nil,
		websocket.NewRedisTokenClaimStore(redisClient, "sprout:voice:device-token"),
	)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("create device token verifier: %w", err)
	}

	audioConfig := cfg.Audio
	audioConfig.DevicePlatformBaseURL = cfg.AI.DevicePlatformBaseURL
	audioConfig.DevicePlatformServiceToken = cfg.AI.DevicePlatformServiceToken
	adapters, err := pipeline.Build(audioConfig)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("build audio adapters: %w", err)
	}

	references := reference.NewBus()

	var devicePolicy content_policy.DevicePolicy
	if cfg.Security.ContentPolicyEnabled {
		if policyResolver == nil {
			return nil, nil, nil, errors.New("content policy is enabled but no policy resolver is configured")
		}
		devicePolicy = content_policy.NewDeviceEvaluator(policyResolver, 0)
	}

	runner, err := conversation.New(conversation.Config{
		ASR:                 adapters.ASR,
		LLM:                 adapters.LLM,
		TTS:                 adapters.TTS,
		Model:               cfg.Audio.LLMModel,
		Voice:               cfg.Audio.TTSConfig.Voice,
		Language:            "zh",
		DevicePolicy:        devicePolicy,
		MaxInputRunes:       cfg.Security.MaxInputRunes,
		InputFilterEnabled:  cfg.Security.InputFilterEnabled,
		OutputFilterEnabled: cfg.Security.OutputFilterEnabled,
		SafetyPolicy:        safetyReader,
		SafetyAudit:         audit.NewSlogSink(logger),
		ReferencePublisher: func(deviceID string, pcm []int16) {
			references.Publish(deviceID, pcm)
		},
	})
	if err != nil {
		return nil, nil, nil, fmt.Errorf("build conversation pipeline: %w", err)
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
		ReadLimitBytes:      int64(cfg.WebSocket.MaxFrameBytes),
		WriteQueueDepth:     cfg.WebSocket.WriteQueueDepth,
		SegmentQueueDepth:   cfg.WebSocket.SegmentQueueDepth,
		PongWait:            seconds(cfg.WebSocket.PongWaitSeconds),
		PingInterval:        seconds(cfg.WebSocket.PingIntervalSeconds),
		AuthRecheckInterval: seconds(cfg.WebSocket.AuthRecheckSeconds),
		WriteTimeout:        seconds(cfg.WebSocket.WriteTimeoutSeconds),
		MaxFramesPerSecond:  cfg.WebSocket.MaxFramesPerSecond,
		MaxBytesPerSecond:   cfg.WebSocket.MaxBytesPerSecond,
		AllowedOrigins:      cfg.WebSocket.AllowedOrigins,
		SessionTTL:          seconds(cfg.WebSocket.SessionTokenTTLSeconds),
		ContinuityWindow:    seconds(cfg.WebSocket.ContinuityWindowSeconds),
		Manager:             manager,
		Verifier:            verifier,
		Authorizer:          deviceAuthorizer,
		TokenRevoker:        verifier.RevokeIdentity,
		OnDeviceConnected: func(deviceID string, connection websocket.DeviceFrameSink) func() {
			return deviceConnections.Register(deviceID, connection)
		},
		DetectorFactory: func() vad.Detector {
			return vad.NewEnergyDetector(detectorConfig)
		},
		PreprocessFactory: func() (preprocess.Processor, error) {
			return preprocess.NewProcessor(preprocessOptions, frame.SamplesPerFrame)
		},
		ReferenceAudio: func(deviceID string) []int16 {
			return references.Latest(deviceID)
		},
		OnDeviceDisconnected: func(deviceID string) {
			// Drop the retained speaker reference so raw playback PCM cannot
			// outlive the connection it was captured for.
			references.Clear(deviceID)
		},
		OnSessionClosed: func(sessionID string, cause error) {
			runner.ClearSession(sessionID)
		},
		SegmentHandler: func(ctx context.Context, segment session.AudioSegment, sink websocket.AudioSink) {
			// The conversation handler runs per completed utterance; a provider
			// failure ends that turn only and is logged without child content.
			if turnErr := runner.TurnWithCategory(
				ctx,
				segment.SessionID,
				segment.DeviceID,
				segment.ContentCategory,
				segment.PCM,
				sink,
			); turnErr != nil {
				logger.Warn("voice conversation turn failed",
					"session_id", segment.SessionID,
					"device_id", segment.DeviceID,
					"error", sharedobservability.RedactValue(turnErr.Error()),
				)
			}
		},
	}, logger)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("create realtime transport: %w", err)
	}

	shutdown := func() {
		manager.CloseAll()
	}
	logger.Info("device realtime endpoint enabled", "path", cfg.WebSocket.Path)
	return realtimeServer.Handler(), manager, shutdown, nil
}

func seconds(value int) time.Duration {
	return time.Duration(value) * time.Second
}

// intercomDeviceSource adapts the shared device registry to the intercom
// relay's device source interface.
//
// The registry is declared once and both guardian relays write through it. The
// two relay packages each declare their own DeviceConnection interface, and Go
// method signatures are invariant, so the bridge lives here in the composition
// root instead of forcing either package to export a duplicate type.
type intercomDeviceSource struct {
	connections *careview.DeviceConnectionRegistry
}

// DeviceConnection returns the live device connection of one device.
func (source intercomDeviceSource) DeviceConnection(
	deviceID string,
) (intercom.DeviceConnection, error) {
	if source.connections == nil {
		return nil, intercom.ErrDeviceNotConnected
	}
	connection, err := source.connections.DeviceConnection(deviceID)
	if err != nil {
		return nil, err
	}
	return connection, nil
}
