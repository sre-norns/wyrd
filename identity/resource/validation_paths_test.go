package resource

import (
	"github.com/sre-norns/wyrd/identity/model"
	"testing"
)

func TestFieldPathForResource(t *testing.T) {
	for _, tc := range []struct {
		value       any
		field, want string
	}{
		{model.PersonalProfile{}, "display_name", "spec.displayName"},
		{&model.AccountMembership{}, "display_name", "status.displayName"},
		{model.PersonalProfile{}, "revision", "metadata.version"},
		{model.AgentIdentityToken{}, "expires_at", "spec.expiresAt"},
		{model.AccountInvitation{}, "expires_at", "status.expiresAt"},
	} {
		if got := FieldPathFor(tc.value, tc.field); got != tc.want {
			t.Errorf("%T/%s = %s; want %s", tc.value, tc.field, got, tc.want)
		}
	}
}
