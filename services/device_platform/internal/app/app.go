// Package app wires and runs the device platform service.
package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/TissyBoxC/sprout-platform/packages/go/observability"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/config"
	aiProvider "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/ai_gateway/provider"
	aiRepository "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/ai_gateway/repository"
	aiService "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/ai_gateway/service"
	auditRepository "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/audit/repository"
	auditService "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/audit/service"
	authRepository "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/auth/repository"
	authService "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/auth/service"
	childRepository "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/child/repository"
	childService "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/child/service"
	contentRepository "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/content/repository"
	contentService "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/content/service"
	bindingRepository "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/device_binding/repository"
	bindingService "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/device_binding/service"
	runtimeRepository "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/device_runtime/repository"
	runtimeService "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/device_runtime/service"
	featureCenterDomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/feature_center/domain"
	featureCenterRepository "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/feature_center/repository"
	featureCenterService "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/feature_center/service"
	operationsRepository "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/operations/repository"
	operationsService "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/operations/service"
	notificationRepository "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/notification/repository"
	notificationService "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/notification/service"
	otaDomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/ota/domain"
	otaRepository "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/ota/repository"
	otaService "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/ota/service"
	policyRepository "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/parent_policy/repository"
	policyService "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/parent_policy/service"
	privacyRepository "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/privacy/repository"
	privacyService "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/privacy/service"
	releaseStoreService "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/release_store/service"
	serviceVersionRepository "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/service_version/repository"
	serviceVersionService "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/service_version/service"
	usageReportRepository "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/usage_report/repository"
	usageReportService "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/usage_report/service"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/platform/cache"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/platform/database"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/platform/security"
	platformhttp "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/transport/http"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/transport/mqtt"
)

