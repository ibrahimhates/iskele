package middleware

import "github.com/ibrahimhates/iskele/internal/store"

// Permission is one capability an endpoint requires.
type Permission string

// The permission set. Endpoints declare what they need; roles are granted a
// set of permissions. Adding an endpoint means picking a permission, not
// editing a role check in three places.
const (
	// PermRead covers every listing, inspection, log and stat.
	PermRead Permission = "read"
	// PermOperate covers container lifecycle, image pulls, stack up/down.
	PermOperate Permission = "operate"
	// PermCreate covers creating containers, volumes and networks.
	PermCreate Permission = "create"
	// PermDelete covers removing containers, volumes and networks.
	PermDelete Permission = "delete"
	// PermBuild covers Dockerfile builds, which read arbitrary host paths.
	PermBuild Permission = "build"
	// PermPrune covers bulk destructive cleanup.
	PermPrune Permission = "prune"
	// PermPrivileged covers privileged containers, capabilities, devices,
	// security-opt and host bind mounts — anything equivalent to host root.
	PermPrivileged Permission = "privileged"
	// PermAdmin covers users, settings, registries and the audit log.
	PermAdmin Permission = "admin"
)

// rolePermissions is the authoritative RBAC matrix (PLAN §7).
//
// viewer reads. operator additionally runs and manages workloads. admin
// additionally builds, prunes, uses privileged options and administers the
// installation.
var rolePermissions = map[store.Role]map[Permission]bool{
	store.RoleViewer: {
		PermRead: true,
	},
	store.RoleOperator: {
		PermRead:    true,
		PermOperate: true,
		PermCreate:  true,
		PermDelete:  true,
	},
	store.RoleAdmin: {
		PermRead:       true,
		PermOperate:    true,
		PermCreate:     true,
		PermDelete:     true,
		PermBuild:      true,
		PermPrune:      true,
		PermPrivileged: true,
		PermAdmin:      true,
	},
}

// RoleHas reports whether a role carries a permission.
//
// An unknown role has no permissions: a corrupted or future role value must
// fail closed.
func RoleHas(role store.Role, perm Permission) bool {
	perms, ok := rolePermissions[role]
	if !ok {
		return false
	}
	return perms[perm]
}

// PermissionsOf returns the permissions a role carries, for the UI to hide
// controls the caller cannot use.
func PermissionsOf(role store.Role) []Permission {
	perms := rolePermissions[role]
	out := make([]Permission, 0, len(perms))
	// Iterate the declared order rather than the map, so the result is stable.
	for _, p := range allPermissions {
		if perms[p] {
			out = append(out, p)
		}
	}
	return out
}

// allPermissions is the declaration order used for stable output.
var allPermissions = []Permission{
	PermRead, PermOperate, PermCreate, PermDelete,
	PermBuild, PermPrune, PermPrivileged, PermAdmin,
}
