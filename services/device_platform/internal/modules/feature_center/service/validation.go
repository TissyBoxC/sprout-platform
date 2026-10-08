package service

import (
	"fmt"
	"math"
	"net/url"
	"reflect"
	"regexp"
	"strings"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/feature_center/domain"
)

var durationPattern = regexp.MustCompile(`^[1-9][0-9]*(ms|s|m|h|d)$`)

func validateObject(
	fields []domain.ConfigField,
	values map[string]any,
	immutable map[string]struct{},
	path string,
) error {
	if values == nil {
		values = map[string]any{}
	}
	allowed := make(map[string]domain.ConfigField, len(fields))
	for _, field := range fields {
		allowed[field.Key] = field
	}
	for key := range values {
		_, exists := allowed[key]
		if !exists {
			return validationError(joinField(path, key), domain.ErrUnknownConfigField)
		}
		if _, locked := immutable[key]; locked {
			return validationError(joinField(path, key), domain.ErrImmutableConfigField)
		}
	}
	for _, field := range fields {
		value, exists := values[field.Key]
		fieldPath := joinField(path, field.Key)
		if field.Required && (!exists || isEmptyValue(value)) {
			return validationError(fieldPath, domain.ErrInvalidConfig)
		}
		if !exists {
			continue
		}
		if field.Type == domain.ConfigFieldObject {
			nested, ok := asObject(value)
			if !ok {
				return validationError(fieldPath, domain.ErrInvalidConfig)
			}
			if err := validateObject(field.Fields, nested, immutable, fieldPath); err != nil {
				return err
			}
			continue
		}
		if err := validateValue(field, value); err != nil {
			return validationError(fieldPath, err)
		}
	}
	return nil
}

// validateSubmittedObject rejects fields the caller is not allowed to change.
// Registry defaults are not passed here, so a system field may be part of the
// merged document without blocking edits to other fields.
func validateSubmittedObject(
	fields []domain.ConfigField,
	values map[string]any,
	path string,
) error {
	if len(values) == 0 {
		return nil
	}
	allowed := make(map[string]domain.ConfigField, len(fields))
	for _, field := range fields {
		allowed[field.Key] = field
	}
	for key, value := range values {
		field, exists := allowed[key]
		if !exists {
			return validationError(joinField(path, key), domain.ErrUnknownConfigField)
		}
		if !field.Editable {
			return validationError(joinField(path, key), domain.ErrImmutableConfigField)
		}
		if field.Type != domain.ConfigFieldObject {
			continue
		}
		nested, ok := asObject(value)
		if !ok {
			return validationError(joinField(path, key), domain.ErrInvalidConfig)
		}
		if err := validateSubmittedObject(field.Fields, nested, joinField(path, key)); err != nil {
			return err
		}
	}
	return nil
}

func validateValue(field domain.ConfigField, value any) error {
	switch field.Type {
	case domain.ConfigFieldBoolean:
		if _, ok := value.(bool); !ok {
			return domain.ErrInvalidConfig
		}
	case domain.ConfigFieldInteger:
		number, ok := numericValue(value)
		if !ok || math.Trunc(number) != number {
			return domain.ErrInvalidConfig
		}
		if err := validateRange(field, number); err != nil {
			return err
		}
	case domain.ConfigFieldNumber:
		number, ok := numericValue(value)
		if !ok {
			return domain.ErrInvalidConfig
		}
		if err := validateRange(field, number); err != nil {
			return err
		}
	case domain.ConfigFieldString, domain.ConfigFieldSecret:
		if _, ok := value.(string); !ok {
			return domain.ErrInvalidConfig
		}
	case domain.ConfigFieldSelect:
		text, ok := value.(string)
		if !ok || !optionExists(field.Options, text) {
			return domain.ErrInvalidConfig
		}
	case domain.ConfigFieldMultiselect:
		values, ok := stringSlice(value)
		if !ok {
			return domain.ErrInvalidConfig
		}
		if field.AllowCustom {
			for _, item := range values {
				if len(item) > 128 {
					return domain.ErrInvalidConfig
				}
			}
			return nil
		}
		for _, item := range values {
			if !optionExists(field.Options, item) {
				return domain.ErrInvalidConfig
			}
		}
	case domain.ConfigFieldDuration:
		text, ok := value.(string)
		if !ok || !durationPattern.MatchString(text) {
			return domain.ErrInvalidConfig
		}
		if _, err := time.ParseDuration(normalizeDuration(text)); err != nil {
			return domain.ErrInvalidConfig
		}
	case domain.ConfigFieldURL:
		text, ok := value.(string)
		if !ok {
			return domain.ErrInvalidConfig
		}
		parsed, err := url.ParseRequestURI(text)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			return domain.ErrInvalidConfig
		}
	default:
		return domain.ErrInvalidConfig
	}
	return nil
}

func validateRange(field domain.ConfigField, value float64) error {
	if field.Min != nil && value < *field.Min {
		return domain.ErrInvalidConfig
	}
	if field.Max != nil && value > *field.Max {
		return domain.ErrInvalidConfig
	}
	return nil
}

func numericValue(value any) (float64, bool) {
	switch typed := value.(type) {
	case int:
		return float64(typed), true
	case int32:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case float32:
		return float64(typed), true
	case float64:
		return typed, true
	default:
		return 0, false
	}
}

func stringSlice(value any) ([]string, bool) {
	switch typed := value.(type) {
	case []string:
		return typed, true
	case []any:
		result := make([]string, 0, len(typed))
		for _, item := range typed {
			text, ok := item.(string)
			if !ok {
				return nil, false
			}
			result = append(result, text)
		}
		return result, true
	default:
		return nil, false
	}
}

func optionExists(options []domain.ConfigOption, value string) bool {
	for _, option := range options {
		if option.Value == value {
			return true
		}
	}
	return false
}

func asObject(value any) (map[string]any, bool) {
	switch typed := value.(type) {
	case map[string]any:
		return typed, true
	case map[string]string:
		result := make(map[string]any, len(typed))
		for key, item := range typed {
			result[key] = item
		}
		return result, true
	default:
		reflected := reflect.ValueOf(value)
		if reflected.IsValid() && reflected.Kind() == reflect.Map &&
			reflected.Type().Key().Kind() == reflect.String {
			result := make(map[string]any, reflected.Len())
			iter := reflected.MapRange()
			for iter.Next() {
				result[iter.Key().String()] = iter.Value().Interface()
			}
			return result, true
		}
		return nil, false
	}
}

func isEmptyValue(value any) bool {
	if value == nil {
		return true
	}
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed) == ""
	case []string:
		return len(typed) == 0
	case []any:
		return len(typed) == 0
	case map[string]any:
		return len(typed) == 0
	default:
		return false
	}
}

func joinField(path string, key string) string {
	if path == "" {
		return key
	}
	return fmt.Sprintf("%s.%s", path, key)
}

func validationError(field string, err error) error {
	return &domain.ConfigValidationError{Field: field, Err: err}
}

func normalizeDuration(value string) string {
	if strings.HasSuffix(value, "d") {
		days := strings.TrimSuffix(value, "d")
		return days + "h"
	}
	return value
}
