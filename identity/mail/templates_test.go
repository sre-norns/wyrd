package mail

import (
	"strings"
	"testing"
)

func TestAllTemplatesBrandAndEscape(t *testing.T) {
	for _, name := range []string{"identity-link", "existing-user", "invitation", "project-access", "new-sign-in", "confirm-sign-in"} {
		t.Run(name, func(t *testing.T) {
			m, err := Render(name, Data{ProductName: "Urth", Subject: "Urth sign-in", URL: "https://example.test/confirm?token=one&next=two", Account: "<script>bad</script>", Project: "<script>bad</script>", Instruction: "<script>bad</script>", Provider: "<script>bad</script>"})
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(m.Text, "Exp-Bench") || strings.Contains(m.HTML, "<script>") {
				t.Fatal("product branding or HTML escaping failed")
			}
			if m.Subject == "" || m.Text == "" || m.HTML == "" {
				t.Fatal("incomplete template")
			}
		})
	}
}
