package auth

import "strings"

// Grant is one per-resource capability grant: holder may exercise
// Capability on (ResourceType, ResourceID). It mirrors one row of the
// resource_grants table seeded in db/migrations/008_identity.sql.
type Grant struct {
	HolderID     string
	Capability   string
	ResourceType string
	ResourceID   string
}

// Policy is the in-memory capability checker over role bindings (user_roles
// plus role_capabilities) and per-resource grants (resource_grants). The
// system actor always holds the write capability, matching its admin role
// binding seeded in the identity migration.
type Policy struct {
	// RoleCapabilities maps role name to the capabilities it confers.
	RoleCapabilities map[string][]string
	// Bindings maps actor ID to its bound role names.
	Bindings map[string][]string
	// Grants lists per-resource grants by holder actor ID.
	Grants []Grant
}

// Can reports whether holder may exercise capability. An empty resourceType
// checks the global (non-resource-scoped) capability via role bindings, with
// the system actor shortcut; a non-empty resourceType additionally consults
// per-resource grants. Unknown actors and empty capabilities never pass.
func (p Policy) Can(holderID, capability, resourceType, resourceID string) bool {
	holderID = strings.TrimSpace(holderID)
	capability = strings.TrimSpace(capability)
	if holderID == "" || capability == "" {
		return false
	}
	if holderID == SystemActorID && capability == WriteCapability {
		return true
	}
	for _, role := range p.Bindings[holderID] {
		for _, granted := range p.RoleCapabilities[strings.TrimSpace(role)] {
			if strings.TrimSpace(granted) == capability {
				return true
			}
		}
	}
	if strings.TrimSpace(resourceType) == "" {
		return false
	}
	for _, grant := range p.Grants {
		if strings.TrimSpace(grant.HolderID) == holderID &&
			strings.TrimSpace(grant.Capability) == capability &&
			strings.TrimSpace(grant.ResourceType) == strings.TrimSpace(resourceType) &&
			strings.TrimSpace(grant.ResourceID) == strings.TrimSpace(resourceID) {
			return true
		}
	}
	return false
}

// DefaultPolicy mirrors the migration seeds: admin and editor hold write,
// the system actor is bound to admin.
func DefaultPolicy() Policy {
	return Policy{
		RoleCapabilities: map[string][]string{
			"admin":  {WriteCapability},
			"editor": {WriteCapability},
			"viewer": {},
		},
		Bindings: map[string][]string{
			SystemActorID: {"admin"},
		},
	}
}
