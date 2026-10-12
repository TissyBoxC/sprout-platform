package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/feature_center/domain"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/feature_center/repository"
	operationsdomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/operations/domain"
)

type memoryRepository struct {
	configs map[string]*domain.FeatureConfig
}

func newMemoryRepository() *memoryRepository {
	return &memoryRepository{configs: map[string]*domain.FeatureConfig{}}
}

func (r *memoryRepository) Get(
	_ context.Context,
	featureID string,
) (*domain.FeatureConfig, error) {
	config := r.configs[featureID]
	if config == nil {
		return nil, domain.ErrFeatureConfigNotFound
	}
	copyConfig := *config
	copyConfig.Values = mergeValues(config.Values)
	return &copyConfig, nil
}

func (r *memoryRepository) List(
	_ context.Context,
) ([]domain.FeatureConfig, error) {
	result := make([]domain.FeatureConfig, 0, len(r.configs))
	for _, config := range r.configs {
		copyConfig := *config
		copyConfig.Values = mergeValues(config.Values)
		result = append(result, copyConfig)
	}
	return result, nil
}

func (r *memoryRepository) Save(
	_ context.Context,
	featureID string,
	values map[string]any,
	actorID string,
	expectedVersion int64,
) (*domain.FeatureConfig, error) {
	current := r.configs[featureID]
	currentVersion := int64(0)
	if current != nil {
		currentVersion = current.Version
	}
	if expectedVersion != currentVersion {
		return nil, &domain.VersionConflictError{
			ExpectedVersion: expectedVersion,
			CurrentVersion:  currentVersion,
		}
	}
	config := &domain.FeatureConfig{
		FeatureID: featureID,
		Values:    mergeValues(values),
		Version:   currentVersion + 1,
		UpdatedBy: actorID,
		UpdatedAt: time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC),
	}
	r.configs[featureID] = config
	copyConfig := *config
	copyConfig.Values = mergeValues(config.Values)
	return &copyConfig, nil
}

type fixedClock struct{}

func (fixedClock) Now() time.Time {
	return time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
}

type memoryRuntimeSettings struct {
	settings *operationsdomain.Settings
	version  int64
}

func (reader *memoryRuntimeSettings) Settings(
	context.Context,
) (*operationsdomain.Settings, int64, error) {
	return reader.settings, reader.version, nil
}

func (reader *memoryRuntimeSettings) UpdateSettings(
	_ context.Context,
	settings *operationsdomain.Settings,
	_ string,
	expectedVersion int64,
) (*operationsdomain.Settings, int64, error) {
	if expectedVersion != reader.version {
		return nil, 0, &operationsdomain.SettingsVersionConflictError{
			ExpectedVersion: expectedVersion,
			CurrentVersion:  reader.version,
		}
	}
	reader.settings = settings
	reader.version++
	return settings, reader.version, nil
}

func newTestService(t *testing.T) (*Service, *memoryRepository) {
	t.Helper()
	repository := newMemoryRepository()
	service, err := New(Options{
		Repository: repository,
		Definitions: []Definition{
			featureDef(
				"voice_gateway",
				"语音网关",
				"测试功能",
				"voice",
				"语音平台",
				true,
				configFields(
					boolField("enabled", "启用", false, true),
					integerField("max_sessions", "最大会话", float64(1), true, true, "路"),
					secretField("session_secret", "会话密钥", true, true),
					selectField("mode", "模式", "safe", []domain.ConfigOption{
						{Value: "safe", Label: "安全"},
						{Value: "fast", Label: "快速"},
					}, true),
					durationField("timeout", "超时", "30s", true, true),
					urlField("websocket_url", "地址", "wss://voice.example.com", true, true),
				),
				metrics("active_session_count", "活跃会话", "路"),
			),
			featureDef(
				"deployment_infrastructure",
				"部署基础设施",
				"只读功能",
				"platform",
				"平台运维",
				false,
				nil,
				nil,
			),
		},
		HealthProbes: map[string]HealthProbe{
			"voice_gateway": HealthProbeFunc(func(
				_ context.Context,
				_ string,
			) (domain.HealthStatus, error) {
				return domain.HealthHealthy, nil
			}),
		},
		Clock: fixedClock{},
	})
	if err != nil {
		t.Fatalf("create feature center service: %v", err)
	}
	return service, repository
}

