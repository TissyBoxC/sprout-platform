// Package service owns feature registry projection, configuration validation,
// and health checks.
package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/feature_center/domain"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/feature_center/repository"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/platform/clock"
)

// HealthProbe returns the current health of one registered feature.
type HealthProbe interface {
	Check(ctx context.Context, featureID string) (domain.HealthStatus, error)
}

// HealthProbeFunc adapts a function to HealthProbe.
type HealthProbeFunc func(ctx context.Context, featureID string) (domain.HealthStatus, error)

// Check implements HealthProbe.
func (probe HealthProbeFunc) Check(
	ctx context.Context,
	featureID string,
) (domain.HealthStatus, error) {
	return probe(ctx, featureID)
}

// Options contains feature-center service dependencies.
type Options struct {
	Repository   repository.Repository
	Definitions  []Definition
	HealthProbes map[string]HealthProbe
	Clock        clock.Clock
}

// Service is the feature-center application service.
type Service struct {
	repository  repository.Repository
	definitions []Definition
	byID        map[string]Definition
	probes      map[string]HealthProbe
	clock       clock.Clock

	runtimeSettings RuntimeSettingsReader
	modelCatalog    ModelCatalogReader
}

// ValidateHealthProbeCoverage rejects a runtime composition where an active
// feature has no health probe. Planned features are allowed to omit probes
// because their capability is intentionally not implemented yet.
func ValidateHealthProbeCoverage(
	definitions []Definition,
	probes map[string]HealthProbe,
) error {
	missing := make([]string, 0)
	for _, definition := range definitions {
		if definition.Feature.Status != domain.FeatureStatusActive {
			continue
		}
		if probes[definition.Feature.ID] == nil {
			missing = append(missing, definition.Feature.ID)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	return fmt.Errorf(
		"active features missing health probes: %s",
		strings.Join(missing, ", "),
	)
}

// New creates a feature-center service.
func New(options Options) (*Service, error) {
	if options.Repository == nil {
		return nil, errors.New("feature center repository is required")
	}
	definitions := options.Definitions
	if len(definitions) == 0 {
		definitions = DefaultRegistry()
	}
	byID := make(map[string]Definition, len(definitions))
	for _, definition := range definitions {
		if !validFeatureID(definition.Feature.ID) {
			return nil, fmt.Errorf("%w: %s", domain.ErrInvalidFeatureID, definition.Feature.ID)
		}
		if _, exists := byID[definition.Feature.ID]; exists {
			return nil, fmt.Errorf("duplicate feature id: %s", definition.Feature.ID)
		}
		byID[definition.Feature.ID] = definition
	}
	timeSource := options.Clock
	if timeSource == nil {
		timeSource = clock.SystemClock{}
	}
	return &Service{
		repository:  options.Repository,
		definitions: append([]Definition(nil), definitions...),
		byID:        byID,
		probes:      options.HealthProbes,
		clock:       timeSource,
	}, nil
}

// List returns every registered feature with persisted values merged in.
func (s *Service) List(ctx context.Context) ([]domain.Feature, error) {
	overlays, err := s.repository.List(ctx)
	if err != nil {
		return nil, err
	}
	byID := make(map[string]domain.FeatureConfig, len(overlays))
	for _, overlay := range overlays {
		byID[overlay.FeatureID] = overlay
	}
	result := make([]domain.Feature, 0, len(s.definitions))
	for _, definition := range s.definitions {
		var overlay *domain.FeatureConfig
		if current, ok := byID[definition.Feature.ID]; ok {
			copyConfig := current
			overlay = &copyConfig
		}
		result = append(result, s.project(ctx, definition, overlay))
	}
	return result, nil
}

// Get returns one registered feature.
func (s *Service) Get(ctx context.Context, featureID string) (*domain.Feature, error) {
	featureID = strings.TrimSpace(featureID)
	definition, ok := s.byID[featureID]
	if !ok {
		return nil, domain.ErrFeatureNotFound
	}
	overlay, err := s.repository.Get(ctx, featureID)
	if errors.Is(err, domain.ErrFeatureConfigNotFound) {
		overlay = nil
	} else if err != nil {
		return nil, err
	}
	feature := s.project(ctx, definition, overlay)
	return &feature, nil
}

// UpdateConfig validates and persists one feature's editable values.
func (s *Service) UpdateConfig(
	ctx context.Context,
	featureID string,
	values map[string]any,
	actorID string,
	expectedVersion int64,
) (*domain.Feature, error) {
	featureID = strings.TrimSpace(featureID)
	definition, ok := s.byID[featureID]
	if !ok {
		return nil, domain.ErrFeatureNotFound
	}
	if !hasEditableField(definition.Feature.ConfigSchema) {
		return nil, domain.ErrFeatureReadOnly
	}
	if expectedVersion < 0 {
		return nil, domain.ErrInvalidConfig
	}
	if err := validateSubmittedObject(definition.Feature.ConfigSchema, values, ""); err != nil {
		return nil, err
	}
	runtimeHandled, err := s.updateRuntimeValues(
		ctx,
		featureID,
		values,
		actorID,
		expectedVersion,
	)
	if err != nil {
		return nil, err
	}
	if runtimeHandled {
		return s.Get(ctx, featureID)
	}
	current, err := s.repository.Get(ctx, featureID)
	if errors.Is(err, domain.ErrFeatureConfigNotFound) {
		current = nil
	} else if err != nil {
		return nil, err
	}
	merged := mergeValues(
		effectiveValues(definition.Feature.ConfigSchema),
		valueMap(current),
		values,
	)
	if err := validateObject(definition.Feature.ConfigSchema, merged, definition.ImmutableKeys, ""); err != nil {
		return nil, err
	}
	saved, err := s.repository.Save(ctx, featureID, merged, strings.TrimSpace(actorID), expectedVersion)
	if err != nil {
		return nil, err
	}
	feature := s.project(ctx, definition, saved)
	return &feature, nil
}

// CheckHealth runs the configured probe and returns the resulting feature.
//
// The refreshed health detail is attached to the caller's response only. The
// shared registry map is never mutated, so concurrent list, get, and health
// requests cannot race or leak a previous caller's probe result.
func (s *Service) CheckHealth(
	ctx context.Context,
	featureID string,
) (*domain.Feature, error) {
	featureID = strings.TrimSpace(featureID)
	definition, ok := s.byID[featureID]
	if !ok {
		return nil, domain.ErrFeatureNotFound
	}
	probe := s.probes[featureID]
	if probe == nil {
		return nil, domain.ErrHealthCheckUnavailable
	}
	startedAt := s.clock.Now().UTC()
	status, err := probe.Check(ctx, featureID)
	if err != nil {
		return nil, err
	}
	if !status.Valid() {
		return nil, domain.ErrHealthCheckFailed
	}
	overlay, err := s.repository.Get(ctx, featureID)
	if errors.Is(err, domain.ErrFeatureConfigNotFound) {
		overlay = nil
	} else if err != nil {
		return nil, err
	}
	feature := s.project(ctx, definition, overlay)
	elapsed := int(s.clock.Now().UTC().Sub(startedAt).Milliseconds())
	if elapsed < 0 {
		elapsed = 0
	}
	checkedAt := s.clock.Now().UTC()
	feature.Health = status
	feature.HealthDetail = &domain.HealthDetail{
		Status:    status,
		CheckedAt: checkedAt,
		Message:   healthMessage(status),
		LatencyMS: &elapsed,
	}
	return &feature, nil
}

func healthMessage(status domain.HealthStatus) string {
	switch status {
	case domain.HealthHealthy:
		return "功能运行正常"
	case domain.HealthDegraded:
		return "功能可用，但部分能力受到影响"
	case domain.HealthUnavailable:
		return "功能暂时不可用"
	default:
		return "暂时无法确认功能状态"
	}
}

func (s *Service) project(
	ctx context.Context,
	definition Definition,
	overlay *domain.FeatureConfig,
) domain.Feature {
	feature := definition.Feature
	feature.ConfigSchema = cloneFields(feature.ConfigSchema)
	feature.Values = effectiveValues(feature.ConfigSchema)
	feature.ConfigVersion = 0
	feature.UpdatedAt = nil
	if runtimeValues, version, err := s.runtimeValues(ctx, feature.ID); err == nil &&
		len(runtimeValues) > 0 {
		feature.Values = mergeValues(feature.Values, runtimeValues)
		feature.ConfigVersion = version
	}
	if overlay != nil {
		feature.Values = mergeValues(feature.Values, overlay.Values)
		if feature.ConfigVersion == 0 {
			feature.ConfigVersion = overlay.Version
			updatedAt := overlay.UpdatedAt.UTC()
			feature.UpdatedAt = &updatedAt
		}
	}
	applyValues(feature.ConfigSchema, feature.Values)
	s.enrichDynamicOptions(ctx, &feature)
	maskSecrets(feature.ConfigSchema)
	feature.Values = valuesFromFields(feature.ConfigSchema)
	return feature
}

// enrichDynamicOptions merges live, non-static choices into a feature schema.
//
// Today only the AI account model catalogue is dynamic: the selectable set is
// owned by the AI gateway and changes as models are published. Any value that
// is already stored is always preserved so an operator never loses a selected
// model that has temporarily dropped out of the catalogue.
func (s *Service) enrichDynamicOptions(ctx context.Context, feature *domain.Feature) {
	if feature == nil || feature.ID != "ai_account" || s.modelCatalog == nil {
		return
	}
	options, err := s.modelCatalog.ModelOptions(ctx)
	if err != nil {
		return
	}
	for index := range feature.ConfigSchema {
		field := &feature.ConfigSchema[index]
		if field.Key != "default_models" {
			continue
		}
		field.Options = mergeModelOptions(field.Options, options, feature.Values)
	}
}

func mergeModelOptions(
	existing []domain.ConfigOption,
	live []ModelOption,
	values map[string]any,
) []domain.ConfigOption {
	seen := make(map[string]struct{}, len(existing)+len(live))
	result := make([]domain.ConfigOption, 0, len(existing)+len(live))
	appendOption := func(value string, label string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		if _, ok := seen[value]; ok {
			return
		}
		seen[value] = struct{}{}
		if strings.TrimSpace(label) == "" {
			label = value
		}
		result = append(result, domain.ConfigOption{Value: value, Label: label})
	}
	for _, option := range existing {
		appendOption(option.Value, option.Label)
	}
	for _, option := range live {
		appendOption(option.ID, option.Label)
	}
	if stored, ok := values["default_models"]; ok {
		if selected, ok := stringListValue(map[string]any{"models": stored}, "models"); ok {
			for _, value := range selected {
				appendOption(value, value)
			}
		}
	}
	return result
}

func hasEditableField(fields []domain.ConfigField) bool {
	for _, field := range fields {
		if field.Editable {
			return true
		}
		if field.Type == domain.ConfigFieldObject &&
			hasEditableField(field.Fields) {
			return true
		}
	}
	return false
}

func effectiveValues(fields []domain.ConfigField) map[string]any {
	values := make(map[string]any)
	for _, field := range fields {
		if field.Type == domain.ConfigFieldObject {
			values[field.Key] = effectiveValues(field.Fields)
			continue
		}
		if field.Default != nil {
			values[field.Key] = cloneValue(field.Default)
		}
	}
	return values
}

func valueMap(config *domain.FeatureConfig) map[string]any {
	if config == nil {
		return map[string]any{}
	}
	return config.Values
}

func mergeValues(maps ...map[string]any) map[string]any {
	result := make(map[string]any)
	for _, values := range maps {
		for key, value := range values {
			if nested, ok := value.(map[string]any); ok {
				existing, _ := result[key].(map[string]any)
				result[key] = mergeValues(existing, nested)
				continue
			}
			result[key] = cloneValue(value)
		}
	}
	return result
}

func applyValues(fields []domain.ConfigField, values map[string]any) {
	for index := range fields {
		value, exists := values[fields[index].Key]
		if !exists {
			continue
		}
		fields[index].Value = cloneValue(value)
		if fields[index].Type == domain.ConfigFieldObject {
			if nested, ok := value.(map[string]any); ok {
				applyValues(fields[index].Fields, nested)
			}
		}
	}
}

func maskSecrets(fields []domain.ConfigField) {
	for index := range fields {
		if fields[index].Type == domain.ConfigFieldObject {
			maskSecrets(fields[index].Fields)
			continue
		}
		if fields[index].Type != domain.ConfigFieldSecret {
			continue
		}
		configured := secretConfigured(fields[index].Value)
		fields[index].Value = map[string]any{
			"configured": configured,
			"mask":       maskSecret(fields[index].Value),
		}
	}
}

func valuesFromFields(fields []domain.ConfigField) map[string]any {
	values := make(map[string]any)
	for _, field := range fields {
		if field.Type == domain.ConfigFieldObject {
			nested := valuesFromFields(field.Fields)
			if field.Value != nil || len(nested) > 0 {
				values[field.Key] = nested
			}
			continue
		}
		if field.Value != nil {
			values[field.Key] = cloneValue(field.Value)
		}
	}
	return values
}

func secretConfigured(value any) bool {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed) != ""
	case map[string]any:
		configured, _ := typed["configured"].(bool)
		return configured
	default:
		return value != nil
	}
}

