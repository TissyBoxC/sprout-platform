package service

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/feature_center/domain"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/feature_center/repository"
	operationsdomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/operations/domain"
)

type memoryRepository struct {
	mu      sync.RWMutex
	configs map[string]*domain.FeatureConfig
	health  map[string]domain.HealthSnapshot
}

type failingHealthRepository struct {
	*memoryRepository
}

func (r *failingHealthRepository) SaveHealth(
	_ context.Context,
	_ *domain.HealthSnapshot,
) error {
	return errors.New("health snapshot storage unavailable")
}

func newMemoryRepository() *memoryRepository {
	return &memoryRepository{
		configs: map[string]*domain.FeatureConfig{},
		health:  map[string]domain.HealthSnapshot{},
	}
}

func (r *memoryRepository) Get(
	_ context.Context,
	featureID string,
) (*domain.FeatureConfig, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
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
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]domain.FeatureConfig, 0, len(r.configs))
	for _, config := range r.configs {
		copyConfig := *config
		copyConfig.Values = mergeValues(config.Values)
		result = append(result, copyConfig)
	}
	return result, nil
}

func (r *memoryRepository) ListHealth(
	_ context.Context,
) ([]domain.HealthSnapshot, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]domain.HealthSnapshot, 0, len(r.health))
	for _, snapshot := range r.health {
		result = append(result, snapshot)
	}
	return result, nil
}

func (r *memoryRepository) GetHealth(
	_ context.Context,
	featureID string,
) (*domain.HealthSnapshot, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	snapshot, ok := r.health[featureID]
	if !ok {
		return nil, domain.ErrFeatureConfigNotFound
	}
	copySnapshot := snapshot
	return &copySnapshot, nil
}

func (r *memoryRepository) SaveHealth(
	_ context.Context,
	snapshot *domain.HealthSnapshot,
) error {
	if snapshot == nil || !snapshot.Status.Valid() {
		return domain.ErrHealthCheckFailed
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if current, ok := r.health[snapshot.FeatureID]; ok &&
		current.CheckedAt.After(snapshot.CheckedAt) {
		return domain.ErrHealthSnapshotStale
	}
	r.health[snapshot.FeatureID] = *snapshot
	return nil
}

func (r *memoryRepository) Save(
	_ context.Context,
	featureID string,
	values map[string]any,
	actorID string,
	expectedVersion int64,
) (*domain.FeatureConfig, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
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

type advancingClock struct {
	mu      sync.Mutex
	current time.Time
	step    time.Duration
}

func (c *advancingClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	result := c.current
	c.current = c.current.Add(c.step)
	return result
}

// memorySchedulerLease models a shared exclusive lease across service
// instances. It intentionally returns the scheduler's public lease error so
// tests exercise the same skip path as PostgreSQL advisory locking.
type memorySchedulerLease struct {
	mu       sync.Mutex
	acquired int
}

func (lease *memorySchedulerLease) WithLeaderLease(
	ctx context.Context,
	work func(context.Context) error,
) error {
	lease.mu.Lock()
	if lease.acquired > 0 {
		lease.mu.Unlock()
		return domain.ErrSchedulerLeaseUnavailable
	}
	lease.acquired++
	lease.mu.Unlock()
	defer func() {
		lease.mu.Lock()
		lease.acquired--
		lease.mu.Unlock()
	}()
	if err := ctx.Err(); err != nil {
		return err
	}
	return work(ctx)
}

type recordingAudit struct {
	mu      sync.Mutex
	entries []recordedAudit
	err     error
}

type recordedAudit struct {
	ActorID   string
	TargetID  string
	Action    string
	Detail    map[string]any
	CreatedAt time.Time
}

func (audit *recordingAudit) RecordAudit(
	_ context.Context,
	actorAccountID string,
	targetAccountID string,
	action string,
	detail map[string]any,
) error {
	audit.mu.Lock()
	if audit.err != nil {
		audit.mu.Unlock()
		return audit.err
	}
	safeDetail := make(map[string]any, len(detail))
	for key, value := range detail {
		safeDetail[key] = value
	}
	audit.entries = append(audit.entries, recordedAudit{
		ActorID:  actorAccountID,
		TargetID: targetAccountID,
		Action:   action,
		Detail:   safeDetail,
	})
	audit.mu.Unlock()
	return nil
}

func (audit *recordingAudit) setError(err error) {
	audit.mu.Lock()
	audit.err = err
	audit.mu.Unlock()
}

func (audit *recordingAudit) snapshot() []recordedAudit {
	audit.mu.Lock()
	defer audit.mu.Unlock()
	return append([]recordedAudit(nil), audit.entries...)
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
		Clock:          fixedClock{},
		SchedulerLease: &memorySchedulerLease{},
	})
	if err != nil {
		t.Fatalf("create feature center service: %v", err)
	}
	return service, repository
}