func TestUpdateConfigValidatesAndPersistsValues(t *testing.T) {
	service, _ := newTestService(t)
	feature, err := service.UpdateConfig(
		context.Background(),
		"voice_gateway",
		map[string]any{
			"enabled":        true,
			"max_sessions":   float64(4),
			"session_secret": "super-secret-value",
			"mode":           "fast",
			"timeout":        "1h",
			"websocket_url":  "wss://voice.example.net/socket",
		},
		"admin-001",
		0,
	)
	if err != nil {
		t.Fatalf("update config: %v", err)
	}
	if feature.ConfigVersion != 1 {
		t.Fatalf("version = %d, want 1", feature.ConfigVersion)
	}
	secret, ok := feature.Values["session_secret"].(map[string]any)
	if !ok || secret["configured"] != true {
		t.Fatalf("secret was not masked: %#v", feature.Values["session_secret"])
	}
	if secret["mask"] == "super-secret-value" {
		t.Fatal("secret plaintext was returned")
	}
}

func TestUpdateConfigRejectsUnknownField(t *testing.T) {
	service, _ := newTestService(t)
	_, err := service.UpdateConfig(
		context.Background(),
		"voice_gateway",
		map[string]any{"unknown_field": true},
		"admin-001",
		0,
	)
	var validation *domain.ConfigValidationError
	if !errors.As(err, &validation) ||
		!errors.Is(validation.Err, domain.ErrUnknownConfigField) {
		t.Fatalf("expected unknown field error, got %v", err)
	}
}

func TestUpdateConfigRejectsInvalidValuesBySchema(t *testing.T) {
	service, _ := newTestService(t)
	_, err := service.UpdateConfig(
		context.Background(),
		"voice_gateway",
		map[string]any{
			"max_sessions": float64(1.5),
			"mode":         "unsafe",
		},
		"admin-001",
		0,
	)
	var validation *domain.ConfigValidationError
	if !errors.As(err, &validation) {
		t.Fatalf("expected validation error, got %v", err)
	}
}

func TestUpdateConfigRejectsReadOnlyFeature(t *testing.T) {
	service, _ := newTestService(t)
	_, err := service.UpdateConfig(
		context.Background(),
		"deployment_infrastructure",
		map[string]any{},
		"admin-001",
		0,
	)
	if !errors.Is(err, domain.ErrFeatureReadOnly) {
		t.Fatalf("expected read-only error, got %v", err)
	}
}

func TestUpdateConfigRejectsImmutableSystemField(t *testing.T) {
	definition := featureDef(
		"platform_security",
		"平台安全",
		"测试不可修改字段",
		"security",
		"安全团队",
		true,
		configFields(
			boolField("minor_mode_default", "默认启用未成年人模式", true, true),
			systemSelectField("runtime_environment", "运行环境", "production", []domain.ConfigOption{
				{Value: "production", Label: "生产环境"},
			}),
		),
		nil,
	)
	service, err := New(Options{
		Repository:  newMemoryRepository(),
		Definitions: []Definition{definition},
		Clock:       fixedClock{},
	})
	if err != nil {
		t.Fatalf("create feature center service: %v", err)
	}
	_, err = service.UpdateConfig(
		context.Background(),
		"platform_security",
		map[string]any{"runtime_environment": "development"},
		"admin-001",
		0,
	)
	if !errors.Is(err, domain.ErrImmutableConfigField) {
		t.Fatalf("expected immutable-field rejection, got %v", err)
	}
}

