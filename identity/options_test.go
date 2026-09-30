package identity

import (
	"reflect"
	"strings"
	"testing"
)

// Options is embedded in products' commands, so its flag names are part of
// their interface. Kong derives a name from the field name and splits "GitHub"
// into git-hub, so those fields name themselves.
func TestGitHubOptionsNameTheirFlags(t *testing.T) {
	for field := range reflect.TypeFor[Options]().Fields() {
		if !strings.HasPrefix(field.Name, "GitHub") {
			continue
		}
		name := field.Tag.Get("name")
		if !strings.HasPrefix(name, "github-") {
			t.Errorf("%s has flag name %q; kong would derive git-hub-…", field.Name, name)
		}
	}
}
