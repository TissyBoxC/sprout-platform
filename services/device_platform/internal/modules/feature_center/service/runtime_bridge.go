package service

import (
	"context"
	"strings"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/feature_center/domain"
	operationsdomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/operations/domain"
)

// RuntimeSettingsReader is the narrow existing-configuration surface used to
// keep registered feature values aligned with the services that actually
// consume them.
type RuntimeSettingsReader interface {
	Settings(ctx context.Context) (*operationsdomain.Settings, int64, error)
	UpdateSettings(
		ctx context.Context,
		settings *operationsdomain.Settings,
		actorAccountID string,
		expectedVersion int64,
	) (*operationsdomain.Settings, int64, error)
}

// ModelCatalogReader supplies the live AI model identifiers so the AI account
// feature can offer real selectable options instead of a static placeholder.
type ModelCatalogReader interface {
	ModelOptions(ctx context.Context) ([]ModelOption, error)
}

// MetricsReader supplies live, read-only operational values for registered
// feature metrics. Implementations must scope their queries to the specific
// feature so an unknown or removed metric can never leak another module's data.
type MetricsReader interface {
	FeatureMetrics(
		ctx context.Context,
		featureID string,
	) (map[string]any, error)
}

// ModelOption is one selectable AI model projected from the gateway catalogue.
type ModelOption struct {
	ID    string
	Label string
}

// SetRuntimeSettingsReader wires existing operations settings into the feature
// center. It is optional so pure unit tests can use an isolated repository.
func (s *Service) SetRuntimeSettingsReader(reader RuntimeSettingsReader) {
	if s == nil {
		return
	}
	s.runtimeSettings = reader
}

// SetModelCatalogReader wires the live AI model catalogue used to populate the
// AI account model options. It is optional.
func (s *Service) SetModelCatalogReader(reader ModelCatalogReader) {
	if s == nil {
		return
	}
	s.modelCatalog = reader
}

// SetMetricsReader wires the live read-only metric source. It is optional so
// pure unit tests keep registry-only behavior.
func (s *Service) SetMetricsReader(reader MetricsReader) {
	if s == nil {
		return
	}
	s.metrics = reader
}

// enrichMetrics replaces every registered metric value with the live value
// reported by the metrics reader. Metrics without a value stay empty so the
// console continues to distinguish "not reported" from a real zero.
func (s *Service) enrichMetrics(
	ctx context.Context,
	feature *domain.Feature,
) {
	if feature == nil {
		return
	}
	feature.MetricsState = domain.MetricsStateNotConfigured
	if len(feature.ReadOnlyMetrics) == 0 {
		return
	}
	if s == nil || s.metrics == nil {
		applyHealthDerivedMetrics(feature)
		return
	}
	values, err := s.metrics.FeatureMetrics(ctx, feature.ID)
	if err != nil {
		feature.MetricsState = domain.MetricsStateUnavailable
		applyHealthDerivedMetrics(feature)
		return
	}
	reported := 0
	for index := range feature.ReadOnlyMetrics {
		key := feature.ReadOnlyMetrics[index].Key
		value, ok := values[key]
		if !ok {
			continue
		}
		feature.ReadOnlyMetrics[index].Value = value
		reported++
	}
	if applyHealthDerivedMetrics(feature) {
		reported++
	}
	if reported == 0 {
		feature.MetricsState = domain.MetricsStateNotReported
		return
	}
	feature.MetricsState = domain.MetricsStateAvailable
}

// applyHealthDerivedMetrics exposes only values the service can calculate from
// an already-persisted health observation. It never turns unknown into a
// successful state; unknown remains an empty metric value.
func applyHealthDerivedMetrics(feature *domain.Feature) bool {
	if feature == nil || feature.ID != "voice_gateway" {
		return false
	}
	var value any
	switch feature.Health {
	case domain.HealthHealthy:
		value = "ready"
	case domain.HealthDegraded:
		value = "degraded"
	case domain.HealthUnavailable:
		value = "unavailable"
	default:
		return false
	}
	for index := range feature.ReadOnlyMetrics {
		if feature.ReadOnlyMetrics[index].Key != "gateway_ready_state" {
			continue
		}
		feature.ReadOnlyMetrics[index].Value = value
		return true
	}
	return false
}

