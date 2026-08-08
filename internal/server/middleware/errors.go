package middleware

import (
	"errors"
	"fmt"

	"github.com/ibrahimhates/iskele/internal/store"
)

// ErrUnauthenticated is returned when a request carries no valid credential.
var ErrUnauthenticated = errors.New("authentication required")

// PermissionError reports that an authenticated caller's role is insufficient.
//
// The message names the role and the missing permission: the caller already
// knows their own role, so this is not an information leak, and it saves an
// operator from guessing why an action is refused.
type PermissionError struct {
	Role       store.Role
	Permission Permission
}

func (e *PermissionError) Error() string {
	return fmt.Sprintf("role %q may not %s", e.Role, e.Permission)
}
