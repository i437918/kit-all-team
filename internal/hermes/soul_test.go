package hermes

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/mi1man-cmd/kit-all-team/internal/domain"
)

func TestSoulForRole_ReturnsDistinctRoleTemplates(t *testing.T) {
	tests := map[domain.Role]string{
		domain.RoleAnalyst:   "# Роль: Аналитик 1С",
		domain.RoleDeveloper: "# Роль: Программист 1С",
		domain.RoleArchitect: "# Роль: Архитектор 1С",
	}
	var bodies [][]byte
	for role, title := range tests {
		t.Run(string(role), func(t *testing.T) {
			body, err := SoulForRole(role)
			if err != nil {
				t.Fatalf("SoulForRole() error = %v", err)
			}
			if !strings.Contains(string(body), title) {
				t.Fatalf("SOUL.md does not contain %q: %q", title, body)
			}
			if !bytes.HasSuffix(body, []byte("\n")) {
				t.Fatalf("SOUL.md must end with LF: %q", body)
			}
			bodies = append(bodies, body)
		})
	}
	for left := range bodies {
		for right := left + 1; right < len(bodies); right++ {
			if bytes.Equal(bodies[left], bodies[right]) {
				t.Fatal("role templates must be distinct")
			}
		}
	}
}

func TestSoulForRole_RejectsUnknownRole(t *testing.T) {
	_, err := SoulForRole(domain.Role("unknown"))
	if err == nil {
		t.Fatal("SoulForRole() error = nil")
	}
	var validation *domain.ValidationError
	if !errors.As(err, &validation) || validation.Code != domain.RoleUnknown {
		t.Fatalf("SoulForRole() error = %v, want ROLE_UNKNOWN", err)
	}
}