// runtimeValues projects existing operations settings into feature-center
// values. Only fields that have a real runtime consumer are included.
func (s *Service) runtimeValues(
	ctx context.Context,
	featureID string,
) (map[string]any, int64, error) {
	if s == nil || s.runtimeSettings == nil {
		return nil, 0, nil
	}
	settings, version, err := s.runtimeSettings.Settings(ctx)
	if err != nil {
		return nil, 0, err
	}
	switch featureID {
	case "ai_account":
		return map[string]any{
			"default_balance_usd": settings.AI.DefaultBalanceUSD,
			"default_concurrency": settings.AI.DefaultConcurrency,
			"default_models":      append([]string(nil), settings.AI.DefaultModels...),
		}, version, nil
	case "auth":
		return map[string]any{
			"registration_enabled":        settings.Account.RegistrationEnabled,
			"phone_verification_required": settings.Account.PhoneVerificationRequired,
			"email_login_enabled":         settings.Account.EmailLoginEnabled,
		}, version, nil
	case "app_update":
		return map[string]any{
			"default_channel":            settings.Update.Channel,
			"minimum_client_version":     settings.Update.MinClientVersion,
			"mandatory_update_threshold": settings.Update.ForceUpgradeBelow,
		}, version, nil
	case "platform_security":
		return map[string]any{
			"minor_mode_default":          settings.Safety.MinorModeDefault,
			"output_moderation_enabled":   settings.Safety.OutputModerationEnabled,
			"crisis_intervention_enabled": settings.Safety.CrisisInterventionEnabled,
		}, version, nil
	case "voice_conversation":
		return map[string]any{
			"continuous_conversation_enabled": settings.Voice.ContinuousConversationEnabled,
			"idle_window_seconds":             settings.Voice.IdleWindowSeconds,
			"barge_in_enabled":                settings.Voice.BargeInEnabled,
			"far_field_enabled":               settings.Voice.FarFieldEnabled,
		}, version, nil
	case "usage_report":
		return map[string]any{
			"retention_days": settings.Retention.UsageReportDays,
		}, version, nil
	default:
		return nil, 0, nil
	}
}

