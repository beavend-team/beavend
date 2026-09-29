package auth

import "fmt"

// Role represents an administrative user role.
type Role string

const (
	RoleViewer Role = "viewer"
	RoleEditor Role = "editor"
	RoleOwner  Role = "owner"
)

// Level returns the numeric privilege level of the role.
func (r Role) Level() int {
	switch r {
	case RoleOwner:
		return 3
	case RoleEditor:
		return 2
	case RoleViewer:
		return 1
	default:
		return 0
	}
}

// Can checks if role r has sufficient privileges to perform an action requiring minRole.
func (r Role) Can(minRole Role) bool {
	return r.Level() >= minRole.Level()
}

// ValidateRole validates that the role string is a recognized admin role.
func ValidateRole(s string) (Role, error) {
	r := Role(s)
	switch r {
	case RoleOwner, RoleEditor, RoleViewer:
		return r, nil
	default:
		return "", fmt.Errorf("invalid admin role: %q (must be 'owner', 'editor', or 'viewer')", s)
	}
}