func TestDefaultRegistryCoversRequiredFeatures(t *testing.T) {
	required := []string{
		"auth",
		"parent_account",
		"ai_account",
		"child_profile",
		"parent_policy",
		"device_binding",
		"device_runtime",
		"device_diagnostics",
		"content_library",
		"ota_release",
		"app_update",
		"download_server",
		"service_version",
		"ui_text",
		"voice_gateway",
		"voice_conversation",
		"ai_model_gateway",
		"usage_report",
		"notification",
		"platform_security",
		"deployment_infrastructure",
	}
	definitions := DefaultRegistry()
	if len(definitions) != len(required) {
		t.Fatalf("definition count = %d, want %d", len(definitions), len(required))
	}
	registered := make(map[string]Definition, len(definitions))
	for _, definition := range definitions {
		registered[definition.Feature.ID] = definition
		if definition.Feature.Category == "" ||
			definition.Feature.Owner == "" ||
			definition.Feature.Status == "" ||
			definition.Feature.HealthSource == "" ||
			definition.Feature.ConfigSchema == nil ||
			definition.Feature.ReadOnlyMetrics == nil {
			t.Fatalf("incomplete feature definition: %+v", definition.Feature)
		}
	}
	for _, featureID := range required {
		if _, ok := registered[featureID]; !ok {
			t.Fatalf("missing required feature %q", featureID)
		}
	}
}

func TestDefaultRegistryActiveFeaturesHaveHealthAndReadOnlyReason(t *testing.T) {
	required := []string{
		"auth",
		"parent_account",
		"ai_account",
		"child_profile",
		"parent_policy",
		"device_binding",
		"device_runtime",
		"device_diagnostics",
		"content_library",
		"ota_release",
		"app_update",
		"download_server",
		"service_version",
		"voice_gateway",
		"ai_model_gateway",
		"usage_report",
		"platform_security",
		"deployment_infrastructure",
	}
	registered := make(map[string]Definition)
	for _, definition := range DefaultRegistry() {
		registered[definition.Feature.ID] = definition
	}
	for _, featureID := range required {
		definition, ok := registered[featureID]
		if !ok {
			t.Fatalf("missing required active feature %q", featureID)
		}
		if definition.Feature.Status != domain.FeatureStatusActive {
			t.Fatalf("%s status = %q, want active", featureID, definition.Feature.Status)
		}
		if len(definition.Feature.ConfigSchema) == 0 &&
			definition.Feature.ReadOnlyReason == "" {
			t.Fatalf(
				"%s has no editable config and must explain why it is read-only",
				featureID,
			)
		}
	}
}

func TestValidateHealthProbeCoverageRejectsActiveFeatureWithoutProbe(t *testing.T) {
	definitions := []Definition{
		featureDef(
			"voice_gateway",
			"语音网关",
			"已上线功能",
			"voice",
			"语音平台",
			false,
			nil,
			nil,
		),
		plannedFeatureDef(
			"notification",
			"消息通知",
			"规划功能",
			"platform",
			"平台运维",
		),
	}
	err := ValidateHealthProbeCoverage(definitions, map[string]HealthProbe{})
	if err == nil || !strings.Contains(err.Error(), "voice_gateway") {
		t.Fatalf("expected missing probe error for voice_gateway, got %v", err)
	}
	if strings.Contains(err.Error(), "notification") {
		t.Fatalf("planned feature must not be treated as missing: %v", err)
	}
}

func TestUpdateConfigRejectsStaleVersion(t *testing.T) {
	service, _ := newTestService(t)
	if _, err := service.UpdateConfig(
		context.Background(),
		"voice_gateway",
		map[string]any{
			"enabled":        true,
			"session_secret": "first-secret",
		},
		"admin-001",
		0,
	); err != nil {
		t.Fatalf("first update: %v", err)
	}
	_, err := service.UpdateConfig(
		context.Background(),
		"voice_gateway",
		map[string]any{"enabled": false},
		"admin-002",
		0,
	)
	if !errors.Is(err, domain.ErrConfigVersionConflict) {
		t.Fatalf("expected version conflict, got %v", err)
	}
}