// updateRuntimeValues applies only the fields that are backed by an existing
// runtime consumer. Unknown or purely registry-local values return false and
// remain persisted by the feature-center repository.
func (s *Service) updateRuntimeValues(
	ctx context.Context,
	featureID string,
	values map[string]any,
	actorID string,
	expectedVersion int64,
) (bool, error) {
	if s == nil || s.runtimeSettings == nil {
		return false, nil
	}
	settings, _, err := s.runtimeSettings.Settings(ctx)
	if err != nil {
		return false, err
	}
	switch featureID {
	case "ai_account":
		if value, ok := numberValue(values, "default_balance_usd"); ok {
			settings.AI.DefaultBalanceUSD = value
		}
		if value, ok := intValue(values, "default_concurrency"); ok {
			settings.AI.DefaultConcurrency = value
		}
		if value, ok := stringListValue(values, "default_models"); ok {
			settings.AI.DefaultModels = value
		}
		_, _, err := s.runtimeSettings.UpdateSettings(
			ctx,
			settings,
			actorID,
			expectedVersion,
		)
		return true, err
	case "auth":
		if value, ok := boolValue(values, "registration_enabled"); ok {
			settings.Account.RegistrationEnabled = value
		}
		if value, ok := boolValue(values, "phone_verification_required"); ok {
			settings.Account.PhoneVerificationRequired = value
		}
		if value, ok := boolValue(values, "email_login_enabled"); ok {
			settings.Account.EmailLoginEnabled = value
		}
		_, _, err := s.runtimeSettings.UpdateSettings(
			ctx,
			settings,
			actorID,
			expectedVersion,
		)
		return true, err
	case "app_update":
		if value, ok := strValue(values, "default_channel"); ok {
			settings.Update.Channel = value
		}
		if value, ok := strValue(values, "minimum_client_version"); ok {
			settings.Update.MinClientVersion = value
		}
		if value, ok := strValue(values, "mandatory_update_threshold"); ok {
			settings.Update.ForceUpgradeBelow = value
		}
		_, _, err := s.runtimeSettings.UpdateSettings(
			ctx,
			settings,
			actorID,
			expectedVersion,
		)
		return true, err
	case "platform_security":
		if value, ok := boolValue(values, "minor_mode_default"); ok {
			settings.Safety.MinorModeDefault = value
		}
		if value, ok := boolValue(values, "output_moderation_enabled"); ok {
			settings.Safety.OutputModerationEnabled = value
		}
		if value, ok := boolValue(values, "crisis_intervention_enabled"); ok {
			settings.Safety.CrisisInterventionEnabled = value
		}
		_, _, err := s.runtimeSettings.UpdateSettings(
			ctx,
			settings,
			actorID,
			expectedVersion,
		)
		return true, err
	case "voice_conversation":
		if value, ok := boolValue(values, "continuous_conversation_enabled"); ok {
			settings.Voice.ContinuousConversationEnabled = value
		}
		if value, ok := intValue(values, "idle_window_seconds"); ok {
			settings.Voice.IdleWindowSeconds = value
		}
		if value, ok := boolValue(values, "barge_in_enabled"); ok {
			settings.Voice.BargeInEnabled = value
		}
		if value, ok := boolValue(values, "far_field_enabled"); ok {
			settings.Voice.FarFieldEnabled = value
		}
		settings.SetVoiceConfigured()
		_, _, err := s.runtimeSettings.UpdateSettings(
			ctx,
			settings,
			actorID,
			expectedVersion,
		)
		return true, err
	case "usage_report":
		if value, ok := intValue(values, "retention_days"); ok {
			settings.Retention.UsageReportDays = value
		}
		settings.Retention.SetUsageReportConfigured()
		_, _, err := s.runtimeSettings.UpdateSettings(
			ctx,
			settings,
			actorID,
			expectedVersion,
		)
		return true, err
	default:
		return false, nil
	}
}

func boolValue(values map[string]any, key string) (bool, bool) {
	value, ok := values[key]
	if !ok {
		return false, false
	}
	typed, ok := value.(bool)
	return typed, ok
}

func intValue(values map[string]any, key string) (int, bool) {
	value, ok := values[key]
	if !ok {
		return 0, false
	}
	switch typed := value.(type) {
	case int:
		return typed, true
	case int64:
		return int(typed), true
	case float64:
		if typed != float64(int(typed)) {
			return 0, false
		}
		return int(typed), true
	default:
		return 0, false
	}
}

func numberValue(values map[string]any, key string) (float64, bool) {
	value, ok := values[key]
	if !ok {
		return 0, false
	}
	switch typed := value.(type) {
	case int:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case float64:
		return typed, true
	default:
		return 0, false
	}
}

func stringListValue(values map[string]any, key string) ([]string, bool) {
	value, ok := values[key]
	if !ok {
		return nil, false
	}
	switch typed := value.(type) {
	case []string:
		return append([]string(nil), typed...), true
	case []any:
		result := make([]string, 0, len(typed))
		for _, item := range typed {
			text, ok := item.(string)
			if !ok {
				return nil, false
			}
			result = append(result, strings.TrimSpace(text))
		}
		return result, true
	default:
		return nil, false
	}
}

func strValue(values map[string]any, key string) (string, bool) {
	value, ok := values[key]
	if !ok {
		return "", false
	}
	text, ok := value.(string)
	return strings.TrimSpace(text), ok
}

var _ RuntimeSettingsReader = interface {
	Settings(context.Context) (*operationsdomain.Settings, int64, error)
	UpdateSettings(context.Context, *operationsdomain.Settings, string, int64) (*operationsdomain.Settings, int64, error)
}(nil)
