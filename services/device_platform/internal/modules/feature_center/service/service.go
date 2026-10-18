// Package service owns feature registry projection, configuration validation,
// and health checks.
package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

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

// HealthProbeChecker is implemented by probes that can declare whether they
// have a real runtime signal. A probe that explicitly reports false fails
// coverage validation so a temporary placeholder cannot satisfy production
// assembly.
type HealthProbeChecker interface {
	HealthProbeReady() bool
}

// Check implements HealthProbe.
func (probe HealthProbeFunc) Check(
	ctx context.Context,
	featureID string,
) (domain.HealthStatus, error) {
	return probe(ctx, featureID)
}

// Options contains feature-center service dependencies.
type Options struct {
	Repository     repository.Repository
	Definitions    []Definition
	HealthProbes   map[string]HealthProbe
	Clock          clock.Clock
	Audit          AuditRecorder
	SchedulerLease repository.SchedulerLease
}

// AuditRecorder writes safe, attributable feature-center operations.
type AuditRecorder interface {
	RecordAudit(
		ctx context.Context,
		actorAccountID string,
		targetAccountID string,
		action string,
		detail map[string]any,
	) error
}

// AuditWriteError indicates that the primary write completed but the audit
// event could not be persisted. Operators must retry the audit path.
type AuditWriteError struct {
	Err                   error
	PrimaryWriteCompleted bool
}

// FeatureAuditFailure captures a health snapshot whose primary persistence
// succeeded but whose audit event was rejected by the audit store.
type FeatureAuditFailure struct {
	Snapshot *domain.HealthSnapshot
	Err      error
}

// Error implements error.
func (e *AuditWriteError) Error() string {
	if e == nil || e.Err == nil {
		return "feature center audit write failed"
	}
	return "feature center audit write failed: " + e.Err.Error()
}

// Unwrap exposes the audit dependency error.
func (e *AuditWriteError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
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
	metrics         MetricsReader
	audit           AuditRecorder
	schedulerLease  repository.SchedulerLease
	auditFailuresMu sync.Mutex
	auditFailures   []FeatureAuditFailure
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
		probe := probes[definition.Feature.ID]
		if probe == nil {
			missing = append(missing, definition.Feature.ID)
			continue
		}
		if checker, ok := probe.(HealthProbeChecker); ok && !checker.HealthProbeReady() {
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
		repository:     options.Repository,
		definitions:    append([]Definition(nil), definitions...),
		byID:           byID,
		probes:         options.HealthProbes,
		clock:          timeSource,
		audit:          options.Audit,
		schedulerLease: options.SchedulerLease,
	}, nil
}