func TestVoiceConversationConfigBridgesToRuntimeSettings(t *testing.T) {
	service, err := New(Options{
		Repository:  newMemoryRepository(),
		Definitions: DefaultRegistry(),
		Clock:       fixedClock{},
	})
	if err != nil {
		t.Fatalf("create service: %v", err)
	}
	settings := &operationsdomain.Settings{
		AI: operationsdomain.AISettings{DefaultConcurrency: 1},
		Voice: operationsdomain.VoiceSettings{
			ContinuousConversationEnabled: true,
			IdleWindowSeconds:             8,
			BargeInEnabled:                true,
			FarFieldEnabled:               true,
		},
	}
	settings.SetVoiceConfigured()
	runtimeSettings := &memoryRuntimeSettings{settings: settings}
	service.SetRuntimeSettingsReader(runtimeSettings)

	feature, err := service.UpdateConfig(
		context.Background(),
		"voice_conversation",
		map[string]any{
			"continuous_conversation_enabled": false,
			"idle_window_seconds":             float64(25),
			"barge_in_enabled":                false,
			"far_field_enabled":               true,
		},
		"admin-001",
		0,
	)
	if err != nil {
		t.Fatalf("update voice conversation: %v", err)
	}
	if runtimeSettings.settings.Voice != (operationsdomain.VoiceSettings{
		ContinuousConversationEnabled: false,
		IdleWindowSeconds:             25,
		BargeInEnabled:                false,
		FarFieldEnabled:               true,
	}) {
		t.Fatalf("runtime voice settings = %+v", runtimeSettings.settings.Voice)
	}
	if feature.Values["idle_window_seconds"] != 25 {
		t.Fatalf("projected idle window = %#v, want 25", feature.Values["idle_window_seconds"])
	}
}

func TestUsageReportConfigBridgesToRuntimeSettings(t *testing.T) {
	service, err := New(Options{
		Repository:  newMemoryRepository(),
		Definitions: DefaultRegistry(),
		Clock:       fixedClock{},
	})
	if err != nil {
		t.Fatalf("create service: %v", err)
	}
	runtimeSettings := &memoryRuntimeSettings{settings: &operationsdomain.Settings{
		Retention: operationsdomain.RetentionSettings{
			ConversationDays: 30,
			UsageReportDays:  365,
		},
	}}
	runtimeSettings.settings.Retention.SetUsageReportConfigured()
	service.SetRuntimeSettingsReader(runtimeSettings)

	feature, err := service.UpdateConfig(
		context.Background(),
		"usage_report",
		map[string]any{"retention_days": float64(120)},
		"admin-001",
		0,
	)
	if err != nil {
		t.Fatalf("update usage report: %v", err)
	}
	if runtimeSettings.settings.Retention.UsageReportDays != 120 {
		t.Fatalf(
			"runtime usage report days = %d, want 120",
			runtimeSettings.settings.Retention.UsageReportDays,
		)
	}
	if feature.Values["retention_days"] != 120 {
		t.Fatalf(
			"projected usage report days = %#v, want 120",
			feature.Values["retention_days"],
		)
	}
}

func TestUsageReportConfigRejectsOutOfRangeRetention(t *testing.T) {
	service, err := New(Options{
		Repository:  newMemoryRepository(),
		Definitions: DefaultRegistry(),
		Clock:       fixedClock{},
	})
	if err != nil {
		t.Fatalf("create service: %v", err)
	}
	settings := &operationsdomain.Settings{
		Retention: operationsdomain.RetentionSettings{UsageReportDays: 365},
	}
	settings.Retention.SetUsageReportConfigured()
	service.SetRuntimeSettingsReader(&memoryRuntimeSettings{settings: settings})

	for _, days := range []float64{0, 3651} {
		_, err := service.UpdateConfig(
			context.Background(),
			"usage_report",
			map[string]any{"retention_days": days},
			"admin-001",
			0,
		)
		var validation *domain.ConfigValidationError
		if !errors.As(err, &validation) {
			t.Fatalf("days=%v: expected validation error, got %v", days, err)
		}
	}
}