func registeredDefinition(t *testing.T, featureID string) Definition {
	t.Helper()
	for _, definition := range DefaultRegistry() {
		if definition.Feature.ID == featureID {
			return definition
		}
	}
	t.Fatalf("registered feature %q not found", featureID)
	return Definition{}
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

func TestUpdateConfigRecordsVersionedAudit(t *testing.T) {
	repository := newMemoryRepository()
	audit := &recordingAudit{}
	definition := featureDef(
		"voice_gateway",
		"语音网关",
		"测试功能",
		"voice",
		"语音平台",
		true,
		configFields(
			boolField("enabled", "启用", false, true),
		),
		nil,
	)
	service, err := New(Options{
		Repository:  repository,
		Definitions: []Definition{definition},
		Clock:       fixedClock{},
		Audit:       audit,
	})
	if err != nil {
		t.Fatalf("create service: %v", err)
	}
	if _, err := service.UpdateConfig(
		context.Background(),
		"voice_gateway",
		map[string]any{"enabled": true},
		"admin-001",
		0,
	); err != nil {
		t.Fatalf("first update: %v", err)
	}
	if _, err := service.UpdateConfig(
		context.Background(),
		"voice_gateway",
		map[string]any{"enabled": false},
		"admin-001",
		1,
	); err != nil {
		t.Fatalf("second update: %v", err)
	}
	entries := audit.snapshot()
	if len(entries) != 2 {
		t.Fatalf("audit entry count = %d, want 2", len(entries))
	}
	first := entries[0].Detail
	second := entries[1].Detail
	if first["previous_version"] != int64(0) ||
		first["version"] != int64(1) ||
		second["previous_version"] != int64(1) ||
		second["version"] != int64(2) {
		t.Fatalf("unexpected version audit: %#v %#v", first, second)
	}
	if _, leaked := second["enabled"]; leaked {
		t.Fatalf("audit leaked configuration values: %#v", second)
	}
}

func TestConfigAuditFailureReportsPrimaryWriteCompleted(t *testing.T) {
	audit := &recordingAudit{err: errors.New("audit storage unavailable")}
	definition := featureDef(
		"voice_gateway",
		"语音网关",
		"测试功能",
		"voice",
		"语音平台",
		true,
		configFields(
			boolField("enabled", "启用", false, true),
		),
		nil,
	)
	repository := newMemoryRepository()
	service, err := New(Options{
		Repository:  repository,
		Definitions: []Definition{definition},
		Clock:       fixedClock{},
		Audit:       audit,
	})
	if err != nil {
		t.Fatalf("create service: %v", err)
	}
	feature, err := service.UpdateConfig(
		context.Background(),
		"voice_gateway",
		map[string]any{"enabled": true},
		"admin-001",
		0,
	)
	var auditErr *AuditWriteError
	if !errors.As(err, &auditErr) || !auditErr.PrimaryWriteCompleted {
		t.Fatalf("expected completed-primary audit error, got %v", err)
	}
	if feature == nil ||
		feature.ConfigVersion != 1 ||
		feature.Values["enabled"] != true {
		t.Fatalf("audit failure did not return the saved feature: %+v", feature)
	}
	saved, getErr := repository.Get(context.Background(), "voice_gateway")
	if getErr != nil || saved.Version != 1 {
		t.Fatalf("primary config write was not retained: %+v %v", saved, getErr)
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

func TestUpdateConfigRejectsInvalidAppVersion(t *testing.T) {
	service, err := New(Options{
		Repository:  newMemoryRepository(),
		Definitions: []Definition{registeredDefinition(t, "app_update")},
		Clock:       fixedClock{},
	})
	if err != nil {
		t.Fatalf("create feature center service: %v", err)
	}
	_, err = service.UpdateConfig(
		context.Background(),
		"app_update",
		map[string]any{"minimum_client_version": "latest"},
		"admin-001",
		0,
	)
	var validation *domain.ConfigValidationError
	if !errors.As(err, &validation) ||
		!errors.Is(validation.Err, domain.ErrInvalidConfig) {
		t.Fatalf("expected invalid version error, got %v", err)
	}
}

func TestUpdateConfigAcceptsSemanticAppVersion(t *testing.T) {
	service, err := New(Options{
		Repository:  newMemoryRepository(),
		Definitions: []Definition{registeredDefinition(t, "app_update")},
		Clock:       fixedClock{},
	})
	if err != nil {
		t.Fatalf("create feature center service: %v", err)
	}
	feature, err := service.UpdateConfig(
		context.Background(),
		"app_update",
		map[string]any{"minimum_client_version": "1.2.3"},
		"admin-001",
		0,
	)
	if err != nil {
		t.Fatalf("update app version: %v", err)
	}
	if feature.Values["minimum_client_version"] != "1.2.3" {
		t.Fatalf(
			"minimum_client_version = %#v, want 1.2.3",
			feature.Values["minimum_client_version"],
		)
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

func TestUpdateConfigRejectsMinorModeOverride(t *testing.T) {
	service, err := New(Options{
		Repository:  newMemoryRepository(),
		Definitions: DefaultRegistry(),
		Clock:       fixedClock{},
	})
	if err != nil {
		t.Fatalf("create service: %v", err)
	}
	_, err = service.UpdateConfig(
		context.Background(),
		"platform_security",
		map[string]any{"minor_mode_default": false},
		"admin-001",
		0,
	)
	var validation *domain.ConfigValidationError
	if !errors.As(err, &validation) ||
		!errors.Is(validation.Err, domain.ErrImmutableConfigField) {
		t.Fatalf("expected immutable minor mode error, got %v", err)
	}
}

func TestPlatformSecurityProjectsMinorModeAsEnforcedSystemValue(t *testing.T) {
	runtimeSettings := &memoryRuntimeSettings{settings: &operationsdomain.Settings{
		Safety: operationsdomain.SafetySettings{MinorModeDefault: false},
	}}
	service, err := New(Options{
		Repository:  newMemoryRepository(),
		Definitions: DefaultRegistry(),
		Clock:       fixedClock{},
	})
	if err != nil {
		t.Fatalf("create service: %v", err)
	}
	service.SetRuntimeSettingsReader(runtimeSettings)

	feature, err := service.Get(context.Background(), "platform_security")
	if err != nil {
		t.Fatalf("get platform security: %v", err)
	}
	if feature.Values["minor_mode_default"] != true {
		t.Fatalf(
			"projected minor mode = %#v, want enforced true",
			feature.Values["minor_mode_default"],
		)
	}
	for _, field := range feature.ConfigSchema {
		if field.Key != "minor_mode_default" {
			continue
		}
		if field.Editable {
			t.Fatal("minor mode must be exposed as a read-only system field")
		}
		if field.Source != domain.ConfigSourceSystem {
			t.Fatalf("minor mode source = %q, want system", field.Source)
		}
		return
	}
	t.Fatal("minor mode field is missing from the platform security schema")
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
			systemBoolField("minor_mode_default", "未成年人保护模式", true),
			boolField("output_moderation_enabled", "启用输出审核", true, true),
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
		"family_membership",
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
		"telemetry",
		"notification",
		"platform_security",
		"vision",
		"display",
		"touch",
		"screen",
		"connectivity",
		"power",
		"motion",
		"form_factor",
		"video_companion",
		"video_session",
		"wearable",
		"device_mesh",
		"bluetooth_audio",
		"learning_visualization",
		"image_privacy",
		"english_learning",
		"alarm_reminder",
		"companionship",
		"child_memory",
		"growth_report",
		"device_child_binding",
		"voice_intercom",
		"care_view",
		"assessment",
		"local_command",
		"offline_content",
		"content_engagement",
		"content_theme_pack",
		"playback",
		"recommendation",
		"privacy_lifecycle",
		"deployment_infrastructure",
		"billing_orders",
		"billing_payments",
		"billing_reconciliation",
		"family_spending_limit",
	}
	definitions := DefaultRegistry()
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
	platformSecurity, ok := registered["platform_security"]
	if !ok {
		t.Fatal("platform security definition is missing")
	}
	minorMode := findConfigField(platformSecurity.Feature.ConfigSchema, "minor_mode_default")
	if minorMode == nil || minorMode.Editable ||
		minorMode.Source != domain.ConfigSourceSystem {
		t.Fatalf("minor mode must be a read-only system invariant: %+v", minorMode)
	}
	if len(required) != len(registered) {
		t.Fatalf("required inventory count = %d, registered count = %d",
			len(required), len(registered))
	}
	for _, featureID := range required {
		if _, ok := registered[featureID]; !ok {
			t.Fatalf("missing required feature %q", featureID)
		}
	}
	for featureID := range registered {
		found := false
		for _, requiredFeatureID := range required {
			if featureID == requiredFeatureID {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("unexpected feature %q must be added to the required inventory", featureID)
		}
	}
}

func findConfigField(
	fields []domain.ConfigField,
	key string,
) *domain.ConfigField {
	for index := range fields {
		if fields[index].Key == key {
			return &fields[index]
		}
		if fields[index].Type == domain.ConfigFieldObject {
			if found := findConfigField(fields[index].Fields, key); found != nil {
				return found
			}
		}
	}
	return nil
}

func TestDefaultRegistryActiveFeaturesHaveHealthAndReadOnlyReason(t *testing.T) {
	required := []string{
		"auth",
		"parent_account",
		"family_membership",
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

// TestDefaultRegistryMetricsHaveVerifiableSource is a guardrail against
// registry-only placeholders. A feature metric may only be exposed when it is
// backed by one of the reviewed live sources: MetricsRepository, the app-level
// featureCenterMetricsReader, or a runtime settings projection.
func TestDefaultRegistryMetricsHaveVerifiableSource(t *testing.T) {
	repositoryMetrics := map[string]struct{}{
		"active_auth_session_count":             {},
		"active_parent_count":                   {},
		"family_member_count":                   {},
		"family_pending_invitation_count":       {},
		"active_ai_account_count":               {},
		"child_profile_count":                   {},
		"policy_update_count":                   {},
		"bound_device_count":                    {},
		"online_device_count":                   {},
		"diagnostic_event_count":                {},
		"published_content_count":               {},
		"published_firmware_count":              {},
		"app_release_count":                     {},
		"published_ui_text_count":               {},
		"reported_usage_days":                   {},
		"telemetry_device_count":                {},
		"telemetry_online_count":                {},
		"telemetry_pending_total":               {},
		"notification_total":                    {},
		"notification_pending_delivery":         {},
		"notification_failed_delivery":          {},
		"vision_task_count":                     {},
		"vision_failed_task_count":              {},
		"display_device_count":                  {},
		"display_touch_device_count":            {},
		"touch_device_count":                    {},
		"touch_haptic_device_count":             {},
		"screen_device_count":                   {},
		"screen_point_read_device_count":        {},
		"cellular_device_count":                 {},
		"cellular_roaming_count":                {},
		"power_device_count":                    {},
		"power_low_power_count":                 {},
		"power_deep_sleep_count":                {},
		"motion_device_count":                   {},
		"motion_running_count":                  {},
		"motion_faulted_count":                  {},
		"form_factor_device_count":              {},
		"form_factor_action_count":              {},
		"video_call_count":                      {},
		"video_session_device_count":            {},
		"wearable_device_count":                 {},
		"wearable_sos_event_count":              {},
		"device_group_count":                    {},
		"device_mesh_pending_sync_count":        {},
		"bluetooth_device_count":                {},
		"bluetooth_connected_count":             {},
		"learning_visualization_device_count":   {},
		"image_privacy_device_count":            {},
		"image_privacy_upload_allowed_count":    {},
		"english_course_count":                  {},
		"english_practice_count":                {},
		"alarm_reminder_count":                  {},
		"alarm_reminder_enabled_count":          {},
		"alarm_reminder_event_count":            {},
		"companionship_record_count":            {},
		"habit_reminder_count":                  {},
		"memory_count":                          {},
		"memory_child_count":                    {},
		"memory_retention_days":                 {},
		"local_command_count":                   {},
		"offline_policy_device_count":           {},
		"content_favorite_count":                {},
		"content_play_history_count":            {},
		"content_theme_pack_count":              {},
		"playback_request_count":                {},
		"playback_pending_count":                {},
		"playback_failed_count":                 {},
		"recommendation_candidate_count":        {},
		"consent_event_count":                   {},
		"pending_deletion_count":                {},
		"outdated_service_count":                {},
		"running_service_count":                 {},
		"download_file_count":                   {},
		"notification_configured_channel_count": {},
		"conversation_count_7d":                 {},
		"available_model_count":                 {},
		"mfa_enabled_admin_count":               {},
		"gateway_ready_state":                   {},
		"pending_order_count":                   {},
		"paid_order_count_30d":                  {},
		"expired_order_count":                   {},
		"configured_channel_count":              {},
		"registered_channel_count":              {},
		"reconciliation_run_count":              {},
		"reconciliation_difference_cny_cents":   {},
		"unresolved_discrepancy_count":          {},
		"spending_limit_family_count":           {},
		"spending_limit_rejection_count":        {},
		"growth_report_count":                   {},
		"growth_report_child_count":             {},
		"growth_report_stale_days":              {},
		"device_child_bound_count":              {},
		"device_child_unassigned_count":         {},
		"intercom_open_call_count":              {},
		"intercom_calls_24h":                    {},
		"care_view_open_session_count":          {},
		"care_view_sessions_24h":                {},
		"assessment_active_session_count":       {},
		"assessment_sessions_24h":               {},
	}
	for _, definition := range DefaultRegistry() {
		for _, metric := range definition.Feature.ReadOnlyMetrics {
			if _, ok := repositoryMetrics[metric.Key]; !ok {
				t.Fatalf(
					"feature %s exposes metric %s without a verified live source",
					definition.Feature.ID,
					metric.Key,
				)
			}
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

type placeholderHealthProbe struct{}

func (placeholderHealthProbe) Check(
	context.Context,
	string,
) (domain.HealthStatus, error) {
	return domain.HealthUnknown, nil
}

func (placeholderHealthProbe) HealthProbeReady() bool {
	return false
}

func TestValidateHealthProbeCoverageRejectsPlaceholderProbe(t *testing.T) {
	definitions := []Definition{
		featureDef(
			"voice_gateway",
			"语音网关",
			"测试功能",
			"voice",
			"语音平台",
			false,
			nil,
			nil,
		),
	}
	err := ValidateHealthProbeCoverage(
		definitions,
		map[string]HealthProbe{
			"voice_gateway": placeholderHealthProbe{},
		},
	)
	if err == nil {
		t.Fatal("placeholder probe passed active feature coverage")
	}
}

func TestDefaultRegistryIncludesRuntimeWiredFeatures(t *testing.T) {
	active := make(map[string]bool)
	for _, definition := range DefaultRegistry() {
		if definition.Feature.Status == domain.FeatureStatusActive {
			active[definition.Feature.ID] = true
		}
	}
	for _, featureID := range []string{"app_update", "voice_conversation"} {
		if !active[featureID] {
			t.Fatalf("runtime-wired feature %q is missing from the active registry", featureID)
		}
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

func TestManualHealthCheckRecordsSafeAttributedAudit(t *testing.T) {
	repository := newMemoryRepository()
	audit := &recordingAudit{}
	service, err := New(Options{
		Repository: repository,
		Definitions: []Definition{
			registeredDefinition(t, "voice_gateway"),
		},
		HealthProbes: map[string]HealthProbe{
			"voice_gateway": HealthProbeFunc(func(
				_ context.Context,
				_ string,
			) (domain.HealthStatus, error) {
				return domain.HealthDegraded, nil
			}),
		},
		Clock: &advancingClock{
			current: time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC),
			step:    0,
		},
		Audit: audit,
	})
	if err != nil {
		t.Fatalf("create service: %v", err)
	}
	_, err = service.CheckHealthAs(
		context.Background(),
		"voice_gateway",
		"admin-001",
	)
	if err != nil {
		t.Fatalf("check health: %v", err)
	}
	entries := audit.snapshot()
	if len(entries) != 1 {
		t.Fatalf("audit entry count = %d, want 1", len(entries))
	}
	entry := entries[0]
	if entry.ActorID != "admin-001" ||
		entry.Action != "admin.feature_center.health.checked" ||
		entry.Detail["feature_id"] != "voice_gateway" ||
		entry.Detail["status"] != string(domain.HealthDegraded) ||
		entry.Detail["trigger"] != domain.HealthTriggerManual ||
		entry.Detail["message"] != "功能可用，但部分能力受到影响" {
		t.Fatalf("unexpected health audit: %+v", entry)
	}
	if _, ok := entry.Detail["checked_at"].(time.Time); !ok {
		t.Fatalf("checked_at audit value = %#v, want time.Time", entry.Detail["checked_at"])
	}
}

func TestManualHealthAuditFailureReturnsUpdatedFeature(t *testing.T) {
	repository := newMemoryRepository()
	service, err := New(Options{
		Repository: repository,
		Definitions: []Definition{
			registeredDefinition(t, "voice_gateway"),
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
		Audit: &recordingAudit{
			err: errors.New("audit storage unavailable"),
		},
	})
	if err != nil {
		t.Fatalf("create service: %v", err)
	}

	feature, err := service.CheckHealthAs(
		context.Background(),
		"voice_gateway",
		"admin-001",
	)
	var auditErr *AuditWriteError
	if !errors.As(err, &auditErr) || !auditErr.PrimaryWriteCompleted {
		t.Fatalf("expected completed-primary audit error, got %v", err)
	}
	if feature == nil ||
		feature.Health != domain.HealthHealthy ||
		feature.HealthDetail == nil {
		t.Fatalf("audit failure did not return refreshed health: %+v", feature)
	}
	stored, getErr := repository.GetHealth(context.Background(), "voice_gateway")
	if getErr != nil || stored.Status != domain.HealthHealthy {
		t.Fatalf("health snapshot was not retained: %+v %v", stored, getErr)
	}
}

func TestProbeFailureLeavesPreviousHealthSnapshotUntouched(t *testing.T) {
	repository := newMemoryRepository()
	currentStatus := domain.HealthHealthy
	service, err := New(Options{
		Repository: repository,
		Definitions: []Definition{
			registeredDefinition(t, "voice_gateway"),
		},
		HealthProbes: map[string]HealthProbe{
			"voice_gateway": HealthProbeFunc(func(
				_ context.Context,
				_ string,
			) (domain.HealthStatus, error) {
				if currentStatus == "" {
					return "", errors.New("probe socket unavailable")
				}
				return currentStatus, nil
			}),
		},
		Clock: &advancingClock{
			current: time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC),
			step:    time.Second,
		},
	})
	if err != nil {
		t.Fatalf("create service: %v", err)
	}
	initial, err := service.CheckHealth(
		context.Background(),
		"voice_gateway",
	)
	if err != nil {
		t.Fatalf("initial check: %v", err)
	}
	if initial.HealthDetail == nil {
		t.Fatal("initial health detail missing")
	}
	checkedAt := initial.HealthDetail.CheckedAt

	currentStatus = ""
	if _, err := service.CheckHealth(
		context.Background(),
		"voice_gateway",
	); err == nil {
		t.Fatal("expected probe failure")
	}
	stored, err := repository.GetHealth(
		context.Background(),
		"voice_gateway",
	)
	if err != nil {
		t.Fatalf("read stored health: %v", err)
	}
	if stored.Status != domain.HealthHealthy ||
		stored.CheckedAt != checkedAt {
		t.Fatalf("probe failure overwrote last known health: %+v", stored)
	}
}

func TestHealthSnapshotFailureDoesNotReturnHealthyProjection(t *testing.T) {
	repository := &failingHealthRepository{
		memoryRepository: newMemoryRepository(),
	}
	service, err := New(Options{
		Repository: repository,
		Definitions: []Definition{
			registeredDefinition(t, "voice_gateway"),
		},
		HealthProbes: map[string]HealthProbe{
			"voice_gateway": HealthProbeFunc(func(
				_ context.Context,
				_ string,
			) (domain.HealthStatus, error) {
				return domain.HealthHealthy, nil
			}),
		},
		Clock:          fixedClock{},
		SchedulerLease: &memorySchedulerLease{},
	})
	if err != nil {
		t.Fatalf("create service: %v", err)
	}
	feature, err := service.CheckHealth(
		context.Background(),
		"voice_gateway",
	)
	if err == nil || feature != nil {
		t.Fatalf(
			"persistence failure must not return a healthy projection: %+v %v",
			feature,
			err,
		)
	}
}

func TestStaleHealthSnapshotDoesNotAuditOrProjectSuccess(t *testing.T) {
	repository := newMemoryRepository()
	staleAt := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	if err := repository.SaveHealth(context.Background(), &domain.HealthSnapshot{
		FeatureID: "voice_gateway",
		Status:    domain.HealthUnavailable,
		CheckedAt: staleAt.Add(time.Minute),
		Message:   "功能暂时不可用",
		LatencyMS: 9,
		Trigger:   domain.HealthTriggerSchedule,
	}); err != nil {
		t.Fatalf("seed newer health snapshot: %v", err)
	}
	audit := &recordingAudit{}
	service, err := New(Options{
		Repository: repository,
		Definitions: []Definition{
			registeredDefinition(t, "voice_gateway"),
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
		Audit: audit,
	})
	if err != nil {
		t.Fatalf("create service: %v", err)
	}

	feature, err := service.CheckHealthAs(
		context.Background(),
		"voice_gateway",
		"admin-001",
	)
	if !errors.Is(err, domain.ErrHealthSnapshotStale) {
		t.Fatalf("expected stale snapshot error, got %v", err)
	}
	if feature != nil {
		t.Fatalf("stale snapshot returned a successful projection: %+v", feature)
	}
	if entries := audit.snapshot(); len(entries) != 0 {
		t.Fatalf("stale snapshot produced audit success: %+v", entries)
	}
	stored, err := repository.GetHealth(context.Background(), "voice_gateway")
	if err != nil {
		t.Fatalf("load stored health: %v", err)
	}
	if stored.Status != domain.HealthUnavailable ||
		stored.CheckedAt != staleAt.Add(time.Minute) {
		t.Fatalf("newer snapshot was overwritten: %+v", stored)
	}
}

func TestScheduledHealthCheckRecordsSystemAudit(t *testing.T) {
	repository := newMemoryRepository()
	audit := &recordingAudit{}
	service, err := New(Options{
		Repository: repository,
		Definitions: []Definition{
			registeredDefinition(t, "service_version"),
		},
		HealthProbes: map[string]HealthProbe{
			"service_version": HealthProbeFunc(func(
				_ context.Context,
				_ string,
			) (domain.HealthStatus, error) {
				return domain.HealthUnavailable, nil
			}),
		},
		Clock: fixedClock{},
		Audit: audit,
	})
	if err != nil {
		t.Fatalf("create service: %v", err)
	}
	if _, err := service.CheckScheduledHealth(
		context.Background(),
		"service_version",
	); err != nil {
		t.Fatalf("check scheduled health: %v", err)
	}
	entries := audit.snapshot()
	if len(entries) != 1 {
		t.Fatalf("audit entry count = %d, want 1", len(entries))
	}
	entry := entries[0]
	if entry.ActorID != "" ||
		entry.Action != "system.feature_center.health.checked" ||
		entry.Detail["status"] != string(domain.HealthUnavailable) ||
		entry.Detail["trigger"] != domain.HealthTriggerSchedule {
		t.Fatalf("unexpected scheduled audit: %+v", entry)
	}
}

func TestScheduledHealthContinuesAfterOneProbeFails(t *testing.T) {
	repository := newMemoryRepository()
	definitions := []Definition{
		registeredDefinition(t, "voice_gateway"),
		registeredDefinition(t, "service_version"),
	}
	service, err := New(Options{
		Repository:  repository,
		Definitions: definitions,
		HealthProbes: map[string]HealthProbe{
			"voice_gateway": HealthProbeFunc(func(
				_ context.Context,
				_ string,
			) (domain.HealthStatus, error) {
				return "", errors.New("voice probe unavailable")
			}),
			"service_version": HealthProbeFunc(func(
				_ context.Context,
				_ string,
			) (domain.HealthStatus, error) {
				return domain.HealthHealthy, nil
			}),
		},
		Clock:          fixedClock{},
		SchedulerLease: &memorySchedulerLease{},
	})
	if err != nil {
		t.Fatalf("create service: %v", err)
	}
	features, err := service.CheckAllScheduledHealth(context.Background())
	if err == nil {
		t.Fatal("expected aggregate scheduler error")
	}
	if len(features) != 1 || features[0].ID != "service_version" {
		t.Fatalf("later healthy feature was not checked: %+v", features)
	}
	if _, err := repository.GetHealth(
		context.Background(),
		"service_version",
	); err != nil {
		t.Fatalf("second feature snapshot missing: %v", err)
	}
	if _, err := repository.GetHealth(
		context.Background(),
		"voice_gateway",
	); !errors.Is(err, domain.ErrFeatureConfigNotFound) {
		t.Fatalf("failed probe fabricated a snapshot: %v", err)
	}
}

func TestScheduledHealthRequiresSharedLeaderLease(t *testing.T) {
	service, repository := newTestService(t)
	service.schedulerLease = nil

	features, err := service.CheckAllScheduledHealth(context.Background())
	if !errors.Is(err, domain.ErrSchedulerLeaseRequired) {
		t.Fatalf("expected required lease error, got %v", err)
	}
	if features != nil {
		t.Fatalf("unexpected features without lease: %+v", features)
	}
	if _, err := repository.GetHealth(
		context.Background(),
		"voice_gateway",
	); !errors.Is(err, domain.ErrFeatureConfigNotFound) {
		t.Fatalf("scheduler without lease persisted health: %v", err)
	}
}

func TestScheduledHealthSkipsWhenAnotherReplicaOwnsLease(t *testing.T) {
	outerLease := &memorySchedulerLease{}
	entered := make(chan struct{})
	release := make(chan struct{})
	exited := make(chan struct{})
	go func() {
		defer close(exited)
		leaseErr := outerLease.WithLeaderLease(context.Background(), func(context.Context) error {
			close(entered)
			<-release
			return nil
		})
		if leaseErr != nil {
			t.Errorf("outer lease: %v", leaseErr)
		}
	}()
	<-entered
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
		<-exited
	}()
	service, repository := newTestService(t)
	service.schedulerLease = outerLease

	features, err := service.CheckAllScheduledHealth(context.Background())
	if !errors.Is(err, domain.ErrSchedulerLeaseUnavailable) {
		t.Fatalf("expected lease unavailable error, got %v", err)
	}
	if features != nil {
		t.Fatalf("skipped scheduler returned features: %+v", features)
	}
	if _, err := repository.GetHealth(
		context.Background(),
		"voice_gateway",
	); !errors.Is(err, domain.ErrFeatureConfigNotFound) {
		t.Fatalf("non-leader replica persisted health: %v", err)
	}
}

type failingCallbackLease struct{}

func (failingCallbackLease) WithLeaderLease(
	_ context.Context,
	work func(context.Context) error,
) error {
	return work(context.Background())
}

type failingReleaseLease struct{}

func (failingReleaseLease) WithLeaderLease(
	ctx context.Context,
	work func(context.Context) error,
) error {
	workErr := work(ctx)
	return &repository.SchedulerLeaseReleaseError{
		Err: errors.Join(domain.ErrSchedulerLeaseReleaseFailed, workErr),
	}
}

func TestScheduledHealthPreservesProbeFailureWhenLeaseSucceeds(t *testing.T) {
	service, _ := newTestService(t)
	service.schedulerLease = failingCallbackLease{}
	service.probes["voice_gateway"] = HealthProbeFunc(func(
		context.Context,
		string,
	) (domain.HealthStatus, error) {
		return "", errors.New("probe unavailable")
	})

	features, err := service.CheckAllScheduledHealth(context.Background())
	if err == nil || !strings.Contains(err.Error(), "probe unavailable") {
		t.Fatalf("scheduled cycle failure = %v, want probe cause", err)
	}
	if errors.Is(err, domain.ErrSchedulerLeaseReleaseFailed) {
		t.Fatalf("successful lease release was misreported: %v", err)
	}
	if len(features) != 0 {
		t.Fatalf("failed probe returned features: %+v", features)
	}
}

func TestScheduledHealthPreservesReleaseFailureWhenProbeAlsoFails(t *testing.T) {
	service, _ := newTestService(t)
	service.schedulerLease = failingReleaseLease{}
	service.probes["voice_gateway"] = HealthProbeFunc(func(
		context.Context,
		string,
	) (domain.HealthStatus, error) {
		return "", errors.New("probe unavailable")
	})

	_, err := service.CheckAllScheduledHealth(context.Background())
	if !errors.Is(err, domain.ErrSchedulerLeaseReleaseFailed) {
		t.Fatalf("scheduled lease failure = %v, want release failure", err)
	}
	if !strings.Contains(err.Error(), "probe unavailable") {
		t.Fatalf("scheduled lease failure = %v, want callback cause retained", err)
	}
}

func TestScheduledLeaseProtectsOneCycleAcrossServiceInstances(t *testing.T) {
	lease := &memorySchedulerLease{}
	repository := newMemoryRepository()
	definitions := []Definition{registeredDefinition(t, "voice_gateway")}
	var probeCalls int
	var probeMu sync.Mutex
	started := make(chan struct{}, 1)
	probe := HealthProbeFunc(func(
		_ context.Context,
		_ string,
	) (domain.HealthStatus, error) {
		probeMu.Lock()
		probeCalls++
		probeMu.Unlock()
		select {
		case started <- struct{}{}:
		default:
		}
		time.Sleep(100 * time.Millisecond)
		return domain.HealthHealthy, nil
	})
	newInstance := func() *Service {
		instance, err := New(Options{
			Repository:     repository,
			Definitions:    definitions,
			HealthProbes:   map[string]HealthProbe{"voice_gateway": probe},
			Clock:          fixedClock{},
			SchedulerLease: lease,
		})
		if err != nil {
			t.Fatalf("create service instance: %v", err)
		}
		return instance
	}

	errCh := make(chan error, 1)
	go func() {
		_, err := newInstance().CheckAllScheduledHealth(context.Background())
		errCh <- err
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("leader probe did not start")
	}

	second, err := newInstance().CheckAllScheduledHealth(context.Background())
	if !errors.Is(err, domain.ErrSchedulerLeaseUnavailable) {
		t.Fatalf("second replica should skip while lease is held, got %v", err)
	}
	if second != nil {
		t.Fatalf("second replica returned work: %+v", second)
	}
	probeMu.Lock()
	calls := probeCalls
	probeMu.Unlock()
	if calls != 1 {
		t.Fatalf("probe calls = %d, want exactly one leader cycle", calls)
	}

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("leader cycle = %v, want nil", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("leader cycle did not finish")
	}
}

func TestRunHealthSchedulerRunsOneFullIntervalAfterSlowCycle(t *testing.T) {
	service, _ := newTestService(t)
	firstStarted := make(chan struct{})
	secondStarted := make(chan time.Time, 1)
	var calls atomic.Int64
	service.probes["voice_gateway"] = HealthProbeFunc(func(
		context.Context,
		string,
	) (domain.HealthStatus, error) {
		call := calls.Add(1)
		if call == 1 {
			close(firstStarted)
			time.Sleep(40 * time.Millisecond)
		} else if call == 2 {
			secondStarted <- time.Now()
		}
		return domain.HealthHealthy, nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := make(chan error, 1)
	startedAt := time.Now()
	go func() {
		errCh <- service.RunHealthScheduler(ctx, 80*time.Millisecond)
	}()
	<-firstStarted
	time.Sleep(55 * time.Millisecond)
	if got := calls.Load(); got != 1 {
		t.Fatalf("cycles within fixed ticker window = %d, want 1", got)
	}
	select {
	case secondAt := <-secondStarted:
		if elapsed := secondAt.Sub(startedAt); elapsed < 110*time.Millisecond {
			t.Fatalf("second cycle started too early after %s", elapsed)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("second scheduled cycle did not start")
	}
	cancel()
	if err := <-errCh; !errors.Is(err, context.Canceled) {
		t.Fatalf("scheduler shutdown = %v, want context.Canceled", err)
	}
}

func TestScheduledAuditFailureRetainsHealthAndCanBeDrained(t *testing.T) {
	repository := newMemoryRepository()
	audit := &recordingAudit{err: errors.New("audit storage unavailable")}
	service, err := New(Options{
		Repository: repository,
		Definitions: []Definition{
			registeredDefinition(t, "voice_gateway"),
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
		Audit: audit,
	})
	if err != nil {
		t.Fatalf("create service: %v", err)
	}
	feature, err := service.CheckScheduledHealth(
		context.Background(),
		"voice_gateway",
	)
	if err != nil {
		t.Fatalf("scheduled check should retain its snapshot: %v", err)
	}
	if feature.Health != domain.HealthHealthy {
		t.Fatalf("health = %q, want healthy", feature.Health)
	}
	failures := service.DrainAuditFailures()
	if len(failures) != 1 ||
		failures[0].Snapshot == nil ||
		failures[0].Snapshot.FeatureID != "voice_gateway" {
		t.Fatalf("unexpected audit failures: %+v", failures)
	}
	if remaining := service.DrainAuditFailures(); len(remaining) != 0 {
		t.Fatalf("drain did not clear failures: %+v", remaining)
	}
}

func TestAuditFailureQueueIsRaceSafeUnderConcurrentDrain(t *testing.T) {
	repository := newMemoryRepository()
	audit := &recordingAudit{err: errors.New("audit storage unavailable")}
	service, err := New(Options{
		Repository: repository,
		Definitions: []Definition{
			registeredDefinition(t, "voice_gateway"),
		},
		HealthProbes: map[string]HealthProbe{
			"voice_gateway": HealthProbeFunc(func(
				_ context.Context,
				_ string,
			) (domain.HealthStatus, error) {
				return domain.HealthHealthy, nil
			}),
		},
		Clock:          fixedClock{},
		Audit:          audit,
		SchedulerLease: &memorySchedulerLease{},
	})
	if err != nil {
		t.Fatalf("create service: %v", err)
	}

	const workers = 8
	const checksPerWorker = 25
	var wg sync.WaitGroup
	var totalDrained int64
	for index := 0; index < workers; index++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for attempt := 0; attempt < checksPerWorker; attempt++ {
				_, _ = service.CheckScheduledHealth(
					context.Background(),
					"voice_gateway",
				)
				atomic.AddInt64(
					&totalDrained,
					int64(len(service.DrainAuditFailures())),
				)
			}
		}()
	}
	wg.Wait()
	totalDrained += int64(len(service.DrainAuditFailures()))
	if totalDrained != workers*checksPerWorker {
		t.Fatalf(
			"audit failures drained = %d, want %d",
			totalDrained,
			workers*checksPerWorker,
		)
	}
}

func TestValidateHealthProbeCoverageRejectsMissingActiveProbe(t *testing.T) {
	err := ValidateHealthProbeCoverage(
		[]Definition{
			featureDef(
				"voice_gateway",
				"语音网关",
				"测试功能",
				"voice",
				"语音平台",
				true,
				nil,
				nil,
			),
		},
		nil,
	)
	if err == nil {
		t.Fatal("expected active feature without health probe to be rejected")
	}
}

func TestCheckHealthRejectsFeatureWithoutProbe(t *testing.T) {
	service, _ := newTestService(t)
	_, err := service.CheckHealth(context.Background(), "deployment_infrastructure")
	if !errors.Is(err, domain.ErrHealthCheckUnavailable) {
		t.Fatalf("expected unavailable health check, got %v", err)
	}
}

func TestCheckHealthPersistsSnapshotAcrossServiceRestart(t *testing.T) {
	repository := newMemoryRepository()
	definitions := []Definition{registeredDefinition(t, "voice_gateway")}
	probes := map[string]HealthProbe{
		"voice_gateway": HealthProbeFunc(func(
			_ context.Context,
			_ string,
		) (domain.HealthStatus, error) {
			return domain.HealthDegraded, nil
		}),
	}
	checkedAt := time.Date(2026, 10, 8, 12, 0, 1, 0, time.UTC)
	first, err := New(Options{
		Repository:   repository,
		Definitions:  definitions,
		HealthProbes: probes,
		Clock: &advancingClock{
			current: checkedAt,
			step:    25 * time.Millisecond,
		},
	})
	if err != nil {
		t.Fatalf("create first service: %v", err)
	}
	checked, err := first.CheckHealth(context.Background(), "voice_gateway")
	if err != nil {
		t.Fatalf("check health: %v", err)
	}
	if checked.Health != domain.HealthDegraded ||
		checked.HealthDetail == nil ||
		checked.HealthDetail.LatencyMS == nil ||
		*checked.HealthDetail.LatencyMS != 25 {
		t.Fatalf("unexpected checked projection: %+v", checked.HealthDetail)
	}

	restarted, err := New(Options{
		Repository:   repository,
		Definitions:  definitions,
		HealthProbes: probes,
		Clock:        fixedClock{},
	})
	if err != nil {
		t.Fatalf("create restarted service: %v", err)
	}
	features, err := restarted.List(context.Background())
	if err != nil {
		t.Fatalf("list after restart: %v", err)
	}
	if len(features) != 1 {
		t.Fatalf("feature count = %d, want 1", len(features))
	}
	feature := features[0]
	if feature.Health != domain.HealthDegraded ||
		feature.HealthDetail == nil {
		t.Fatalf("durable health missing after restart: %+v", feature)
	}
	if feature.HealthDetail.CheckedAt != checked.HealthDetail.CheckedAt ||
		feature.HealthDetail.Message != "功能可用，但部分能力受到影响" ||
		feature.HealthDetail.LatencyMS == nil ||
		*feature.HealthDetail.LatencyMS != 25 {
		t.Fatalf("durable health detail changed: %+v", feature.HealthDetail)
	}

	// The registry definition is shared with other services; recording a
	// health observation must not mutate it in place.
	inspection, err := restarted.Get(context.Background(), "voice_gateway")
	if err != nil {
		t.Fatalf("get after restart: %v", err)
	}
	if inspection.Health != domain.HealthDegraded {
		t.Fatalf("get health = %q, want degraded", inspection.Health)
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
	err    error
}

func (reader staticMetricsReader) FeatureMetrics(
	_ context.Context,
	featureID string,
) (map[string]any, error) {
	return reader.values[featureID], reader.err
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
	if parentAccount.MetricsState != domain.MetricsStateAvailable {
		t.Fatalf("metrics state = %q, want available", parentAccount.MetricsState)
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
	if aiAccount.MetricsState != domain.MetricsStateNotReported {
		t.Fatalf(
			"unreported metrics state = %q, want not_reported",
			aiAccount.MetricsState,
		)
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
	if feature.MetricsState != domain.MetricsStateNotConfigured {
		t.Fatalf(
			"metrics state = %q, want not_configured",
			feature.MetricsState,
		)
	}
}

func TestProjectMarksMetricsReaderFailureUnavailable(t *testing.T) {
	service, err := New(Options{
		Repository:  newMemoryRepository(),
		Definitions: DefaultRegistry(),
		Clock:       fixedClock{},
	})
	if err != nil {
		t.Fatalf("create service: %v", err)
	}
	service.SetMetricsReader(staticMetricsReader{
		err: errors.New("metrics database unavailable"),
	})

	feature, err := service.Get(context.Background(), "parent_account")
	if err != nil {
		t.Fatalf("get parent_account: %v", err)
	}
	if feature.MetricsState != domain.MetricsStateUnavailable {
		t.Fatalf("metrics state = %q, want unavailable", feature.MetricsState)
	}
	if feature.ReadOnlyMetrics[0].Value != nil {
		t.Fatalf(
			"failed metrics read produced a value: %#v",
			feature.ReadOnlyMetrics[0].Value,
		)
	}
}

func TestProjectMarksMetricsNotReportedWhenReaderReturnsNoRegisteredValues(t *testing.T) {
	service, err := New(Options{
		Repository:  newMemoryRepository(),
		Definitions: DefaultRegistry(),
		Clock:       fixedClock{},
	})
	if err != nil {
		t.Fatalf("create service: %v", err)
	}
	service.SetMetricsReader(staticMetricsReader{
		values: map[string]map[string]any{
			"parent_account": {"unregistered_metric": int64(9)},
		},
	})

	feature, err := service.Get(context.Background(), "parent_account")
	if err != nil {
		t.Fatalf("get parent_account: %v", err)
	}
	if feature.MetricsState != domain.MetricsStateNotReported {
		t.Fatalf("metrics state = %q, want not_reported", feature.MetricsState)
	}
	if feature.ReadOnlyMetrics[0].Value != nil {
		t.Fatalf("unregistered metric value leaked into projection: %#v", feature.ReadOnlyMetrics[0].Value)
	}
}

func TestVoiceGatewayHealthDerivedMetricUsesRetainedState(t *testing.T) {
	service, err := New(Options{
		Repository:  newMemoryRepository(),
		Definitions: DefaultRegistry(),
		Clock:       fixedClock{},
	})
	if err != nil {
		t.Fatalf("create service: %v", err)
	}
	for _, test := range []struct {
		health domain.HealthStatus
		want   any
	}{
		{health: domain.HealthHealthy, want: "ready"},
		{health: domain.HealthDegraded, want: "degraded"},
		{health: domain.HealthUnavailable, want: "unavailable"},
		{health: domain.HealthUnknown, want: nil},
	} {
		t.Run(string(test.health), func(t *testing.T) {
			service.probes = map[string]HealthProbe{
				"voice_gateway": HealthProbeFunc(func(
					_ context.Context,
					_ string,
				) (domain.HealthStatus, error) {
					return test.health, nil
				}),
			}
			feature, err := service.CheckHealth(context.Background(), "voice_gateway")
			if err != nil {
				t.Fatalf("check health: %v", err)
			}
			if len(feature.ReadOnlyMetrics) != 1 {
				t.Fatalf("voice gateway metric count = %d, want 1", len(feature.ReadOnlyMetrics))
			}
			if got := feature.ReadOnlyMetrics[0].Value; got != test.want {
				t.Fatalf("gateway metric = %#v, want %#v", got, test.want)
			}
		})
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
