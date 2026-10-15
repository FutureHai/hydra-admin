package observability

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"reflect"
	"strings"
	"time"
)

const Redacted = "[REDACTED]"

var sensitive = map[string]bool{
	"password": true, "passwordhash": true, "secret": true, "clientsecret": true,
	"authorization": true, "cookie": true, "setcookie": true,
	"accesstoken": true, "refreshtoken": true, "idtoken": true,
	"token": true, "pagetoken": true, "csrf": true, "code": true, "assertion": true,
}

func IsSensitiveKey(key string) bool {
	key = strings.ToLower(key)
	key = strings.ReplaceAll(key, "_", "")
	key = strings.ReplaceAll(key, "-", "")
	return sensitive[key]
}

func SanitizeString(value string) string {
	if !strings.Contains(value, "?") {
		return value
	}
	u, err := url.Parse(value)
	if err != nil {
		return Redacted
	}
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return Redacted
	}
	changed := false
	for key := range q {
		if IsSensitiveKey(key) {
			q[key] = []string{Redacted}
			changed = true
		}
	}
	if !changed {
		return value
	}
	u.RawQuery = q.Encode()
	return u.String()
}

// RedactAny recursively handles maps, slices, structs and nested values before logging.
func RedactAny(v any) any { return redactReflect(reflect.ValueOf(v), 0, map[uintptr]bool{}) }

func redactReflect(v reflect.Value, depth int, seen map[uintptr]bool) any {
	if !v.IsValid() {
		return nil
	}
	if depth > 16 {
		return "[TRUNCATED]"
	}
	if v.CanInterface() {
		if t, ok := v.Interface().(time.Time); ok {
			return t
		}
		if u, ok := v.Interface().(url.URL); ok {
			return SanitizeString(u.String())
		}
		if err, ok := v.Interface().(error); ok {
			return SanitizeString(err.Error())
		}
		if logValuer, ok := v.Interface().(slog.LogValuer); ok {
			return redactValue(logValuer.LogValue(), depth+1)
		}
	}
	switch v.Kind() {
	case reflect.Interface, reflect.Pointer:
		if v.IsNil() {
			return nil
		}
		if v.Kind() == reflect.Pointer {
			ptr := v.Pointer()
			if seen[ptr] {
				return "[CYCLE]"
			}
			seen[ptr] = true
			defer delete(seen, ptr)
		}
		return redactReflect(v.Elem(), depth+1, seen)
	case reflect.String:
		return SanitizeString(v.String())
	case reflect.Map:
		if v.IsNil() {
			return nil
		}
		if v.Type().Key().Kind() != reflect.String {
			return "[UNSUPPORTED_MAP]"
		}
		out := make(map[string]any, v.Len())
		iter := v.MapRange()
		for iter.Next() {
			key := iter.Key().String()
			if IsSensitiveKey(key) {
				out[key] = Redacted
			} else {
				out[key] = redactReflect(iter.Value(), depth+1, seen)
			}
		}
		return out
	case reflect.Slice, reflect.Array:
		if v.Kind() == reflect.Slice && v.IsNil() {
			return nil
		}
		out := make([]any, v.Len())
		for i := range out {
			out[i] = redactReflect(v.Index(i), depth+1, seen)
		}
		return out
	case reflect.Struct:
		out := map[string]any{}
		typ := v.Type()
		for i := 0; i < v.NumField(); i++ {
			field := typ.Field(i)
			if field.PkgPath != "" {
				continue
			}
			key := strings.Split(field.Tag.Get("json"), ",")[0]
			if key == "-" {
				continue
			}
			if key == "" {
				key = field.Name
			}
			if IsSensitiveKey(key) {
				out[key] = Redacted
			} else {
				out[key] = redactReflect(v.Field(i), depth+1, seen)
			}
		}
		return out
	default:
		if v.CanInterface() {
			return v.Interface()
		}
		return fmt.Sprint(v)
	}
}

func redactValue(v slog.Value, depth int) any {
	v = v.Resolve()
	if v.Kind() == slog.KindGroup {
		out := map[string]any{}
		for _, attr := range v.Group() {
			if IsSensitiveKey(attr.Key) {
				out[attr.Key] = Redacted
			} else {
				out[attr.Key] = redactValue(attr.Value, depth+1)
			}
		}
		return out
	}
	if v.Kind() == slog.KindString {
		return SanitizeString(v.String())
	}
	if v.Kind() == slog.KindAny {
		return RedactAny(v.Any())
	}
	return v.Any()
}

func redactAttr(attr slog.Attr) slog.Attr {
	if IsSensitiveKey(attr.Key) {
		return slog.String(attr.Key, Redacted)
	}
	return slog.Any(attr.Key, redactValue(attr.Value, 0))
}

type RedactingHandler struct {
	inner          slog.Handler
	sensitiveGroup bool
}

func NewRedactingHandler(inner slog.Handler) *RedactingHandler {
	return &RedactingHandler{inner: inner}
}
func (h *RedactingHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}
func (h *RedactingHandler) Handle(ctx context.Context, record slog.Record) error {
	clean := slog.NewRecord(record.Time, record.Level, SanitizeString(record.Message), record.PC)
	record.Attrs(func(attr slog.Attr) bool {
		if h.sensitiveGroup {
			clean.AddAttrs(slog.String(attr.Key, Redacted))
		} else {
			clean.AddAttrs(redactAttr(attr))
		}
		return true
	})
	return h.inner.Handle(ctx, clean)
}
func (h *RedactingHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	clean := make([]slog.Attr, len(attrs))
	for i, attr := range attrs {
		if h.sensitiveGroup {
			clean[i] = slog.String(attr.Key, Redacted)
		} else {
			clean[i] = redactAttr(attr)
		}
	}
	return &RedactingHandler{inner: h.inner.WithAttrs(clean), sensitiveGroup: h.sensitiveGroup}
}
func (h *RedactingHandler) WithGroup(name string) slog.Handler {
	return &RedactingHandler{inner: h.inner.WithGroup(name), sensitiveGroup: h.sensitiveGroup || IsSensitiveKey(name)}
}