func TestVoiceConversationConfigRejectsIdleOutsideRuntimeBounds(t *testing.T) {
	service, err := New(Options{
		Repository:  newMemoryRepository(),
		Definitions: DefaultRegistry(),
		Clock:       fixedClock{},
	})
	if err != nil {
		t.Fatalf("create service: %v", err)
	}
	runtimeSettings := &memoryRuntimeSettings{
		settings: &operationsdomain.Settings{
			Voice: operationsdomain.DefaultVoiceSettings(),
		},
	}
	runtimeSettings.settings.SetVoiceConfigured()
	service.SetRuntimeSettingsReader(runtimeSettings)

	for _, idle := range []float64{1, 61} {
		_, err := service.UpdateConfig(
			context.Background(),
			"voice_conversation",
			map[string]any{"idle_window_seconds": idle},
			"admin-001",
			0,
		)
		var validation *domain.ConfigValidationError
		if !errors.As(err, &validation) {
			t.Fatalf("idle=%v: expected validation error, got %v", idle, err)
		}
	}
}

func TestCheckHealthUsesInjectedProbe(t *testing.T) {
	service, _ := newTestService(t)
	feature, err := service.CheckHealth(context.Background(), "voice_gateway")
	if err != nil {
		t.Fatalf("check health: %v", err)
	}
	if feature.Health != domain.HealthHealthy {
		t.Fatalf("health = %q, want healthy", feature.Health)
	}
}

func TestCheckHealthRejectsFeatureWithoutProbe(t *testing.T) {
	service, _ := newTestService(t)
	_, err := service.CheckHealth(context.Background(), "deployment_infrastructure")
	if !errors.Is(err, domain.ErrHealthCheckUnavailable) {
		t.Fatalf("expected unavailable health check, got %v", err)
	}
}

func TestCheckHealthDoesNotMutateSharedRegistry(t *testing.T) {
	service, _ := newTestService(t)
	if _, err := service.CheckHealth(context.Background(), "voice_gateway"); err != nil {
		t.Fatalf("check health: %v", err)
	}
	// A subsequent list must still report the registry default, not a probe
	// result leaked from the previous caller.
	features, err := service.List(context.Background())
	if err != nil {
		t.Fatalf("list features: %v", err)
	}
	for _, feature := range features {
		if feature.ID != "voice_gateway" {
			continue
		}
		if feature.Health != domain.HealthUnknown {
			t.Fatalf("registry health leaked: %q", feature.Health)
		}
		if feature.HealthDetail != nil {
			t.Fatal("registry health detail leaked across callers")
		}
	}
}

func TestDefaultRegistryMarksUITextAsActive(t *testing.T) {
	service, err := New(Options{
		Repository:  newMemoryRepository(),
		Definitions: DefaultRegistry(),
		Clock:       fixedClock{},
	})
	if err != nil {
		t.Fatalf("create default feature center service: %v", err)
	}
	features, err := service.List(context.Background())
	if err != nil {
		t.Fatalf("list features: %v", err)
	}
	byID := make(map[string]domain.Feature, len(features))
	for _, feature := range features {
		byID[feature.ID] = feature
	}
	// Remote UI text is now implemented, so it must be an active capability
	// with a read-only surface and polling metrics rather than a planned stub.
	uiText, ok := byID["ui_text"]
	if !ok {
		t.Fatalf("missing feature %q", "ui_text")
	}
	if uiText.Status != domain.FeatureStatusActive {
		t.Fatalf("ui_text status = %q, want active", uiText.Status)
	}
	if len(uiText.ReadOnlyMetrics) == 0 {
		t.Fatalf("ui_text should expose read-only publication metrics")
	}

	// The notification module is now implemented, so it must be a registered
	// active capability with a read-only configuration surface.
	notification, ok := byID["notification"]
	if !ok {
		t.Fatalf("missing feature %q", "notification")
	}
	if notification.Status != domain.FeatureStatusActive {
		t.Fatalf("notification status = %q, want active", notification.Status)
	}
	if notification.ReadOnlyReason == "" {
		t.Fatalf("notification should explain why external channel config is read-only")
	}
	if len(notification.ReadOnlyMetrics) == 0 {
		t.Fatalf("notification should expose read-only delivery metrics")
	}
}

