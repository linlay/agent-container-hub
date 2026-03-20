package sandbox

import (
	"fmt"
	"strings"

	"agent-container-hub/internal/model"
)

func validateEnvironmentName(name string) error {
	name = strings.TrimSpace(name)
	if !model.ValidEnvironmentName.MatchString(name) {
		return fmt.Errorf("%w: environment name must match %s", ErrValidation, model.ValidEnvironmentName.String())
	}
	return nil
}
