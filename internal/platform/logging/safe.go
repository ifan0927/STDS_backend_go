package logging

import "log/slog"

// AllowlistAttrs converts request-derived fields into structured log attrs
// using only explicitly allowed keys.
func AllowlistAttrs(fields map[string]any, allowedKeys ...string) []any {
	if len(fields) == 0 || len(allowedKeys) == 0 {
		return nil
	}

	attrs := make([]any, 0, len(allowedKeys))
	for _, key := range allowedKeys {
		value, ok := fields[key]
		if !ok {
			continue
		}

		attrs = append(attrs, slog.Any(key, value))
	}

	return attrs
}