func maskSecret(value any) string {
	text, ok := value.(string)
	if !ok || strings.TrimSpace(text) == "" {
		return ""
	}
	const visible = 4
	runes := []rune(text)
	if len(runes) <= visible {
		return strings.Repeat("*", len(runes))
	}
	return strings.Repeat("*", len(runes)-visible) + string(runes[len(runes)-visible:])
}

func cloneFields(fields []domain.ConfigField) []domain.ConfigField {
	result := make([]domain.ConfigField, len(fields))
	copy(result, fields)
	for index := range result {
		result[index].Options = append([]domain.ConfigOption(nil), result[index].Options...)
		result[index].Fields = cloneFields(result[index].Fields)
		result[index].Default = cloneValue(result[index].Default)
		result[index].Value = cloneValue(result[index].Value)
	}
	return result
}

func cloneValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		result := make(map[string]any, len(typed))
		for key, nested := range typed {
			result[key] = cloneValue(nested)
		}
		return result
	case []any:
		result := make([]any, len(typed))
		for index := range typed {
			result[index] = cloneValue(typed[index])
		}
		return result
	case []string:
		return append([]string(nil), typed...)
	default:
		return typed
	}
}

func validFeatureID(value string) bool {
	if value == "" || value[0] < 'a' || value[0] > 'z' {
		return false
	}
	for index, character := range value {
		if character >= 'a' && character <= 'z' {
			continue
		}
		if character >= '0' && character <= '9' && index > 0 {
			continue
		}
		if character == '_' && index > 0 && index < len(value)-1 {
			continue
		}
		return false
	}
	return true
}
