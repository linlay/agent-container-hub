package runtime

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

var unableToFindImagePattern = regexp.MustCompile(`(?i)unable to find image ['"]?([^'"\s]+)['"]? locally`)

type commandFailure struct {
	detail        string
	publicMessage string
	cause         error
}

func (e *commandFailure) Error() string {
	return e.detail
}

func (e *commandFailure) Unwrap() error {
	return e.cause
}

func (e *commandFailure) PublicMessage() string {
	return e.publicMessage
}

func PublicErrorMessage(err error) (string, bool) {
	var publicErr interface {
		error
		PublicMessage() string
	}
	if !errors.As(err, &publicErr) {
		return "", false
	}
	message := strings.TrimSpace(publicErr.PublicMessage())
	if message == "" {
		return "", false
	}
	return message, true
}

func newCommandFailure(binary string, args []string, result commandResult, err error, publicMessage string) error {
	return &commandFailure{
		detail:        formatCommandFailure(binary, args, result, err),
		publicMessage: strings.TrimSpace(publicMessage),
		cause:         err,
	}
}

func formatCommandFailure(binary string, args []string, result commandResult, err error) string {
	detail := strings.TrimSpace(result.stderr)
	if detail == "" {
		detail = strings.TrimSpace(result.stdout)
	}
	if detail == "" {
		return fmt.Sprintf("%s %s: %v", binary, strings.Join(args, " "), err)
	}
	return fmt.Sprintf("%s %s: %v: %s", binary, strings.Join(args, " "), err, detail)
}

func classifyCommandPublicMessage(image string, result commandResult) string {
	detail := strings.TrimSpace(result.stderr)
	if detail == "" {
		detail = strings.TrimSpace(result.stdout)
	}
	if detail == "" {
		return ""
	}
	return classifyImageNotFoundMessage(image, detail)
}

func classifyImageNotFoundMessage(image, detail string) string {
	lowerDetail := strings.ToLower(detail)
	if !strings.Contains(lowerDetail, "unable to find image") &&
		!strings.Contains(lowerDetail, "pull access denied") &&
		!strings.Contains(lowerDetail, "repository does not exist") &&
		!strings.Contains(lowerDetail, "manifest unknown") {
		return ""
	}

	image = strings.TrimSpace(image)
	if image == "" {
		matches := unableToFindImagePattern.FindStringSubmatch(detail)
		if len(matches) > 1 {
			image = strings.TrimSpace(matches[1])
		}
	}
	if image == "" {
		return "container image not found"
	}
	return fmt.Sprintf("image %q not found", image)
}
