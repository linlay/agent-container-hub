package util

import (
	"fmt"
	"regexp"
	"unicode"
)

var envKeyPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func ValidateEnvMap(values map[string]string, kind string) error {
	for key, value := range values {
		if !envKeyPattern.MatchString(key) {
			return fmt.Errorf("%s key %q must match %s", kind, key, envKeyPattern.String())
		}
		if containsControlChars(value) {
			return fmt.Errorf("%s value for %q contains control characters", kind, key)
		}
	}
	return nil
}

func containsControlChars(value string) bool {
	for _, r := range value {
		if unicode.IsControl(r) {
			return true
		}
	}
	return false
}
