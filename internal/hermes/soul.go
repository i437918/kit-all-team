package hermes

import (
	_ "embed"

	"github.com/mi1man-cmd/kit-all-team/internal/domain"
)

var (
	//go:embed templates/soul/analyst.md
	analystSoul []byte
	//go:embed templates/soul/developer.md
	developerSoul []byte
	//go:embed templates/soul/architect.md
	architectSoul []byte
)

// SoulForRole returns the immutable Team Kit persona template for role.
func SoulForRole(role domain.Role) ([]byte, error) {
	var template []byte
	switch role {
	case domain.RoleAnalyst:
		template = analystSoul
	case domain.RoleDeveloper:
		template = developerSoul
	case domain.RoleArchitect:
		template = architectSoul
	default:
		return nil, domain.NewValidationError(domain.RoleUnknown, "role", string(role))
	}
	return append([]byte(nil), template...), nil
}
