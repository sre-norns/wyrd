package resource

import (
	"encoding/json"
	"gopkg.in/yaml.v3"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/sre-norns/wyrd/identity/model"
	"github.com/sre-norns/wyrd/pkg/manifest"
)

func bytesOf(t *testing.T, value any) []byte {
	t.Helper()
	b, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
func TestEveryRegisteredSchemaRoundTrips(t *testing.T) {
	for typ, d := range definitions {
		t.Run(d.Model, func(t *testing.T) {
			v := reflect.New(typ).Elem()
			setString(v, "ID", "record")
			setString(v, "UserID", "person")
			setString(v, "Name", "example")
			setString(v, "Status", "active")
			if field := v.FieldByName("Revision"); field.IsValid() {
				field.SetInt(7)
			}
			encoded, err := EncodeResource(v.Interface())
			if err != nil {
				t.Fatal(err)
			}
			got := reflect.New(typ)
			if err := Decode(bytesOf(t, encoded), got.Interface()); err != nil {
				t.Fatal(err)
			}
			again, err := EncodeResource(got.Interface())
			if err != nil {
				t.Fatal(err)
			}
			if string(bytesOf(t, encoded)) != string(bytesOf(t, again)) {
				t.Fatalf("unstable resource: %s / %s", bytesOf(t, encoded), bytesOf(t, again))
			}
			for _, f := range d.Fields {
				if _, ok := typ.FieldByName(f.GoName); !ok {
					t.Errorf("missing domain field %s", f.GoName)
				}
			}
		})
	}
}
func TestCanonicalProjectAndSafeAttribution(t *testing.T) {
	stamp := time.Date(2026, 10, 2, 3, 4, 5, 0, time.UTC)
	value := model.Project{Resource: model.Resource{ID: "p", Name: "demo", AccountID: "a", Revision: 3, Status: "active", CreatedAt: stamp, UpdatedAt: stamp, Labels: manifest.Labels{"snake_key": "kept"}, Actor: model.Principal{Type: "user", UserID: "u", CredentialID: "private-credential", SystemAdmin: true}, Links: map[string]string{"self": "/v1/projects/p"}}, Description: "description", Target: "target"}
	encoded, err := Encode(value)
	if err != nil {
		t.Fatal(err)
	}
	body := bytesOf(t, encoded)
	for _, want := range []string{`"apiVersion":"identity.sre-norns.com/v1"`, `"kind":"projects"`, `"account":"a"`, `"version":3`, `"lastModifiedBy":{"type":"user","userId":"u"}`, `"snake_key":"kept"`, `"_links"`, `"ref":"/v1/projects/p"`} {
		if !strings.Contains(string(body), want) {
			t.Errorf("missing %s in %s", want, body)
		}
	}
	for _, private := range []string{"private-credential", "system_admin", "credential_id", "account_id", "created_at", "revision"} {
		if strings.Contains(string(body), private) {
			t.Errorf("leaked %s", private)
		}
	}
	var got model.Project
	if err := Decode(body, &got); err != nil {
		t.Fatal(err)
	}
	if got.ID != "p" || got.Revision != 3 || got.Actor.UserID != "u" || got.Actor.CredentialID != "" || got.Description != "description" || got.Links["self"] != "/v1/projects/p" {
		t.Fatalf("decoded %+v", got)
	}
	for _, bad := range []string{`{"id":"p"}`, strings.Replace(string(body), APIVersion, "wrong/v1", 1), strings.Replace(string(body), `"kind":"projects"`, `"kind":"accounts"`, 1), strings.Replace(string(body), `"description":"description"`, `"unknown":true`, 1), string(body) + `{}`} {
		if Decode([]byte(bad), &got) == nil {
			t.Errorf("accepted invalid resource %s", bad)
		}
	}
}
func TestOperationSecretsAndNestedResources(t *testing.T) {
	account := model.Account{Resource: model.Resource{ID: "a", Revision: 1}, OwnerInvitation: &model.AccountInvitation{Resource: model.Resource{ID: "i", AccountID: "a", Revision: 1}, Email: "owner@example.test", Token: "one-time"}}
	read, err := Encode(account)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(bytesOf(t, read)), "one-time") || strings.Contains(string(bytesOf(t, read)), "ownerInvitation") {
		t.Fatal("read leaked operation")
	}
	result, err := EncodeResult(account)
	if err != nil {
		t.Fatal(err)
	}
	var decoded model.Account
	if err = Decode(bytesOf(t, result), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.OwnerInvitation == nil || decoded.OwnerInvitation.Token != "one-time" || decoded.OwnerInvitation.AccountID != "a" {
		t.Fatalf("operation result: %+v", decoded)
	}
	system := model.SystemAccount{SystemRecord: model.SystemRecord{ID: "a", Revision: 2}, Limits: []model.Limit{{Resource: model.Resource{ID: "l", AccountID: "a", Revision: 1}, Value: 5}}}
	nested, err := Encode(system)
	if err != nil {
		t.Fatal(err)
	}
	raw := bytesOf(t, nested)
	if !strings.Contains(string(raw), `"kind":"limits"`) {
		t.Fatalf("flat nested limit: %s", raw)
	}
	var got model.SystemAccount
	if err = Decode(raw, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Limits) != 1 || got.Limits[0].Value != 5 || got.Limits[0].AccountID != "a" {
		t.Fatalf("lost nested resource: %+v", got)
	}
	if Decode([]byte(strings.Replace(string(raw), `"kind":"limits"`, `"kind":"projects"`, 1)), &got) == nil {
		t.Fatal("accepted wrong nested kind")
	}
	preview, _ := EncodeResource(model.ImpactPreview{SystemRecord: model.SystemRecord{ID: "preview"}, TargetAccountID: "target"})
	if preview.Metadata.Account != "" || !strings.Contains(string(bytesOf(t, preview)), `"targetAccountId":"target"`) {
		t.Fatal("operation target became ownership")
	}
}
func TestStrictCreatePatchAndCLIApply(t *testing.T) {
	create := `{"apiVersion":"identity.sre-norns.com/v1","kind":"projects","metadata":{"name":"demo","labels":{"Keep_Key":"yes"}},"spec":{"description":"","target":""}}`
	var value model.Project
	if _, err := DecodeInput([]byte(create), &value, false); err != nil {
		t.Fatal(err)
	}
	if value.Name != "demo" || value.Labels["Keep_Key"] != "yes" {
		t.Fatal(value)
	}
	for _, bad := range []string{`{"name":"flat"}`, strings.Replace(create, `"name":"demo"`, `"account":"injected"`, 1), strings.Replace(create, `"target":""`, `"currentContextId":"forged"`, 1), strings.Replace(create, `"spec":`, `"status":{},"spec":`, 1), strings.Replace(create, `"target":""`, `"target":null`, 1), create + `{}`} {
		if _, err := DecodeInput([]byte(bad), &value, false); err == nil {
			t.Errorf("accepted %s", bad)
		}
	}
	fields, err := DecodeInput([]byte(`{"metadata":{"labels":{"remove":null,"add":"new"}},"spec":{"description":""}}`), &value, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := fields["target"]; ok || string(fields["description"]) != `""` || !strings.Contains(string(fields["labels"]), `"remove":null`) {
		t.Fatalf("patch distinctions lost %s", bytesOf(t, fields))
	}
	for _, bad := range []string{`{"status":{"phase":"inactive"}}`, `{"operation":"suspend","spec":{}}`, `{"metadata":{"uid":"overwrite"}}`, `{"spec":null}`} {
		if _, err := DecodeInput([]byte(bad), &value, true); err == nil {
			t.Errorf("accepted %s", bad)
		}
	}
	if _, err = DecodeInput([]byte(`{"operation":"suspend"}`), &value, true); err != nil || value.Status != "suspended" {
		t.Fatalf("command %v %+v", err, value)
	}
	var profile model.PersonalProfile
	fields, err = DecodeInput([]byte(`{"spec":{"displayName":null}}`), &profile, true)
	if err != nil || string(fields["display_name"]) != "null" {
		t.Fatal(fields, err)
	}
	value = model.Project{Resource: model.Resource{ID: "p", Name: "demo", Revision: 9, AccountID: "a", Status: "active"}, Description: ""}
	read, _ := Encode(value)
	fields, err = DecodeDocument(bytesOf(t, read), &value)
	if err != nil {
		t.Fatal(err)
	}
	raw := string(bytesOf(t, fields))
	if strings.Contains(raw, "status") || strings.Contains(raw, "account") || strings.Contains(raw, "version") || !strings.Contains(raw, `"description":""`) {
		t.Fatalf("unsafe apply %s", raw)
	}
}

func TestYAMLUsesTheSameSchemaFields(t *testing.T) {
	value := model.Project{Resource: model.Resource{ID: "p", AccountID: "a", Actor: model.Principal{Type: "user", UserID: "u"}}, CurrentContextID: "context"}
	encoded, err := EncodeResource(value)
	if err != nil {
		t.Fatal(err)
	}
	data, err := yaml.Marshal(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "currentContextId:") || !strings.Contains(string(data), "lastModifiedBy:") || strings.Contains(string(data), "state:") {
		t.Fatalf("incorrect YAML %s", data)
	}
	decoded, err := registry.DecodeYAML(data)
	if err != nil {
		t.Fatal(err)
	}
	var got model.Project
	if err := Decode(bytesOf(t, decoded), &got); err != nil {
		t.Fatal(err)
	}
	if got.CurrentContextID != "context" || got.Actor.UserID != "u" {
		t.Fatal(got)
	}
}
func TestUserCannotBeSerialized(t *testing.T) {
	if _, err := Encode(model.User{Password: []byte("private")}); err == nil {
		t.Fatal("persisted user accepted")
	}
}
