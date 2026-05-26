package validate

import (
	"fmt"
	"strings"
)

func Required(field, value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("validation error: %s is required", field)
	}
	return nil
}

func Positive(field string, value float64) error {
	if value < 0 {
		return fmt.Errorf("validation error: %s must be non-negative", field)
	}
	return nil
}

func OneOf(field, value string, allowed ...string) error {
	for _, a := range allowed {
		if value == a {
			return nil
		}
	}
	return fmt.Errorf("validation error: invalid %s", field)
}
