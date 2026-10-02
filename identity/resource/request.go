package resource

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
)

// InputError names the canonical field that could not be accepted.
type InputError struct{ Field, Detail string }

func (e *InputError) Error() string      { return e.Field + ": " + e.Detail }
func invalid(field, detail string) error { return &InputError{field, detail} }

func object(data []byte, path string) (map[string]json.RawMessage, error) {
	var value map[string]json.RawMessage
	if err := json.Unmarshal(data, &value); err != nil || value == nil {
		return nil, invalid(path, "expected a JSON object")
	}
	return value, nil
}

// DecodeInput accepts the canonical create envelope or a merge patch. It returns
// the exact supplied fields in domain notation for the transactional services;
// absent fields and explicit null remain distinguishable. Ownership comes only
// from the authorized route. Read-only fields are rejected, even when zero.
func DecodeInput(data []byte, dest any, patch bool) (map[string]json.RawMessage, error) {
	v := reflect.ValueOf(dest)
	if v.Kind() != reflect.Pointer || v.IsNil() {
		return nil, fmt.Errorf("destination must be a non-nil pointer")
	}
	v = v.Elem()
	d, ok := definitions[v.Type()]
	if !ok {
		return nil, ErrNotResource
	}
	obj, err := object(data, "body")
	if err != nil {
		return nil, err
	}
	fields := map[string]json.RawMessage{}
	if raw, command := obj["operation"]; command {
		if !patch || len(obj) != 1 {
			return nil, invalid("operation", "a lifecycle command cannot include resource fields")
		}
		var operation string
		if json.Unmarshal(raw, &operation) != nil {
			return nil, invalid("operation", "expected an operation name")
		}
		phase, ok := operations(d.Model)[operation]
		if !ok {
			return nil, invalid("operation", "unsupported lifecycle operation")
		}
		fields["status"], _ = json.Marshal(phase)
	} else {
		for key := range obj {
			switch key {
			case "apiVersion", "kind", "metadata", "spec":
			default:
				return nil, invalid(key, "field is read-only or unknown")
			}
		}
		for key, want := range map[string]string{"apiVersion": APIVersion, "kind": string(d.Kind)} {
			raw, present := obj[key]
			if !present && patch {
				continue
			}
			var actual string
			if !present || json.Unmarshal(raw, &actual) != nil || actual != want {
				return nil, invalid(key, "expected "+want)
			}
		}
		if raw, present := obj["metadata"]; present {
			meta, err := object(raw, "metadata")
			if err != nil {
				return nil, err
			}
			for key, raw := range meta {
				if (key != "name" && key != "labels") || !metadataWritable(d.Model, patch) {
					return nil, invalid("metadata."+key, "field is read-only or unknown")
				}
				if key == "name" && bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
					return nil, invalid("metadata.name", "expected a string")
				}
				if key == "labels" && !bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
					var labels map[string]*string
					if err := json.Unmarshal(raw, &labels); err != nil {
						return nil, invalid("metadata.labels", "expected string labels (or null to remove)")
					}
					if !patch {
						for _, value := range labels {
							if value == nil {
								return nil, invalid("metadata.labels", "create labels must be strings")
							}
						}
					}
				}
				fields[key] = raw
			}
		}
		raw, present := obj["spec"]
		if !present && !patch {
			return nil, invalid("spec", "required")
		}
		if present {
			spec, err := object(raw, "spec")
			if err != nil {
				return nil, err
			}
			for key, raw := range spec {
				var field *Field
				for i := range d.Fields {
					f := &d.Fields[i]
					if f.Name == key && ((!patch && f.Create) || (patch && f.Patch)) {
						field = f
						break
					}
				}
				if field == nil {
					return nil, invalid("spec."+key, "field is read-only or unknown")
				}
				fields[field.Domain] = raw
			}
		}
	}
	// Decode each input separately so failures identify the public field path.
	v.SetZero()
	for name, raw := range fields {
		goName, path := "", ""
		if name == "name" {
			goName, path = "Name", "metadata.name"
		}
		if name == "labels" {
			goName, path = "Labels", "metadata.labels"
		}
		if name == "status" {
			goName, path = "Status", "operation"
		}
		for _, f := range d.Fields {
			if f.Domain == name {
				goName, path = f.GoName, "spec."+f.Name
				break
			}
		}
		field := v.FieldByName(goName)
		if !field.IsValid() || !field.CanAddr() {
			return nil, invalid(path, "field is not writable")
		}
		// Label nulls are applied by the merge-patch service, not a map of strings.
		if name == "labels" && patch {
			continue
		}
		if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) && field.Kind() != reflect.Pointer && field.Kind() != reflect.Map && field.Kind() != reflect.Slice {
			return nil, invalid(path, "null is not supported")
		}
		if err := json.Unmarshal(raw, field.Addr().Interface()); err != nil {
			return nil, invalid(path, "invalid field value")
		}
	}
	return fields, nil
}

