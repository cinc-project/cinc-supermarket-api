package supermarket

import (
	"fmt"
	"strings"
)

// requireArg rejects a blank path argument before it can be interpolated
// into a request URL.
//
// A blank name or version does not yield a harmless 404 — it addresses the
// collection endpoint. GET /api/v1/cookbooks/ returns the cookbook *list*,
// which decodes into a zero-valued Cookbook with a nil error, so the caller
// silently receives an empty record rather than a failure. The signed DELETE
// variants are worse: DELETE /api/v1/cookbooks/ is a destructive call against
// an endpoint the caller never meant to address.
//
// Whitespace counts as blank; " " would otherwise be sent as %20.
func requireArg(name, value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("supermarket: %s must not be empty: %w", name, ErrInvalidArgument)
	}
	return nil
}