func TestNotificationRegistryKeepsExternalChannelsReadOnly(t *testing.T) {
	service, err := New(Options{
		Repository:  newMemoryRepository(),
		Definitions: DefaultRegistry(),
		Clock:       fixedClock{},
	})
	if err != nil {
		t.Fatalf("create default feature center service: %v", err)
	}
	feature, err := service.Get(context.Background(), "notification")
	if err != nil {
		t.Fatalf("get notification feature: %v", err)
	}
	if len(feature.ConfigSchema) != 0 {
		t.Fatalf("notification config schema should stay empty until senders exist")
	}
	if feature.Values["push_configured"] != nil {
		t.Fatalf("notification must not fabricate push configuration state")
	}
}

func TestConfigFieldClonesAllowCustom(t *testing.T) {
	service, err := New(Options{
		Repository:  newMemoryRepository(),
		Definitions: DefaultRegistry(),
		Clock:       fixedClock{},
	})
	if err != nil {
		t.Fatalf("create service: %v", err)
	}
	feature, err := service.Get(context.Background(), "ai_account")
	if err != nil {
		t.Fatalf("get ai_account: %v", err)
	}
	var found bool
	for _, field := range feature.ConfigSchema {
		if field.Key != "default_models" {
			continue
		}
		found = true
		if !field.AllowCustom {
			t.Fatal("default_models should allow custom values")
		}
	}
	if !found {
		t.Fatal("default_models field is missing")
	}
}

type staticModelCatalog struct {
	options []ModelOption
}

func (catalog staticModelCatalog) ModelOptions(
	_ context.Context,
) ([]ModelOption, error) {
	return catalog.options, nil
}

type staticMetricsReader struct {
	values map[string]map[string]any
}

func (reader staticMetricsReader) FeatureMetrics(
	_ context.Context,
	featureID string,
) (map[string]any, error) {
	return reader.values[featureID], nil
}

func TestProjectMergesLiveMetrics(t *testing.T) {
	service, err := New(Options{
		Repository:  newMemoryRepository(),
		Definitions: DefaultRegistry(),
		Clock:       fixedClock{},
	})
	if err != nil {
		t.Fatalf("create service: %v", err)
	}
	service.SetMetricsReader(staticMetricsReader{values: map[string]map[string]any{
		"parent_account": {"active_parent_count": int64(7)},
		"usage_report":   {"reported_usage_days": int64(3)},
	}})

	parentAccount, err := service.Get(context.Background(), "parent_account")
	if err != nil {
		t.Fatalf("get parent_account: %v", err)
	}
	if len(parentAccount.ReadOnlyMetrics) != 1 {
		t.Fatalf(
			"parent_account metric count = %d, want 1",
			len(parentAccount.ReadOnlyMetrics),
		)
	}
	if value := parentAccount.ReadOnlyMetrics[0].Value; value != int64(7) {
		t.Fatalf("active_parent_count value = %#v, want 7", value)
	}

	usageReport, err := service.Get(context.Background(), "usage_report")
	if err != nil {
		t.Fatalf("get usage_report: %v", err)
	}
	if len(usageReport.ReadOnlyMetrics) != 1 {
		t.Fatalf(
			"usage_report metric count = %d, want 1",
			len(usageReport.ReadOnlyMetrics),
		)
	}
	if value := usageReport.ReadOnlyMetrics[0].Value; value != int64(3) {
		t.Fatalf("reported_usage_days value = %#v, want 3", value)
	}
}