// Run starts the HTTP server and waits for a shutdown signal.
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

	databaseStore, err := database.Open(startupCtx, cfg.Database.DSN)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer databaseStore.Close(context.Background())

	redisCache, err := cache.Open(startupCtx, cfg.Redis.Address, cfg.Redis.Password, cfg.Redis.DB)
	if err != nil {
		return fmt.Errorf("open redis: %w", err)
	}
	defer redisCache.Close()

	mqttClient, err := mqtt.Open(startupCtx, cfg.MQTT)
	if err != nil {
		return fmt.Errorf("open MQTT: %w", err)
	}
	defer mqttClient.Close()

	authPersistence := authRepository.NewPostgresRepository(databaseStore.Pool())
	childPersistence := childRepository.NewPostgresRepository(databaseStore.Pool())
	bindingPersistence := bindingRepository.NewPostgresRepository(databaseStore.Pool())
	usagePersistence := usageReportRepository.NewPostgresRepository(databaseStore.Pool())

	tokenIssuer, err := security.NewHMACTokenIssuer(cfg.Auth.AccessTokenSecret)
	if err != nil {
		return fmt.Errorf("create token issuer: %w", err)
	}
	credentialCipher, err := aiService.NewAESGCMCipher(cfg.Auth.CredentialKey)
	if err != nil {
		return fmt.Errorf("create AI credential cipher: %w", err)
	}
	mfaCipher, err := authService.NewAESGCMTOTPCipher(cfg.Auth.MFACredentialKey)
	if err != nil {
		return fmt.Errorf("create MFA credential cipher: %w", err)
	}
	aiClient, err := aiProvider.New(cfg.AI.BaseURL, cfg.AI.ServiceToken, nil)
	if err != nil {
		return fmt.Errorf("create AI provider client: %w", err)
	}
	aiAccountService, err := aiService.New(aiService.Options{
		Repository:         aiRepository.NewPostgresRepository(databaseStore.Pool()),
		Provider:           aiClient,
		Cipher:             credentialCipher,
		DefaultBalanceUSD:  cfg.AI.DefaultBalanceUSD,
		DefaultModels:      cfg.AI.DefaultModels,
		DefaultConcurrency: cfg.AI.DefaultConcurrency,
	})
	if err != nil {
		return fmt.Errorf("create AI account service: %w", err)
	}
	var phoneVerifier authService.PhoneVerifier
	if cfg.Auth.PhoneVerificationMode == "local" {
		phoneVerifier = authService.NewLocalPhoneVerifier(
			authPersistence,
			nil,
		)
	}
	parentAuthService, err := authService.New(authService.Options{
		Repository:      authPersistence,
		TokenIssuer:     tokenIssuer,
		AIProvisioner:   aiAccountService,
		MFACipher:       mfaCipher,
		PhoneVerifier:   phoneVerifier,
		MFAChallengeTTL: cfg.Auth.MFAChallengeTTL,
		AccessTTL:       cfg.Auth.AccessTokenTTL,
		RefreshTTL:      cfg.Auth.RefreshTokenTTL,
	})
	if err != nil {
		return fmt.Errorf("create authentication service: %w", err)
	}
	parentPolicyService, err := policyService.New(policyService.Options{
		Repository: policyRepository.NewPostgresRepository(databaseStore.Pool()),
	})
	if err != nil {
		return fmt.Errorf("create parent policy service: %w", err)
	}
	childProfileService, err := childService.New(childService.Options{
		Repository:        childPersistence,
		PolicyProvisioner: parentPolicyService,
	})
	if err != nil {
		return fmt.Errorf("create child profile service: %w", err)
	}
	parentAuthService.SetChildProvisioner(childProfileService)
	var voiceTokenIssuer bindingService.VoiceTokenIssuer
	if strings.TrimSpace(cfg.VoiceGateway.WSTokenSecret) != "" {
		signer, signerErr := bindingService.NewVoiceTokenSigner(
			cfg.VoiceGateway.WSTokenSecret,
			cfg.VoiceGateway.WSTokenTTL,
		)
		if signerErr != nil {
			return fmt.Errorf("create voice token signer: %w", signerErr)
		}
		voiceTokenIssuer = signer
	}
	deviceBindingService, err := bindingService.New(bindingService.Options{
		Repository:    bindingPersistence,
		TokenTTL:      15 * time.Minute,
		ProofVerifier: security.ECDSAProofVerifier{},
	})
	if err != nil {
		return fmt.Errorf("create device binding service: %w", err)
	}
	deviceRuntimeService, err := runtimeService.New(runtimeService.Options{
		Repository:       runtimeRepository.NewPostgresRepository(databaseStore.Pool()),
		BindingService:   deviceBindingService,
		OfflineThreshold: cfg.DeviceRuntime.OfflineThreshold,
		CommandTTL:       cfg.DeviceRuntime.CommandTTL,
	})
	if err != nil {
		return fmt.Errorf("create device runtime service: %w", err)
	}
	diagnosticService, err := auditService.New(auditService.Options{
		Repository: auditRepository.NewPostgresRepository(databaseStore.Pool()),
	})
	if err != nil {
		return fmt.Errorf("create diagnostic service: %w", err)
	}
	operations, err := operationsService.New(operationsService.Options{
		Repository: operationsRepository.NewPostgresRepository(
			databaseStore.Pool(),
		),
		OnlineThreshold: cfg.DeviceRuntime.OfflineThreshold,
		OTA: operationsService.OTAConfig{
			ManifestBaseURL: cfg.OTA.ManifestBaseURL,
			ResourceBaseURL: cfg.OTA.ResourceBaseURL,
			ClientBaseURL:   cfg.OTA.ClientBaseURL,
		},
	})
	if err != nil {
		return fmt.Errorf("create operations service: %w", err)
	}
	aiAccountService.SetPolicyReader(operations)
	parentAuthService.SetPolicyReader(operations)
	parentAuthService.SetOverviewReader(operations)

	serviceVersions, err := serviceVersionService.New(serviceVersionService.Options{
		Repository: serviceVersionRepository.NewFileRepository(
			cfg.ServiceVersion.StateDir,
		),
	})
	if err != nil {
		return fmt.Errorf("create service version service: %w", err)
	}
	releaseStore, err := releaseStoreService.New(releaseStoreService.Options{
		RootDir:       cfg.ReleaseStore.RootDir,
		PublicBaseURL: cfg.ReleaseStore.PublicBaseURL,
	})
	if err != nil {
		return fmt.Errorf("create release store service: %w", err)
	}
	defer releaseStore.Close()
	operations.SetArtifactStore(releaseArtifactStoreAdapter{store: releaseStore})
	firmwareSignatureVerifier, err := otaService.NewEd25519KeyringVerifier(
		cfg.OTA.FirmwareSignaturePublicKeys,
	)
	if err != nil {
		return fmt.Errorf("create firmware signature verifier: %w", err)
	}
	ota, err := otaService.New(otaService.Options{
		Repository: otaRepository.NewPostgresRepository(databaseStore.Pool()),
		Verifier:   firmwareSignatureVerifier,
	})
	if err != nil {
		return fmt.Errorf("create OTA service: %w", err)
	}
	featureCenterHealthProbes := map[string]featureCenterService.HealthProbe{
		"auth": nonzeroRowProbe(
			databaseStore.Pool(),
			"SELECT 1 FROM parent_accounts WHERE status = 'active' LIMIT 1",
		),
		"parent_account": nonzeroRowProbe(
			databaseStore.Pool(),
			"SELECT 1 FROM parent_accounts WHERE role = 'parent' LIMIT 1",
		),
		"child_profile": nonzeroRowProbe(
			databaseStore.Pool(),
			"SELECT 1 FROM child_profiles LIMIT 1",
		),
		"parent_policy": nonzeroRowProbe(
			databaseStore.Pool(),
			"SELECT 1 FROM parent_policies LIMIT 1",
		),
		"device_diagnostics": nonzeroRowProbe(
			databaseStore.Pool(),
			"SELECT 1 FROM device_boot_events LIMIT 1",
		),
		"content_library": nonzeroRowProbe(
			databaseStore.Pool(),
			"SELECT 1 FROM content_package_versions LIMIT 1",
		),
		"usage_report": nonzeroRowProbe(
			databaseStore.Pool(),
			"SELECT 1 FROM device_usage_daily LIMIT 1",
		),
		"platform_security": nonzeroRowProbe(
			databaseStore.Pool(),
			"SELECT 1 FROM admin_totp_credentials LIMIT 1",
		),
		"deployment_infrastructure": featureCenterService.HealthProbeFunc(
			func(ctx context.Context, _ string) (featureCenterDomain.HealthStatus, error) {
				if err := databaseStore.Pool().Ping(ctx); err != nil {
					return featureCenterDomain.HealthUnavailable, nil
				}
				if err := redisCache.Client().Ping(ctx).Err(); err != nil {
					return featureCenterDomain.HealthDegraded, nil
				}
				if !mqttClient.Connected() {
					return featureCenterDomain.HealthDegraded, nil
				}
				return featureCenterDomain.HealthHealthy, nil
			},
		),
		"ai_account": featureCenterService.HealthProbeFunc(
			func(ctx context.Context, _ string) (featureCenterDomain.HealthStatus, error) {
				if _, err := aiAccountService.RuntimeConfigForAdmin(ctx); err != nil {
					return featureCenterDomain.HealthUnavailable, nil
				}
				return featureCenterDomain.HealthHealthy, nil
			},
		),
		"ai_model_gateway": featureCenterService.HealthProbeFunc(
			func(ctx context.Context, _ string) (featureCenterDomain.HealthStatus, error) {
				if _, err := aiAccountService.RuntimeConfigForAdmin(ctx); err != nil {
					return featureCenterDomain.HealthUnavailable, nil
				}
				return featureCenterDomain.HealthHealthy, nil
			},
		),
		"voice_gateway": featureCenterService.HealthProbeFunc(
			func(_ context.Context, _ string) (featureCenterDomain.HealthStatus, error) {
				if strings.TrimSpace(cfg.VoiceGateway.WebSocketURL) == "" ||
					strings.TrimSpace(cfg.VoiceGateway.WSTokenSecret) == "" {
					return featureCenterDomain.HealthUnknown, nil
				}
				if voiceTokenIssuer == nil {
					return featureCenterDomain.HealthDegraded, nil
				}
				return featureCenterDomain.HealthHealthy, nil
			},
		),
		"device_runtime": featureCenterService.HealthProbeFunc(
			func(ctx context.Context, _ string) (featureCenterDomain.HealthStatus, error) {
				if _, err := deviceRuntimeService.ListAllDeviceStatuses(ctx); err != nil {
					return featureCenterDomain.HealthDegraded, nil
				}
				return featureCenterDomain.HealthHealthy, nil
			},
		),
		"device_binding": featureCenterService.HealthProbeFunc(
			func(ctx context.Context, _ string) (featureCenterDomain.HealthStatus, error) {
				if _, err := deviceBindingService.ListAllForAdmin(ctx); err != nil {
					return featureCenterDomain.HealthDegraded, nil
				}
				return featureCenterDomain.HealthHealthy, nil
			},
		),
		"download_server": featureCenterService.HealthProbeFunc(
			func(ctx context.Context, _ string) (featureCenterDomain.HealthStatus, error) {
				status, err := releaseStore.IndexStatus(ctx)
				if err != nil {
					return featureCenterDomain.HealthUnavailable, nil
				}
				if !status.IsAvailable {
					return featureCenterDomain.HealthDegraded, nil
				}
				if status.PendingFileCount > 0 {
					return featureCenterDomain.HealthDegraded, nil
				}
				return featureCenterDomain.HealthHealthy, nil
			},
		),
		"ota_release": featureCenterService.HealthProbeFunc(
			func(ctx context.Context, _ string) (featureCenterDomain.HealthStatus, error) {
				releases, err := ota.ListReleases(ctx, otaDomain.ReleaseFilter{
					Page:     1,
					PageSize: 1,
				})
				if err != nil {
					return featureCenterDomain.HealthUnavailable, nil
				}
				if releases.Total == 0 {
					return featureCenterDomain.HealthDegraded, nil
				}
				return featureCenterDomain.HealthHealthy, nil
			},
		),
		"service_version": featureCenterService.HealthProbeFunc(
			func(ctx context.Context, _ string) (featureCenterDomain.HealthStatus, error) {
				result, err := serviceVersions.SnapshotResult(ctx)
				if err != nil {
					return featureCenterDomain.HealthUnavailable, nil
				}
				if result.StateSource != "worker" {
					return featureCenterDomain.HealthUnknown, nil
				}
				for _, service := range result.Services {
					if service.Status == "unknown" {
						return featureCenterDomain.HealthUnknown, nil
					}
				}
				return featureCenterDomain.HealthHealthy, nil
			},
		),
	}
	if err := featureCenterService.ValidateHealthProbeCoverage(
		featureCenterService.DefaultRegistry(),
		featureCenterHealthProbes,
	); err != nil {
		return fmt.Errorf("validate feature center health coverage: %w", err)
	}
	featureCenter, err := featureCenterService.New(featureCenterService.Options{
		Repository: featureCenterRepository.NewPostgresRepository(
			databaseStore.Pool(),
		),
		HealthProbes: featureCenterHealthProbes,
	})
	if err != nil {
		return fmt.Errorf("create feature center service: %w", err)
	}
	featureCenter.SetRuntimeSettingsReader(operations)
	featureCenter.SetModelCatalogReader(aiModelCatalogReader{service: aiAccountService})
	contentLibraryService, err := contentService.New(contentService.Options{
		Repository:  contentRepository.NewPostgresRepository(databaseStore.Pool()),
		AssetReader: contentAssetReader{store: releaseStore},
	})
	if err != nil {
		return fmt.Errorf("create content library service: %w", err)
	}
	usageReports, err := usageReportService.New(usageReportService.Options{
		Repository:    usagePersistence,
		PolicyService: parentPolicyService,
	})
	if err != nil {
		return fmt.Errorf("create usage report service: %w", err)
	}
	privacy, err := privacyService.New(privacyService.Options{
		Repository:     privacyRepository.NewPostgresRepository(databaseStore.Pool()),
		ParentAccounts: authPersistence,
		Children:       childPersistence,
		Devices:        bindingPersistence,
		Runtime:        deviceRuntimeService,
		Usage:          usagePersistence,
		AIAccounts:     aiAccountService,
		Retention:      operations,
	})
	if err != nil {
		return fmt.Errorf("create privacy service: %w", err)
	}
	notifications, err := notificationService.New(notificationService.Options{
		Repository: notificationRepository.NewPostgresRepository(databaseStore.Pool()),
		Commander:  deviceRuntimeService,
		Audit:      privacy,
	})
	if err != nil {
		return fmt.Errorf("create notification service: %w", err)
	}

	server := &http.Server{
		Addr: cfg.HTTP.Address(),
		Handler: platformhttp.NewRouter(platformhttp.RouterOptions{
			Logger:                logger,
			InternalAPIConfig:     cfg.Internal,
			AuthService:           parentAuthService,
			ChildService:          childProfileService,
			ParentPolicyService:   parentPolicyService,
			AIService:             aiAccountService,
			OperationsService:     operations,
			BindingService:        deviceBindingService,
			RuntimeService:        deviceRuntimeService,
			DiagnosticService:     diagnosticService,
			ServiceVersionService: serviceVersions,
			FeatureCenterService:  featureCenter,
			ReleaseStoreService:   releaseStore,
			OTAService:            ota,
			ContentService:        contentLibraryService,
			UsageReportService:    usageReports,
			VoiceTokenIssuer:      voiceTokenIssuer,
			VoiceWebSocketURL:     cfg.VoiceGateway.WebSocketURL,
			PrivacyService:        privacy,
			NotificationService:   notifications,
			AuditRecorder:         privacy,
		}),
		ReadHeaderTimeout: 5 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("device platform started", "address", server.Addr)
		if serveErr := server.ListenAndServe(); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			errCh <- serveErr
		}
	}()

	workerCtx, workerCancel := context.WithCancel(context.Background())
	defer workerCancel()
	go runDeletionWorker(workerCtx, logger, privacy)

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

func runDeletionWorker(
	ctx context.Context,
	logger *slog.Logger,
	privacy *privacyService.Service,
) {
	if privacy == nil {
		return
	}
	const interval = time.Hour
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if err := privacy.ExecuteDueDeletions(ctx, 20); err != nil {
			logger.Error("privacy deletion worker failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