func metadataWritable(model string, patch bool) bool {
	switch model {
	case "Account", "Project", "AccountMembership", "ProjectMembership", "AgentIdentity", "AgentAuthorization":
		return true
	case "AccountInvitation", "AgentIdentityToken":
		return !patch
	}
	return false
}
func operations(model string) map[string]string {
	switch model {
	case "Session", "AgentIdentityToken", "AccountInvitation", "SignInMethod":
		return map[string]string{"revoke": "revoked"}
	case "AccountMembership", "ProjectMembership", "AgentIdentity", "AgentAuthorization":
		return map[string]string{"activate": "active", "deactivate": "inactive", "revoke": "revoked"}
	case "Project":
		return map[string]string{"activate": "active", "deactivate": "inactive", "suspend": "suspended"}
	}
	return nil
}

// Input constructs a writable request from a domain value. It deliberately
// omits observed state and server-owned metadata. Lifecycle changes must use
// Command, which keeps ordinary edits from accidentally changing state.
func Input(value any, patch bool) (map[string]any, error) {
	v := indirect(reflect.ValueOf(value))
	if !v.IsValid() {
		return nil, ErrNotResource
	}
	d, ok := definitions[v.Type()]
	if !ok {
		return nil, ErrNotResource
	}
	spec := map[string]any{}
	for _, f := range d.Fields {
		if (!patch && f.Create) || (patch && f.Patch) {
			spec[f.Name] = v.FieldByName(f.GoName).Interface()
		}
	}
	out := map[string]any{"spec": spec}
	if !patch {
		out["apiVersion"] = APIVersion
		out["kind"] = d.Kind
	}
	if metadataWritable(d.Model, patch) {
		meta := map[string]any{}
		for _, key := range []string{"Name", "Labels"} {
			field := v.FieldByName(key)
			if field.IsValid() {
				name := "name"
				if key == "Labels" {
					name = "labels"
				}
				meta[name] = field.Interface()
			}
		}
		out["metadata"] = meta
	}
	return out, nil
}
func Command(value any, operation string) (map[string]string, error) {
	d, ok := definitions[base(reflect.TypeOf(value))]
	if !ok {
		return nil, ErrNotResource
	}
	if _, ok := operations(d.Model)[operation]; !ok {
		return nil, invalid("operation", "unsupported lifecycle operation")
	}
	return map[string]string{"operation": operation}, nil
}

// FieldPath adapts domain validation fields to the canonical resource envelope.
func FieldPath(domain string) string {
	switch domain {
	case "name", "labels":
		return "metadata." + domain
	case "id":
		return "metadata.uid"
	case "account_id":
		return "metadata.account"
	case "project_id":
		return "metadata.project"
	case "revision":
		return "metadata.version"
	case "status":
		return "status.phase"
	}
	for _, d := range Definitions() {
		for _, f := range d.Fields {
			if f.Domain == domain {
				section := f.Section
				if section == "input" {
					section = "spec"
				}
				return section + "." + f.Name
			}
		}
	}
	return domain
}

// DecodeDocument reads a create document or a server read document for CLI
// apply. Read-only fields are validated when present and then omitted from the
// returned writable patch; omission and explicit null are preserved.
func DecodeDocument(data []byte, dest any) (map[string]json.RawMessage, error) {
	fields, err := object(data, "body")
	if err != nil {
		return nil, err
	}
	var meta map[string]json.RawMessage
	_ = json.Unmarshal(fields["metadata"], &meta)
	if _, read := meta["uid"]; !read {
		if _, err := DecodeInput(data, dest, false); err != nil {
			return nil, err
		}
		return fields, nil
	}
	if err := Decode(data, dest); err != nil {
		return nil, err
	}
	d, ok := definitions[base(reflect.TypeOf(dest))]
	if !ok {
		return nil, ErrNotResource
	}
	out := map[string]json.RawMessage{}
	if metadataWritable(d.Model, true) {
		writable := map[string]json.RawMessage{}
		for _, key := range []string{"name", "labels"} {
			if raw, ok := meta[key]; ok {
				writable[key] = raw
			}
		}
		out["metadata"], _ = json.Marshal(writable)
	}
	var spec map[string]json.RawMessage
	_ = json.Unmarshal(fields["spec"], &spec)
	writable := map[string]json.RawMessage{}
	for _, f := range d.Fields {
		if f.Patch {
			if raw, ok := spec[f.Name]; ok {
				writable[f.Name] = raw
			}
		}
	}
	out["spec"], _ = json.Marshal(writable)
	return out, nil
}
