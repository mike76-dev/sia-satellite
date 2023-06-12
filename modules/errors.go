package modules

import (
	"errors"
	"fmt"
	"strings"
)

// ComposeErrors combines several errors in one.
func ComposeErrors(errs ...error) error {
	var r []error
	for _, err := range errs {
		// Handle nil errors.
		if err == nil {
			continue
		}
		r = append(r, err)
	}
	if len(r) == 0 {
		return nil
	}
	if len(r) == 1 {
		return r[0]
	}
	var s string
	for i, err := range r {
		s = s + fmt.Sprintf("%s", err)
		if i < len(r) - 1 {
			s = s + ": "
		}
	}
	return errors.New(s)
}

// PeekErr checks if a chan error has an error waiting to be returned. If it has
// it will return that error. Otherwise it returns 'nil'.
func PeekErr(errChan <-chan error) (err error) {
	select {
	case err = <-errChan:
	default:
	}
	return
}

// ContainsError checks if one error contains the other.
func ContainsError(base, target error) bool {
	if base == nil || target == nil {
		return false
	}
	return strings.Contains(base.Error(), target.Error())
}

// AddContext wraps an error in a context.
func AddContext(err error, ctx string) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %s", ctx, err.Error())
}
