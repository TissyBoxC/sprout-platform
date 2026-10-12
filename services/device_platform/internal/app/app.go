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
	alarmReminderRepository "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/alarm_reminder/repository"
	alarmReminderService "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/alarm_reminder/service"
	auditRepository "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/audit/repository"
	auditService "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/audit/service"
	authRepository "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/auth/repository"
	authService "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/auth/service"
	bluetoothAudioRepository "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/bluetooth_audio/repository"
	bluetoothAudioService "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/bluetooth_audio/service"
	childRepository "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/child/repository"
	childService "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/child/service"
	companionshipRepository "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/companionship/repository"
	companionshipService "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/companionship/service"
	connectivityRepository "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/connectivity/repository"
	connectivityServicePkg "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/connectivity/service"
	contentRepository "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/content/repository"
	contentService "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/content/service"
	contentOfflinePolicyRepository "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/content_offline_policy/repository"
	contentOfflinePolicyService "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/content_offline_policy/service"
	bindingRepository "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/device_binding/repository"
	bindingService "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/device_binding/service"
	deviceMeshRepository "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/device_mesh/repository"
	deviceMeshService "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/device_mesh/service"
	runtimeRepository "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/device_runtime/repository"
	runtimeService "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/device_runtime/service"
	displayRepository "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/display/repository"
	displayService "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/display/service"
	englishRepository "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/english/repository"
	englishService "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/english/service"
	featureCenterDomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/feature_center/domain"
	featureCenterRepository "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/feature_center/repository"
	featureCenterService "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/feature_center/service"
	formFactorRepository "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/form_factor/repository"
	formFactorService "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/form_factor/service"
	imagePrivacyRepository "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/image_privacy/repository"
	imagePrivacyService "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/image_privacy/service"
	inputRepository "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/input/repository"
	inputService "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/input/service"
	learningVisualizationRepository "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/learning_visualization/repository"
	learningVisualizationService "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/learning_visualization/service"
	localCommandRepository "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/local_command/repository"
	localCommandService "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/local_command/service"
	motionRepository "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/motion/repository"
	motionRuntimeService "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/motion/service"
	notificationRepository "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/notification/repository"
	notificationService "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/notification/service"
	operationsRepository "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/operations/repository"
	operationsService "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/operations/service"
	otaDomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/ota/domain"
	otaRepository "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/ota/repository"
	otaService "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/ota/service"
	policyRepository "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/parent_policy/repository"
	policyService "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/parent_policy/service"
	playbackRepository "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/playback/repository"
	playbackService "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/playback/service"
	powerRepository "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/power/repository"
	powerRuntimeService "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/power/service"
	privacyRepository "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/privacy/repository"
	privacyService "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/privacy/service"
	recommendationService "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/recommendation/service"
	releaseStoreService "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/release_store/service"
	screenRepository "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/screen/repository"
	screenService "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/screen/service"
	serviceVersionRepository "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/service_version/repository"
	serviceVersionService "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/service_version/service"
	ui_textRepository "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/ui_text/repository"
	ui_textService "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/ui_text/service"
	usageReportRepository "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/usage_report/repository"
	usageReportService "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/usage_report/service"
	videoCompanionRepository "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/video_companion/repository"
	videoCompanionService "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/video_companion/service"
	videoSessionRepository "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/video_session/repository"
	videoSessionService "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/video_session/service"
	visionRepository "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/vision/repository"
	visionService "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/vision/service"
	wearableRepository "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/wearable/repository"
	wearableService "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/wearable/service"
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
	companionshipPersistence := companionshipRepository.NewPostgresRepository(databaseStore.Pool())
	englishPersistence := englishRepository.NewPostgresRepository(databaseStore.Pool())
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
	companionshipCompanionService, err := companionshipService.New(companionshipService.Options{
		Repository: companionshipPersistence,
	})
	if err != nil {
		return fmt.Errorf("create companionship service: %w", err)
	}
	alarmReminder, err := alarmReminderService.New(alarmReminderService.Options{
		Repository: alarmReminderRepository.NewPostgresRepository(databaseStore.Pool()),
	})
	if err != nil {
		return fmt.Errorf("create alarm reminder service: %w", err)
	}
	englishLearningService, err := englishService.New(englishService.Options{
		Repository: englishPersistence,
	})
	if err != nil {
		return fmt.Errorf("create english learning service: %w", err)
	}
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
	diagnosticService, err := auditService.New(auditService.Options{
		Repository: auditRepository.NewPostgresRepository(databaseStore.Pool()),
	})
	if err != nil {
		return fmt.Errorf("create diagnostic service: %w", err)
	}
	deviceRuntimeService, err := runtimeService.New(runtimeService.Options{
		Repository:       runtimeRepository.NewPostgresRepository(databaseStore.Pool()),
		BindingService:   deviceBindingService,
		DiagnosticReader: diagnosticService,
		OfflineThreshold: cfg.DeviceRuntime.OfflineThreshold,
		CommandTTL:       cfg.DeviceRuntime.CommandTTL,
	})
	if err != nil {
		return fmt.Errorf("create device runtime service: %w", err)
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
	parentPolicyService.SetVoiceDefaultsReader(operationsVoiceDefaults{
		operations: operations,
	})
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
		"ui_text": nonzeroRowProbe(
			databaseStore.Pool(),
			"SELECT 1 FROM ui_text_package_versions LIMIT 1",
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
	featureCenter.SetMetricsReader(featureCenterMetricsReader{
		repository: featureCenterRepository.NewMetricsRepository(
			databaseStore.Pool(),
			cfg.DeviceRuntime.OfflineThreshold,
		),
		serviceVersions: serviceVersions,
		releaseStore:    releaseStore,
	})
	contentLibraryService, err := contentService.New(contentService.Options{
		Repository:  contentRepository.NewPostgresRepository(databaseStore.Pool()),
		AssetReader: contentAssetReader{store: releaseStore},
	})
	if err != nil {
		return fmt.Errorf("create content library service: %w", err)
	}
	playbackRequests, err := playbackService.New(playbackService.Options{
		Repository: playbackRepository.NewPostgresRepository(databaseStore.Pool()),
		DeviceReader: playbackService.BindingDeviceReader{
			Service: deviceBindingService,
		},
		PackageReader: playbackService.ContentPackageReader{
			Repository: contentRepository.NewPostgresRepository(databaseStore.Pool()),
		},
	})
	if err != nil {
		return fmt.Errorf("create playback request service: %w", err)
	}
	contentRecommendations := recommendationService.New(
		recommendationService.Options{
			Catalog: recommendationService.ContentCatalogReader{
				Repository: contentRepository.NewPostgresRepository(databaseStore.Pool()),
			},
			Signals: recommendationService.ContentSignalReader{
				Repository: contentRepository.NewPostgresRepository(databaseStore.Pool()),
				Devices: recommendationService.BindingDeviceReader{
					Service: deviceBindingService,
				},
			},
		},
	)
	localCommands := localCommandService.New(localCommandService.Options{
		Repository: localCommandRepository.NewPostgresRepository(databaseStore.Pool()),
	})
	contentOfflinePolicy := contentOfflinePolicyService.New(
		contentOfflinePolicyService.Options{
			Repository: contentOfflinePolicyRepository.NewPostgresRepository(
				databaseStore.Pool(),
			),
		},
	)
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
		Commander:      deviceRuntimeService,
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
	uiTextVerifier, err := ui_textService.NewEd25519KeyringVerifier(
		cfg.UIText.SignaturePublicKeys,
	)
	if err != nil {
		return fmt.Errorf("create ui text signature verifier: %w", err)
	}
	uiText, err := ui_textService.New(ui_textService.Options{
		Repository: ui_textRepository.NewPostgresRepository(databaseStore.Pool()),
		Verifier:   uiTextVerifier,
		Audit:      privacy,
	})
	if err != nil {
		return fmt.Errorf("create ui text service: %w", err)
	}
	vision, err := visionService.New(visionService.Options{
		Repository: visionRepository.NewPostgresRepository(databaseStore.Pool()),
		ConsentReader: visionConsentReader{
			bindings: bindingPersistence,
			privacy:  privacy,
		},
		DeviceReader: visionConsentReader{
			bindings: bindingPersistence,
			privacy:  privacy,
		},
	})
	if err != nil {
		return fmt.Errorf("create vision service: %w", err)
	}
	display, err := displayService.New(displayService.Options{
		Repository: displayRepository.NewPostgresRepository(databaseStore.Pool()),
	})
	if err != nil {
		return fmt.Errorf("create display service: %w", err)
	}
	input, err := inputService.New(inputService.Options{
		Repository: inputRepository.NewPostgresRepository(databaseStore.Pool()),
	})
	if err != nil {
		return fmt.Errorf("create input service: %w", err)
	}
	connectivity, err := connectivityServicePkg.New(connectivityServicePkg.Options{
		Repository: connectivityRepository.NewPostgresRepository(databaseStore.Pool()),
	})
	if err != nil {
		return fmt.Errorf("create connectivity service: %w", err)
	}
	devicePower, err := powerRuntimeService.New(powerRuntimeService.Options{
		Repository: powerRepository.NewPostgresRepository(databaseStore.Pool()),
	})
	if err != nil {
		return fmt.Errorf("create power service: %w", err)
	}
	deviceMotion, err := motionRuntimeService.New(motionRuntimeService.Options{
		Repository: motionRepository.NewPostgresRepository(databaseStore.Pool()),
	})
	if err != nil {
		return fmt.Errorf("create motion service: %w", err)
	}
	deviceFormFactor, err := formFactorService.New(formFactorService.Options{
		Repository: formFactorRepository.NewPostgresRepository(databaseStore.Pool()),
	})
	if err != nil {
		return fmt.Errorf("create form factor service: %w", err)
	}
	videoCompanion, err := videoCompanionService.New(videoCompanionService.Options{
		Repository: videoCompanionRepository.NewPostgresRepository(databaseStore.Pool()),
	})
	if err != nil {
		return fmt.Errorf("create video companion service: %w", err)
	}
	wearable, err := wearableService.New(wearableService.Options{
		Repository: wearableRepository.NewPostgresRepository(databaseStore.Pool()),
	})
	if err != nil {
		return fmt.Errorf("create wearable service: %w", err)
	}
	deviceMesh, err := deviceMeshService.New(deviceMeshService.Options{
		Repository: deviceMeshRepository.NewPostgresRepository(databaseStore.Pool()),
	})
	if err != nil {
		return fmt.Errorf("create device mesh service: %w", err)
	}
	screen, err := screenService.New(screenService.Options{
		Repository: screenRepository.NewPostgresRepository(databaseStore.Pool()),
	})
	if err != nil {
		return fmt.Errorf("create screen service: %w", err)
	}
	videoSession, err := videoSessionService.New(videoSessionService.Options{
		Repository: videoSessionRepository.NewPostgresRepository(databaseStore.Pool()),
	})
	if err != nil {
		return fmt.Errorf("create video session service: %w", err)
	}
	bluetoothAudio, err := bluetoothAudioService.New(bluetoothAudioService.Options{
		Repository: bluetoothAudioRepository.NewPostgresRepository(databaseStore.Pool()),
	})
	if err != nil {
		return fmt.Errorf("create bluetooth audio service: %w", err)
	}
	learningVisualization, err := learningVisualizationService.New(
		learningVisualizationService.Options{
			Repository: learningVisualizationRepository.NewPostgresRepository(databaseStore.Pool()),
		},
	)
	if err != nil {
		return fmt.Errorf("create learning visualization service: %w", err)
	}
	imagePrivacy, err := imagePrivacyService.New(imagePrivacyService.Options{
		Repository: imagePrivacyRepository.NewPostgresRepository(databaseStore.Pool()),
	})
	if err != nil {
		return fmt.Errorf("create image privacy service: %w", err)
	}

	server := &http.Server{
		Addr: cfg.HTTP.Address(),
		Handler: platformhttp.NewRouter(platformhttp.RouterOptions{
			Logger:                       logger,
			InternalAPIConfig:            cfg.Internal,
			AuthService:                  parentAuthService,
			ChildService:                 childProfileService,
			CompanionshipService:         companionshipCompanionService,
			AlarmReminderService:         alarmReminder,
			ParentPolicyService:          parentPolicyService,
			AIService:                    aiAccountService,
			OperationsService:            operations,
			BindingService:               deviceBindingService,
			RuntimeService:               deviceRuntimeService,
			DiagnosticService:            diagnosticService,
			ServiceVersionService:        serviceVersions,
			FeatureCenterService:         featureCenter,
			ReleaseStoreService:          releaseStore,
			OTAService:                   ota,
			ContentService:               contentLibraryService,
			EnglishService:               englishLearningService,
			PlaybackService:              playbackRequests,
			RecommendationService:        contentRecommendations,
			LocalCommandService:          localCommands,
			ContentOfflinePolicyService:  contentOfflinePolicy,
			UITextService:                uiText,
			UsageReportService:           usageReports,
			VoiceTokenIssuer:             voiceTokenIssuer,
			VoiceWebSocketURL:            cfg.VoiceGateway.WebSocketURL,
			VoiceConsentReader:           privacy,
			PrivacyService:               privacy,
			NotificationService:          notifications,
			VisionService:                vision,
			DisplayService:               display,
			InputService:                 input,
			ConnectivityService:          connectivity,
			PowerService:                 devicePower,
			MotionService:                deviceMotion,
			FormFactorService:            deviceFormFactor,
			VideoCompanionService:        videoCompanion,
			VideoSessionService:          videoSession,
			BluetoothAudioService:        bluetoothAudio,
			LearningVisualizationService: learningVisualization,
			ImagePrivacyService:          imagePrivacy,
			WearableService:              wearable,
			DeviceMeshService:            deviceMesh,
			ScreenService:                screen,
			AuditRecorder:                privacy,
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
	go runRetentionWorker(workerCtx, logger, privacy, vision)

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

// runRetentionWorker enforces the disclosed short-retention windows. Migration
// 0034 promises that expired vision results are removed by "the same retention
// job that prunes sensitive caches"; this is that job. It also reclaims expired
// authentication artefacts so a long-running deployment does not accumulate
// dead sessions, challenge hashes, verification codes, or export receipts.
func runRetentionWorker(
	ctx context.Context,
	logger *slog.Logger,
	privacy *privacyService.Service,
	vision *visionService.Service,
) {
	const interval = time.Hour
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if privacy != nil {
			purged, err := privacy.PurgeExpiredRecords(ctx)
			if err != nil {
				logger.Error("privacy retention worker failed", "error", err)
			} else if total := purged.Total(); total > 0 {
				logger.Info(
					"privacy retention worker purged expired records",
					"total", total,
				)
			}
			usagePurged, err := privacy.PurgeUsageReports(ctx)
			if err != nil {
				logger.Error("usage report retention worker failed", "error", err)
			} else if total := usagePurged.Total(); total > 0 {
				logger.Info(
					"usage report retention worker purged old days",
					"total", total,
				)
			}
		}
		if vision != nil {
			expiredTasks, deletedResults, err := vision.PruneExpired(ctx)
			if err != nil {
				logger.Error("vision retention worker failed", "error", err)
			} else if expiredTasks > 0 || deletedResults > 0 {
				logger.Info(
					"vision retention worker pruned data",
					"expired_tasks", expiredTasks,
					"deleted_results", deletedResults,
				)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
