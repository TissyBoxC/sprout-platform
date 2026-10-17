package observability

import (
	"fmt"
	"log/slog"
	"reflect"
	"regexp"
	"strings"
)

const redactedValue = "[redacted]"

const (
	maxRedactionDepth        = 8
	maxRedactionCollection   = 64
	maxRedactionStructFields = 32
)

var sensitiveFields = map[string]struct{}{
	"access_token":  {},
	"api_key":       {},
	"authorization": {},
	"audio":         {},
	"body":          {},
	"child_name":    {},
	"conversation":  {},
	"cookie":        {},
	"email":         {},
	"image":         {},
	"password":      {},
	"prompt":        {},
	"refresh_token": {},
	"secret":        {},
	"token":         {},
}

var (
	bearerPattern = regexp.MustCompile(`(?i)\bBearer\s+[A-Za-z0-9._~+/=-]+`)
	emailPattern  = regexp.MustCompile(`(?i)\b[A-Z0-9._%+-]+@[A-Z0-9.-]+\.[A-Z]{2,}\b`)
)

// SanitizeFields removes sensitive values from a slog-style key/value slice.
// Unknown fields are preserved, while free-form values are masked when they
// resemble credentials or personal identifiers.
func SanitizeFields(fields []any) []any {
	sanitized := make([]any, len(fields))
	copy(sanitized, fields)

	for index := 0; index+1 < len(sanitized); index += 2 {
		key, isKey := sanitized[index].(string)
		if !isKey {
			continue
		}
		if isSensitiveField(key) {
			sanitized[index+1] = redactedValue
			continue
		}
		sanitized[index+1] = RedactValue(sanitized[index+1])
	}
	return sanitized
}

// RedactValue masks credentials and email-like values in a scalar field.
func RedactValue(value any) any {
	return redactAny(reflect.ValueOf(value), 0, make(map[reference]struct{}))
}

func redactSlogValue(value slog.Value, depth int) slog.Value {
	if depth >= maxRedactionDepth {
		return slog.StringValue(redactedValue)
	}
	if value.Kind() == slog.KindLogValuer {
		return redactSlogValue(value.Resolve(), depth+1)
	}
	switch value.Kind() {
	case slog.KindString:
		return slog.StringValue(redactString(value.String()))
	case slog.KindGroup:
		attributes := value.Group()
		if len(attributes) > maxRedactionCollection {
			return slog.StringValue(redactedValue)
		}
		sanitized := make([]slog.Attr, len(attributes))
		for index, attribute := range attributes {
			sanitized[index] = sanitizeAttribute(attribute)
		}
		return slog.GroupValue(sanitized...)
	case slog.KindAny:
		return slog.AnyValue(redactAny(
			reflect.ValueOf(value.Any()),
			depth+1,
			make(map[reference]struct{}),
		))
	default:
		return value
	}
}

func redactString(text string) string {
	text = bearerPattern.ReplaceAllString(text, redactedValue)
	return emailPattern.ReplaceAllString(text, redactedValue)
}

func isSensitiveField(key string) bool {
	normalized := strings.ToLower(strings.TrimSpace(key))
	if _, isSensitive := sensitiveFields[normalized]; isSensitive {
		return true
	}
	return strings.HasSuffix(normalized, "_token") ||
		strings.HasSuffix(normalized, "_secret") ||
		strings.HasSuffix(normalized, "_password")
}

type reference struct {
	kind    reflect.Kind
	pointer uintptr
}

func redactAny(
	value reflect.Value,
	depth int,
	seen map[reference]struct{},
) any {
	if !value.IsValid() {
		return nil
	}
	if depth >= maxRedactionDepth {
		return redactedValue
	}
	if value.Kind() == reflect.Interface {
		if value.IsNil() {
			return nil
		}
		return redactAny(value.Elem(), depth+1, seen)
	}
	if value.CanInterface() {
		if _, ok := value.Interface().(error); ok {
			return redactString(fmt.Sprint(value.Interface()))
		}
		if _, ok := value.Interface().(slog.LogValuer); ok {
			valuer := value.Interface().(slog.LogValuer)
			return redactSlogValue(valuer.LogValue(), depth+1)
		}
	}
	switch value.Kind() {
	case reflect.String:
		return redactString(value.String())
	case reflect.Bool,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		if value.CanInterface() {
			return value.Interface()
		}
		return redactedValue
	case reflect.Map:
		return redactMap(value, depth, seen)
	case reflect.Slice:
		if value.IsNil() {
			return nil
		}
		if value.Len() > maxRedactionCollection {
			return redactedValue
		}
		if value.Type().Elem().Kind() == reflect.Uint8 {
			return redactedValue
		}
		return redactSequence(value, depth, seen)
	case reflect.Array:
		return redactSequence(value, depth, seen)
	case reflect.Pointer:
		if value.IsNil() {
			return nil
		}
		if !markReference(value, seen) {
			return redactedValue
		}
		return redactAny(value.Elem(), depth+1, seen)
	case reflect.Struct:
		return redactStruct(value, depth, seen)
	default:
		return redactedValue
	}
}

func redactMap(
	value reflect.Value,
	depth int,
	seen map[reference]struct{},
) any {
	if value.IsNil() {
		return nil
	}
	if !markReference(value, seen) || value.Len() > maxRedactionCollection {
		return redactedValue
	}
	result := make(map[string]any, value.Len())
	iterator := value.MapRange()
	for iterator.Next() {
		key := redactMapKey(iterator.Key(), depth+1, seen)
		if isSensitiveField(key) {
			result[key] = redactedValue
			continue
		}
		result[key] = redactAny(iterator.Value(), depth+1, seen)
	}
	return result
}

func redactMapKey(
	value reflect.Value,
	depth int,
	seen map[reference]struct{},
) string {
	if value.Kind() == reflect.String {
		return fmt.Sprint(redactString(value.String()))
	}
	redacted := redactAny(value, depth, seen)
	return fmt.Sprint(redacted)
}

func redactSequence(
	value reflect.Value,
	depth int,
	seen map[reference]struct{},
) any {
	if value.Len() > maxRedactionCollection {
		return redactedValue
	}
	result := make([]any, value.Len())
	for index := range result {
		result[index] = redactAny(value.Index(index), depth+1, seen)
	}
	return result
}

func redactStruct(
	value reflect.Value,
	depth int,
	seen map[reference]struct{},
) any {
	result := make(map[string]any)
	for index := 0; index < value.NumField(); index++ {
		field := value.Type().Field(index)
		if field.PkgPath != "" {
			continue
		}
		if len(result) >= maxRedactionStructFields {
			return redactedValue
		}
		key := field.Name
		if tag := field.Tag.Get("json"); tag != "" {
			name := strings.Split(tag, ",")[0]
			if name == "-" {
				continue
			}
			if name != "" {
				key = name
			}
		}
		if isSensitiveField(key) {
			result[key] = redactedValue
			continue
		}
		result[key] = redactAny(value.Field(index), depth+1, seen)
	}
	return result
}

func markReference(value reflect.Value, seen map[reference]struct{}) bool {
	switch value.Kind() {
	case reflect.Map, reflect.Slice, reflect.Pointer:
	default:
		return true
	}
	if value.IsNil() {
		return true
	}
	key := reference{kind: value.Kind(), pointer: value.Pointer()}
	if _, exists := seen[key]; exists {
		return false
	}
	seen[key] = struct{}{}
	return true
}
