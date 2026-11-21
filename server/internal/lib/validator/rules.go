package validator

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

// rule reports a failure message; ok=false means validation failed. data
// always comes from JSON decoding, so it is never a pointer.
type rule func(key string, data interface{}) (string, bool)

func (v *FieldValidator) Required() *FieldValidator {
	v.registerRule(func(key string, data interface{}) (string, bool) {
		if data == nil || data == "" {
			return fmt.Sprintf("%s is required", key), false
		}
		return "", true
	})
	return v
}

func (v *FieldValidator) String() *FieldValidator {
	v.registerRule(func(key string, data interface{}) (string, bool) {
		if data == nil || data == "" {
			return "", true
		}
		if _, ok := data.(string); !ok {
			return fmt.Sprintf("%s must be a string", key), false
		}
		return "", true
	})
	return v
}

// MaxLen reports a failure when the value is a string longer than n
// characters.
func (v *FieldValidator) MaxLen(n int) *FieldValidator {
	v.registerRule(func(key string, data interface{}) (string, bool) {
		if data == nil || data == "" {
			return "", true
		}
		s, ok := data.(string)
		if !ok {
			return "", true
		}
		if utf8.RuneCountInString(s) > n {
			return fmt.Sprintf("%s must be at most %d characters", key, n), false
		}
		return "", true
	})
	return v
}

func (v *FieldValidator) Regex(pattern string) *FieldValidator {
	re, err := regexp.Compile(pattern)
	if err != nil {
		// A broken pattern is a programming error; surface it as a failed
		// validation rather than panicking mid-request.
		v.registerRule(func(key string, _ interface{}) (string, bool) {
			return fmt.Sprintf("%s has an invalid pattern", key), false
		})
		return v
	}
	v.registerRule(func(key string, data interface{}) (string, bool) {
		if data == nil || data == "" {
			return "", true
		}
		s, ok := data.(string)
		if !ok {
			return "", true
		}
		if !re.MatchString(s) {
			return fmt.Sprintf("%s has an invalid format", key), false
		}
		return "", true
	})
	return v
}

// UUID reports a failure when the value is not a canonical UUID v7/v4 string.
func (v *FieldValidator) UUID() *FieldValidator {
	v.registerRule(func(key string, data interface{}) (string, bool) {
		if data == nil || data == "" {
			return "", true
		}
		s, ok := data.(string)
		if !ok {
			return fmt.Sprintf("%s must be a string", key), false
		}
		if !uuidRe.MatchString(strings.TrimSpace(s)) {
			return fmt.Sprintf("%s must be a uuid", key), false
		}
		return "", true
	})
	return v
}

// Integer reports a failure when the value is not a whole number. JSON numbers
// decode as float64, so this accepts anything with a zero fractional part.
func (v *FieldValidator) Integer() *FieldValidator {
	v.registerRule(func(key string, data interface{}) (string, bool) {
		if data == nil || data == "" {
			return "", true
		}
		switch n := data.(type) {
		case float64:
			if n != float64(int64(n)) {
				return fmt.Sprintf("%s must be an integer", key), false
			}
		case int, int64:
			// already an integer
		default:
			return fmt.Sprintf("%s must be an integer", key), false
		}
		return "", true
	})
	return v
}

// Min reports a failure when the numeric value is below n.
func (v *FieldValidator) Min(n int64) *FieldValidator {
	v.registerRule(func(key string, data interface{}) (string, bool) {
		if data == nil || data == "" {
			return "", true
		}
		val, ok := toInt(data)
		if !ok {
			return "", true
		}
		if val < n {
			return fmt.Sprintf("%s must be at least %d", key, n), false
		}
		return "", true
	})
	return v
}

var uuidRe = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

func toInt(data interface{}) (int64, bool) {
	switch n := data.(type) {
	case float64:
		if n != float64(int64(n)) {
			return 0, false
		}
		return int64(n), true
	case int:
		return int64(n), true
	case int64:
		return n, true
	case string:
		i, err := strconv.ParseInt(strings.TrimSpace(n), 10, 64)
		if err != nil {
			return 0, false
		}
		return i, true
	default:
		return 0, false
	}
}