// List returns every registered feature with persisted values merged in.
func (s *Service) List(ctx context.Context) ([]domain.Feature, error) {
	overlays, err := s.repository.List(ctx)
	if err != nil {
		return nil, err
	}
	healthSnapshots, err := s.repository.ListHealth(ctx)
	if err != nil {
		return nil, err
	}
	byID := make(map[string]domain.FeatureConfig, len(overlays))
	for _, overlay := range overlays {
		byID[overlay.FeatureID] = overlay
	}
	healthByID := make(map[string]domain.HealthSnapshot, len(healthSnapshots))
	for _, snapshot := range healthSnapshots {
		healthByID[snapshot.FeatureID] = snapshot
	}
	result := make([]domain.Feature, 0, len(s.definitions))
	for _, definition := range s.definitions {
		var overlay *domain.FeatureConfig
		if current, ok := byID[definition.Feature.ID]; ok {
			copyConfig := current
			overlay = &copyConfig
		}
		snapshot, hasHealth := healthByID[definition.Feature.ID]
		result = append(result, s.project(
			ctx,
			definition,
			overlay,
			healthPointer(snapshot, hasHealth),
		))
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
	health, healthErr := s.loadHealth(ctx, featureID)
	if healthErr != nil {
		return nil, healthErr
	}
	feature := s.project(ctx, definition, overlay, health)
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
		current, currentErr := s.repository.Get(ctx, featureID)
		if currentErr != nil &&
			!errors.Is(currentErr, domain.ErrFeatureConfigNotFound) {
			return nil, currentErr
		}
		previousVersion := valueVersion(current)
		auditErr := s.recordConfigAudit(
			ctx,
			featureID,
			strings.TrimSpace(actorID),
			previousVersion,
			previousVersion+1,
		)
		feature, readErr := s.Get(ctx, featureID)
		if readErr != nil {
			return nil, errors.Join(auditErr, readErr)
		}
		return feature, auditErr
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
	saved, err := s.repository.Save(
		ctx,
		featureID,
		merged,
		strings.TrimSpace(actorID),
		expectedVersion,
	)
	if err != nil {
		return nil, err
	}
	auditErr := s.recordConfigAudit(
		ctx,
		featureID,
		strings.TrimSpace(actorID),
		valueVersion(current),
		saved.Version,
	)
	health, err := s.loadHealth(ctx, featureID)
	if err != nil {
		return nil, errors.Join(auditErr, err)
	}
	feature := s.project(ctx, definition, saved, health)
	return &feature, auditErr
}

// CheckHealth runs the configured probe and returns the resulting feature.
//
// The result is persisted before it becomes visible so a process restart does
// not erase the last observed state. A probe or persistence failure returns an
// error and leaves the previous snapshot untouched.
func (s *Service) CheckHealth(
	ctx context.Context,
	featureID string,
) (*domain.Feature, error) {
	return s.checkHealth(
		ctx,
		featureID,
		domain.HealthTriggerManual,
		"",
		false,
	)
}

// CheckHealthAs runs a manual health check attributed to one administrator.
// The actor is persisted with the health audit entry so the operation remains
// attributable after the request context is gone.
func (s *Service) CheckHealthAs(
	ctx context.Context,
	featureID string,
	actorID string,
) (*domain.Feature, error) {
	return s.checkHealth(
		ctx,
		featureID,
		domain.HealthTriggerManual,
		strings.TrimSpace(actorID),
		false,
	)
}

// CheckScheduledHealth runs one probe without an administrator actor and
// records it with the schedule trigger.
func (s *Service) CheckScheduledHealth(
	ctx context.Context,
	featureID string,
) (*domain.Feature, error) {
	return s.checkHealth(
		ctx,
		featureID,
		domain.HealthTriggerSchedule,
		"",
		true,
	)
}

// CheckAllScheduledHealth probes every active feature once. Each feature is
// independent so one unavailable dependency does not suppress later results.
// The returned error describes the first failure after all probes were tried.
func (s *Service) CheckAllScheduledHealth(
	ctx context.Context,
) ([]domain.Feature, error) {
	if s == nil {
		return nil, errors.New("feature center service is required")
	}
	if s.schedulerLease == nil {
		return nil, domain.ErrSchedulerLeaseRequired
	}
	var results []domain.Feature
	var cycleFailure *scheduledCycleError
	leaseErr := s.schedulerLease.WithLeaderLease(ctx, func(leaseCtx context.Context) error {
		var cycleErr error
		results, cycleErr = s.checkAllScheduledHealth(leaseCtx)
		if cycleErr != nil && ctx.Err() == nil {
			cycleFailure = &scheduledCycleError{Err: cycleErr}
			return cycleFailure
		}
		return nil
	})
	if leaseErr != nil {
		if errors.Is(leaseErr, domain.ErrSchedulerLeaseUnavailable) ||
			errors.Is(leaseErr, domain.ErrSchedulerLeaseRequired) {
			return nil, leaseErr
		}

		if errors.As(leaseErr, &cycleFailure) {
			var releaseFailure *repository.SchedulerLeaseReleaseError
			if errors.As(leaseErr, &releaseFailure) {
				return results, leaseErr
			}
			return results, cycleFailure.Err
		}
		return results, fmt.Errorf("run feature health scheduler lease: %w", leaseErr)
	}
	return results, nil
}

// scheduledCycleError marks aggregate probe failures as the non-lease cause of
// a WithLeaderLease error. It keeps lease release failures distinguishable from
// expected feature health failures.
type scheduledCycleError struct {
	Err error
}

func (err *scheduledCycleError) Error() string {
	if err == nil || err.Err == nil {
		return "scheduled feature health cycle failed"
	}
	return err.Err.Error()
}

func (err *scheduledCycleError) Unwrap() error {
	if err == nil {
		return nil
	}
	return err.Err
}

func (s *Service) checkAllScheduledHealth(
	ctx context.Context,
) ([]domain.Feature, error) {
	results := make([]domain.Feature, 0, len(s.definitions))
	var firstErr error
	for _, definition := range s.definitions {
		if definition.Feature.Status != domain.FeatureStatusActive {
			continue
		}
		feature, err := s.CheckScheduledHealth(ctx, definition.Feature.ID)
		if err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf(
					"check scheduled health for %s: %w",
					definition.Feature.ID,
					err,
				)
			}
			continue
		}
		results = append(results, *feature)
	}
	return results, firstErr
}