func TestProjectLeavesUnknownMetricsEmpty(t *testing.T) {
	service, err := New(Options{
		Repository:  newMemoryRepository(),
		Definitions: DefaultRegistry(),
		Clock:       fixedClock{},
	})
	if err != nil {
		t.Fatalf("create service: %v", err)
	}
	// The reader reports a key that the registry does not declare. It must be
	// ignored, and a declared key with no live value must stay unset rather
	// than being rendered as a real zero.
	service.SetMetricsReader(staticMetricsReader{values: map[string]map[string]any{
		"parent_account": {"active_parent_count": int64(7), "unknown_metric": int64(1)},
	}})

	features, err := service.List(context.Background())
	if err != nil {
		t.Fatalf("list features: %v", err)
	}
	byID := make(map[string]domain.Feature, len(features))
	for _, feature := range features {
		byID[feature.ID] = feature
	}
	aiAccount := byID["ai_account"]
	if len(aiAccount.ReadOnlyMetrics) != 1 {
		t.Fatalf("ai_account metric count = %d, want 1", len(aiAccount.ReadOnlyMetrics))
	}
	if aiAccount.ReadOnlyMetrics[0].Value != nil {
		t.Fatalf(
			"unreported ai_account metric = %#v, want nil",
			aiAccount.ReadOnlyMetrics[0].Value,
		)
	}
	if len(byID["parent_account"].ReadOnlyMetrics) != 1 {
		t.Fatalf("parent_account metric count changed")
	}
}

func TestLiveMetricsDoNotMutateSharedRegistry(t *testing.T) {
	service, err := New(Options{
		Repository:  newMemoryRepository(),
		Definitions: DefaultRegistry(),
		Clock:       fixedClock{},
	})
	if err != nil {
		t.Fatalf("create service: %v", err)
	}
	service.SetMetricsReader(staticMetricsReader{values: map[string]map[string]any{
		"parent_account": {"active_parent_count": int64(9)},
	}})
	if _, err := service.Get(context.Background(), "parent_account"); err != nil {
		t.Fatalf("get parent_account: %v", err)
	}
	// A second call must not observe a registry mutated by the first.
	service.SetMetricsReader(nil)
	feature, err := service.Get(context.Background(), "parent_account")
	if err != nil {
		t.Fatalf("get parent_account: %v", err)
	}
	if feature.ReadOnlyMetrics[0].Value != nil {
		t.Fatalf(
			"registry metric leaked across calls: %#v",
			feature.ReadOnlyMetrics[0].Value,
		)
	}
}

func TestAICAccountMergesLiveModelOptions(t *testing.T) {
	repository := newMemoryRepository()
	service, err := New(Options{
		Repository:  repository,
		Definitions: DefaultRegistry(),
		Clock:       fixedClock{},
	})
	if err != nil {
		t.Fatalf("create service: %v", err)
	}
	service.SetModelCatalogReader(staticModelCatalog{
		options: []ModelOption{
			{ID: "gpt-x", Label: "gpt-x（约 120 ms）"},
			{ID: "gpt-y", Label: "gpt-y"},
		},
	})
	feature, err := service.Get(context.Background(), "ai_account")
	if err != nil {
		t.Fatalf("get ai_account: %v", err)
	}
	options := map[string]bool{}
	for _, field := range feature.ConfigSchema {
		if field.Key != "default_models" {
			continue
		}
		for _, option := range field.Options {
			options[option.Value] = true
		}
	}
	if !options["gpt-x"] || !options["gpt-y"] {
		t.Fatalf("live model options missing: %#v", options)
	}
}

func TestMultiselectAllowCustomAcceptsArbitraryValues(t *testing.T) {
	service, _ := newTestService(t)
	definition := featureDef(
		"ai_account",
		"AI 账号",
		"测试自定义多选",
		"ai",
		"AI 服务",
		true,
		configFields(customMultiselectField("default_models", "默认模型", nil)),
		nil,
	)
	custom, err := New(Options{
		Repository:  service.repository,
		Definitions: []Definition{definition},
		Clock:       fixedClock{},
	})
	if err != nil {
		t.Fatalf("create service: %v", err)
	}
	feature, err := custom.UpdateConfig(
		context.Background(),
		"ai_account",
		map[string]any{"default_models": []any{"some-arbitrary-model-id"}},
		"admin-001",
		0,
	)
	if err != nil {
		t.Fatalf("update config: %v", err)
	}
	values, ok := feature.Values["default_models"].([]any)
	if !ok || len(values) != 1 || values[0] != "some-arbitrary-model-id" {
		t.Fatalf("custom model value not persisted: %#v", feature.Values["default_models"])
	}
}

var _ repository.Repository = (*memoryRepository)(nil)
