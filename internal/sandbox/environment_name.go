package sandbox

import (
	"fmt"
	"regexp"
	"strings"
)

var validEnvironmentName = regexp.MustCompile(`^[a-z0-9][a-z0-9_.-]{0,127}$`)

func validateEnvironmentName(name string) error {
	name = strings.TrimSpace(name)
	if !validEnvironmentName.MatchString(name) {
		return fmt.Errorf("%w: environment name must match %s", ErrValidation, validEnvironmentName.String())
	}
	return nil
}