// RunHealthScheduler executes one complete scheduled cycle immediately and
// then waits for the supplied interval. Individual probe and audit failures
// are recorded without preventing later features from being checked.
func (s *Service) RunHealthScheduler(
	ctx context.Context,
	interval time.Duration,
) error {
	if s == nil {
		return errors.New("feature center service is required")
	}
	if interval <= 0 {
		return errors.New("feature health interval must be positive")
	}
	if s.schedulerLease == nil {
		return domain.ErrSchedulerLeaseRequired
	}
	for {
		_, _ = s.CheckAllScheduledHealth(ctx)
		if err := ctx.Err(); err != nil {
			return err
		}
		// A cycle that overruns its interval must not be followed immediately by
		// another one. Start the next fixed delay only after this cycle finishes.
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return ctx.Err()
		case <-timer.C:
		}
	}
}

// DrainAuditFailures returns and clears health snapshots whose primary write
// succeeded but whose audit event could not be persisted. This lets a
// background scheduler surface the degraded security trail without converting
// a successful health observation into a fabricated success or discarding it.
func (s *Service) DrainAuditFailures() []FeatureAuditFailure {
	if s == nil {
		return nil
	}
	s.auditFailuresMu.Lock()
	defer s.auditFailuresMu.Unlock()
	if len(s.auditFailures) == 0 {
		return nil
	}
	failures := append([]FeatureAuditFailure(nil), s.auditFailures...)
	s.auditFailures = nil
	return failures
}

func (s *Service) checkHealth(
	ctx context.Context,
	featureID string,
	trigger string,
	actorID string,
	allowAuditFailure bool,
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
	checkedAt := s.clock.Now().UTC()
	elapsed := int(checkedAt.Sub(startedAt).Milliseconds())
	if elapsed < 0 {
		elapsed = 0
	}
	snapshot := &domain.HealthSnapshot{
		FeatureID: featureID,
		Status:    status,
		CheckedAt: checkedAt,
		Message:   healthMessage(status),
		LatencyMS: elapsed,
		Trigger:   trigger,
	}
	if err := s.repository.SaveHealth(ctx, snapshot); err != nil {
		return nil, err
	}
	auditErr := s.recordHealthAudit(ctx, snapshot, actorID)
	if auditErr != nil && allowAuditFailure {
		s.auditFailuresMu.Lock()
		s.auditFailures = append(s.auditFailures, FeatureAuditFailure{
			Snapshot: snapshot,
			Err:      auditErr,
		})
		s.auditFailuresMu.Unlock()
	}
	feature, projectionErr := s.featureAfterHealthWrite(ctx, definition, snapshot)
	if projectionErr != nil {
		return nil, errors.Join(auditErr, projectionErr)
	}
	if allowAuditFailure {
		return feature, nil
	}
	return feature, auditErr
}

func (s *Service) featureAfterHealthWrite(
	ctx context.Context,
	definition Definition,
	snapshot *domain.HealthSnapshot,
) (*domain.Feature, error) {
	overlay, err := s.repository.Get(ctx, definition.Feature.ID)
	if errors.Is(err, domain.ErrFeatureConfigNotFound) {
		overlay = nil
	} else if err != nil {
		return nil, err
	}
	feature := s.project(ctx, definition, overlay, snapshot)
	return &feature, nil
}

