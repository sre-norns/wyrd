package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/sre-norns/wyrd/identity/model"
	"github.com/sre-norns/wyrd/identity/resource"
)

func TestCanonicalDocumentsAndStructuredOutput(t *testing.T) {
	data, err := os.ReadFile("../examples/project-read.yaml")
	if err != nil {
		t.Fatal(err)
	}
	p, patch, err := DecodeObject[model.Project](data)
	if err != nil {
		t.Fatal(err)
	}
	if p.Revision != 4 || p.Name != "monitoring" || p.Actor.UserID == "" {
		t.Fatalf("read document %+v", p)
	}
	encoded, _ := json.Marshal(patch)
	if strings.Contains(string(encoded), "status") || strings.Contains(string(encoded), "version") || strings.Contains(string(encoded), "account") {
		t.Fatalf("read-only apply fields %s", encoded)
	}
	for _, format := range []string{FormatJSON, FormatYAML} {
		var buf bytes.Buffer
		if err := (Output{Format: format, Stdout: &buf}).Encode(p); err != nil {
			t.Fatal(err)
		}
		got, _, err := DecodeObject[model.Project](buf.Bytes())
		if err != nil {
			t.Fatalf("%s: %v\n%s", format, err, buf.String())
		}
		if got.Revision != p.Revision || got.Actor.UserID != p.Actor.UserID || got.Labels["team_name"] != "sre" {
			t.Fatalf("%s round trip %+v", format, got)
		}
	}
	if _, _, err := DecodeObject[model.Project](append(data, []byte("\n---\nunknown: second-document\n")...)); err == nil {
		t.Fatal("accepted trailing YAML document")
	}
}
func TestExamplesUsePublicInputCodecs(t *testing.T) {
	for _, example := range []struct {
		name  string
		dest  any
		patch bool
	}{
		{"project-create.json", &model.Project{}, false},
		{"project-patch.json", &model.Project{}, true},
		{"profile-patch.json", &model.PersonalProfile{}, true},
		{"revoke.json", &model.Session{}, true},
		{"invitation-create.json", &model.AccountInvitation{}, false},
	} {
		data, err := os.ReadFile("../examples/" + example.name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := resource.DecodeInput(data, example.dest, example.patch); err != nil {
			t.Errorf("%s: %v", example.name, err)
		}
	}
}
func TestOneTimeOutputRequiresAnOperation(t *testing.T) {
	value := model.AgentIdentityToken{Resource: model.Resource{ID: "token"}, Token: "one-time"}
	var buf bytes.Buffer
	output := Output{Format: FormatJSON, Stdout: &buf}
	if err := output.Encode(value); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf.String(), "one-time") {
		t.Fatal("ordinary output exposed token")
	}
	buf.Reset()
	if err := RenderUpsert(output, value, true, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), `"resource":`) || !strings.Contains(buf.String(), "one-time") {
		t.Fatalf("operation output %s", buf.String())
	}
}