func (s *Service) loadHealth(
	ctx context.Context,
	featureID string,
) (*domain.HealthSnapshot, error) {
	snapshot, err := s.repository.GetHealth(ctx, featureID)
	if errors.Is(err, domain.ErrFeatureConfigNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return snapshot, nil
}

func healthPointer(
	snapshot domain.HealthSnapshot,
	ok bool,
) *domain.HealthSnapshot {
	if !ok {
		return nil
	}
	return &snapshot
}

func (s *Service) recordConfigAudit(
	ctx context.Context,
	featureID string,
	actorID string,
	previousVersion int64,
	currentVersion int64,
) error {
	if s.audit == nil {
		return nil
	}
	if err := s.audit.RecordAudit(
		ctx,
		actorID,
		"",
		"admin.feature_center.config.updated",
		map[string]any{
			"feature_id":       featureID,
			"previous_version": previousVersion,
			"version":          currentVersion,
		},
	); err != nil {
		return &AuditWriteError{Err: err, PrimaryWriteCompleted: true}
	}
	return nil
}

func (s *Service) recordHealthAudit(
	ctx context.Context,
	snapshot *domain.HealthSnapshot,
	actorID string,
) error {
	if s.audit == nil || snapshot == nil {
		return nil
	}
	if err := s.audit.RecordAudit(
		ctx,
		strings.TrimSpace(actorID),
		"",
		featureHealthAuditAction(snapshot.Trigger),
		map[string]any{
			"feature_id": snapshot.FeatureID,
			"status":     string(snapshot.Status),
			"latency_ms": snapshot.LatencyMS,
			"trigger":    snapshot.Trigger,
			"checked_at": snapshot.CheckedAt.UTC(),
			"message":    snapshot.Message,
		},
	); err != nil {
		return &AuditWriteError{Err: err, PrimaryWriteCompleted: true}
	}
	return nil
}

func featureHealthAuditAction(trigger string) string {
	if trigger == domain.HealthTriggerSchedule {
		return "system.feature_center.health.checked"
	}
	return "admin.feature_center.health.checked"
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
	health *domain.HealthSnapshot,
) domain.Feature {
	feature := definition.Feature
	feature.ConfigSchema = cloneFields(feature.ConfigSchema)
	feature.ReadOnlyMetrics = cloneMetrics(feature.ReadOnlyMetrics)
	feature.Values = effectiveValues(feature.ConfigSchema)
	feature.ConfigVersion = 0
	feature.UpdatedAt = nil
	if health != nil && health.Status.Valid() {
		feature.Health = health.Status
		latency := health.LatencyMS
		feature.HealthDetail = &domain.HealthDetail{
			Status:    health.Status,
			CheckedAt: health.CheckedAt.UTC(),
			Message:   health.Message,
			LatencyMS: &latency,
		}
	}
	if overlay != nil {
		feature.Values = mergeValues(feature.Values, overlay.Values)
		if feature.ConfigVersion == 0 {
			feature.ConfigVersion = overlay.Version
			updatedAt := overlay.UpdatedAt.UTC()
			feature.UpdatedAt = &updatedAt
		}
	}
	// Runtime-backed fields win over any legacy overlay. Otherwise an old
	// feature-center row could mask the configuration that services actually
	// consume and make a saved change appear to take effect when it did not.
	if runtimeValues, version, err := s.runtimeValues(ctx, feature.ID); err == nil &&
		len(runtimeValues) > 0 {
		feature.Values = mergeValues(feature.Values, runtimeValues)
		feature.ConfigVersion = version
	}
	applyValues(feature.ConfigSchema, feature.Values)
	s.enrichDynamicOptions(ctx, &feature)
	s.enrichMetrics(ctx, &feature)
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

func valueVersion(config *domain.FeatureConfig) int64 {
	if config == nil {
		return 0
	}
	return config.Version
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

// cloneMetrics copies the registry metric slice so a live value projected for
// one caller can never leak into the shared registry or a concurrent request.
func cloneMetrics(metrics []domain.Metric) []domain.Metric {
	result := make([]domain.Metric, len(metrics))
	copy(result, metrics)
	for index := range result {
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
