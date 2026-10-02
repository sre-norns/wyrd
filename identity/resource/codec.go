// Package resource defines the canonical identity wire contract. Domain/storage
// models remain separate: only these explicit mappings reach resource APIs.
package resource

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/sre-norns/wyrd/identity/model"
	"github.com/sre-norns/wyrd/pkg/manifest"
)

const APIVersion = "identity.sre-norns.com/v1"

var ErrNotResource = errors.New("type is not an identity resource")

// Actor is attribution, never an authorization snapshot or credential.
type Actor = model.ResourceActor

type State struct {
	Phase          string `json:"phase" yaml:"phase"`
	Authority      string `json:"authority,omitempty" yaml:"authority,omitempty"`
	LastModifiedBy *Actor `json:"lastModifiedBy,omitempty" yaml:"lastModifiedBy,omitempty"`
}
type Delivery struct {
	State         string     `json:"state" yaml:"state"`
	Attempts      int        `json:"attempts" yaml:"attempts"`
	LastAttemptAt *time.Time `json:"lastAttemptAt,omitempty" yaml:"lastAttemptAt,omitempty"`
	SentAt        *time.Time `json:"sentAt,omitempty" yaml:"sentAt,omitempty"`
	NextRetryAt   *time.Time `json:"nextRetryAt,omitempty" yaml:"nextRetryAt,omitempty"`
	FailureCode   string     `json:"failureCode,omitempty" yaml:"failureCode,omitempty"`
}

// Field declares one mapped field. Input fields are create/command-only and
// never enter resource reads. Metadata and lifecycle commands are handled apart.
type Field struct {
	GoName  string `json:"goName" yaml:"goName"`
	Domain  string `json:"domain" yaml:"domain"`
	Name    string `json:"name" yaml:"name"`
	Section string `json:"section" yaml:"section"`
	Create  bool   `json:"create" yaml:"create"`
	Patch   bool   `json:"patch" yaml:"patch"`
}
type Definition struct {
	Model  string           `json:"model" yaml:"model"`
	Kind   manifest.Kind    `json:"kind" yaml:"kind"`
	Scopes []manifest.Scope `json:"scopes" yaml:"scopes"`
	Fields []Field          `json:"fields" yaml:"fields"`
}
type definition struct {
	Definition
	spec, status reflect.Type
}

var registry manifest.Registry
var definitions = map[reflect.Type]definition{}

func register(value any, kind manifest.Kind, spec, status any, scopes []manifest.Scope, fields []Field) {
	typ := reflect.TypeOf(value)
	if err := registry.Register(manifest.TypeMeta{APIVersion: APIVersion, Kind: kind}, spec, status, scopes...); err != nil {
		panic(err)
	}
	definitions[typ] = definition{Definition: Definition{Model: typ.Name(), Kind: kind, Scopes: scopes, Fields: fields}, spec: reflect.TypeOf(spec), status: reflect.TypeOf(status)}
}
func base(t reflect.Type) reflect.Type {
	for t != nil && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return t
}
func IsResource(value any) bool { _, ok := definitions[base(reflect.TypeOf(value))]; return ok }
func Describe(value any) (Definition, bool) {
	d, ok := definitions[base(reflect.TypeOf(value))]
	out := d.Definition
	out.Fields = append([]Field(nil), out.Fields...)
	out.Scopes = append([]manifest.Scope(nil), out.Scopes...)
	return out, ok
}
func Definitions() []Definition {
	out := make([]Definition, 0, len(definitions))
	for typ := range definitions {
		d, _ := Describe(reflect.New(typ).Interface())
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Model < out[j].Model })
	return out
}
func indirect(v reflect.Value) reflect.Value {
	for v.IsValid() && (v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface) {
		if v.IsNil() {
			return reflect.Value{}
		}
		v = v.Elem()
	}
	return v
}
func stringField(v reflect.Value, name string) string {
	f := v.FieldByName(name)
	if f.IsValid() && f.Kind() == reflect.String {
		return f.String()
	}
	return ""
}
func timeField(v reflect.Value, name string) *time.Time {
	f := v.FieldByName(name)
	if !f.IsValid() {
		return nil
	}
	t, ok := f.Interface().(time.Time)
	if !ok || t.IsZero() {
		return nil
	}
	return &t
}
func metaFor(v reflect.Value) (manifest.ObjectMeta, error) {
	id := stringField(v, "ID")
	if id == "" {
		id = stringField(v, "UserID")
	}
	name := stringField(v, "Name")
	if name == "" {
		name = id
	}
	revision := v.FieldByName("Revision")
	var version int64
	if revision.IsValid() {
		version = revision.Int()
	}
	if version < 0 {
		return manifest.ObjectMeta{}, fmt.Errorf("invalid negative resource revision")
	}
	m := manifest.ObjectMeta{UID: manifest.ResourceID(id), Name: manifest.ResourceName(name), Version: manifest.Version(version), CreatedAt: timeField(v, "CreatedAt"), UpdatedAt: timeField(v, "UpdatedAt")}
	// Only actual ownership fields belong in metadata; TargetAccountID does not.
	m.Account = manifest.ResourceID(stringField(v, "AccountID"))
	m.Project = manifest.ResourceID(stringField(v, "ProjectID"))
	if labels := v.FieldByName("Labels"); labels.IsValid() {
		m.Labels = labels.Interface().(manifest.Labels)
	}
	return m, nil
}
func stateFor(v reflect.Value) State {
	s := State{Phase: stringField(v, "Status"), Authority: stringField(v, "Authority")}
	if field := v.FieldByName("LastModifiedBy"); field.IsValid() {
		actor := field.Interface().(model.ResourceActor)
		if actor.Type != "" {
			s.LastModifiedBy = &actor
		}
		return s
	}
	if f := v.FieldByName("Actor"); f.IsValid() {
		p := f.Interface().(model.Principal)
		if p.Type != "" {
			s.LastModifiedBy = &Actor{Type: p.Type, UserID: p.UserID, AgentID: p.AgentID}
		}
	} else if id := stringField(v, "ActorID"); id != "" {
		s.LastModifiedBy = &Actor{Type: "user", UserID: id}
	}
	return s
}
func typedObject(data map[string]any, typ reflect.Type) (any, error) {
	raw, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	value := reflect.New(typ).Interface()
	if err = json.Unmarshal(raw, value); err != nil {
		return nil, err
	}
	return value, nil
}

// EncodeResource projects only registered, typed resource fields. It never
// includes an operation's one-time token or the database principal snapshot.
func EncodeResource(value any) (manifest.ResourceManifest, error) {
	v := indirect(reflect.ValueOf(value))
	if !v.IsValid() {
		return manifest.ResourceManifest{}, ErrNotResource
	}
	d, ok := definitions[v.Type()]
	if !ok {
		return manifest.ResourceManifest{}, ErrNotResource
	}
	m, err := metaFor(v)
	if err != nil {
		return manifest.ResourceManifest{}, err
	}
	spec := map[string]any{}
	status := map[string]any{}
	state, err := json.Marshal(stateFor(v))
	if err != nil {
		return manifest.ResourceManifest{}, err
	}
	if err = json.Unmarshal(state, &status); err != nil {
		return manifest.ResourceManifest{}, err
	}
	for _, f := range d.Fields {
		if f.Section == "input" {
			continue
		}
		item, err := Encode(v.FieldByName(f.GoName).Interface())
		if err != nil {
			return manifest.ResourceManifest{}, err
		}
		if f.Section == "spec" {
			spec[f.Name] = item
		} else {
			status[f.Name] = item
		}
	}
	specValue, err := typedObject(spec, d.spec)
	if err != nil {
		return manifest.ResourceManifest{}, err
	}
	statusValue, err := typedObject(status, d.status)
	if err != nil {
		return manifest.ResourceManifest{}, err
	}
	out := manifest.ResourceManifest{TypeMeta: manifest.TypeMeta{APIVersion: APIVersion, Kind: d.Kind}, Metadata: m, Spec: specValue, Status: statusValue}
	if links := v.FieldByName("Links"); links.IsValid() && !links.IsNil() {
		out.Links = map[string]manifest.HLink{}
		for name, url := range links.Interface().(map[string]string) {
			out.Links[name] = manifest.HLink{Reference: url, Relationship: name}
		}
	}
	return out, registry.ValidateTypes(out)
}

// Encode preserves protocol/query DTO shapes, but projects registered resources
// inside lists and aggregates. Map keys and opaque JSON remain unchanged.
func Encode(value any) (any, error) {
	v := indirect(reflect.ValueOf(value))
	if !v.IsValid() {
		return nil, nil
	}
	if v.Type() == reflect.TypeFor[model.User]() {
		return nil, fmt.Errorf("persisted users have no resource representation; use PersonalProfile")
	}
	if _, ok := definitions[v.Type()]; ok {
		return EncodeResource(v.Interface())
	}
	if d, ok := v.Interface().(model.InvitationEmailDelivery); ok {
		return Delivery(d), nil
	}
	if _, ok := v.Interface().(json.Marshaler); ok {
		return v.Interface(), nil
	}
	switch v.Kind() {
	case reflect.Slice, reflect.Array:
		if v.Kind() == reflect.Slice && v.IsNil() {
			return nil, nil
		}
		items := make([]any, v.Len())
		for i := range items {
			item, err := Encode(v.Index(i).Interface())
			if err != nil {
				return nil, err
			}
			items[i] = item
		}
		return items, nil
	case reflect.Map:
		if v.Type().Key().Kind() != reflect.String {
			return value, nil
		}
		if v.IsNil() {
			return nil, nil
		}
		out := map[string]any{}
		iter := v.MapRange()
		for iter.Next() {
			item, err := Encode(iter.Value().Interface())
			if err != nil {
				return nil, err
			}
			out[iter.Key().String()] = item
		}
		return out, nil
	case reflect.Struct:
		out := map[string]any{}
		for i := range v.NumField() {
			f := v.Type().Field(i)
			if !f.IsExported() {
				continue
			}
			tag := strings.Split(f.Tag.Get("json"), ",")
			if tag[0] == "-" {
				continue
			}
			if len(tag) > 1 && tag[1] == "omitempty" && v.Field(i).IsZero() {
				continue
			}
			item, err := Encode(v.Field(i).Interface())
			if err != nil {
				return nil, err
			}
			if f.Anonymous && tag[0] == "" {
				if fields, ok := item.(map[string]any); ok {
					for k, x := range fields {
						out[k] = x
					}
				} else {
					return nil, fmt.Errorf("embedded resource requires an explicit operation result")
				}
				continue
			}
			name := tag[0]
			if name == "" {
				name = f.Name
			}
			out[name] = item
		}
		return out, nil
	}
	return value, nil
}

// Decode reads a canonical resource or an explicit query DTO. Registered
// resources never fall back to a flat object. Nested resources are validated
// against their destination type, not just whatever kind the body declares.
func Decode(data []byte, dest any) error {
	v := reflect.ValueOf(dest)
	if v.Kind() != reflect.Pointer || v.IsNil() {
		return fmt.Errorf("decode destination must be a non-nil pointer")
	}
	return decodeValue(data, v.Elem())
}
func decodeValue(data []byte, v reflect.Value) error {
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		if _, ok := definitions[v.Type()]; ok {
			return fmt.Errorf("resource must be an object")
		}
		v.SetZero()
		return nil
	}
	if v.Kind() == reflect.Pointer {
		v.Set(reflect.New(v.Type().Elem()))
		return decodeValue(data, v.Elem())
	}
	if handled, err := decodeCreatedResult(data, v); handled {
		return err
	}
	if d, ok := definitions[v.Type()]; ok {
		return decodeResource(data, v, d)
	}
	if v.Type() == reflect.TypeFor[model.InvitationEmailDelivery]() {
		var delivery Delivery
		if err := json.Unmarshal(data, &delivery); err != nil {
			return err
		}
		v.Set(reflect.ValueOf(model.InvitationEmailDelivery(delivery)))
		return nil
	}
	if v.CanAddr() {
		if _, ok := v.Addr().Interface().(json.Unmarshaler); ok {
			return json.Unmarshal(data, v.Addr().Interface())
		}
	}
	switch v.Kind() {
	case reflect.Slice:
		var raw []json.RawMessage
		if err := json.Unmarshal(data, &raw); err != nil {
			return err
		}
		v.Set(reflect.MakeSlice(v.Type(), len(raw), len(raw)))
		for i := range raw {
			if err := decodeValue(raw[i], v.Index(i)); err != nil {
				return err
			}
		}
		return nil
	case reflect.Struct:
		var raw map[string]json.RawMessage
		if err := json.Unmarshal(data, &raw); err != nil {
			return err
		}
		for i := range v.NumField() {
			f := v.Type().Field(i)
			if !f.IsExported() {
				continue
			}
			tag := strings.Split(f.Tag.Get("json"), ",")[0]
			if tag == "-" {
				continue
			}
			if f.Anonymous && tag == "" {
				if err := decodeValue(data, v.Field(i)); err != nil {
					return err
				}
				continue
			}
			if tag == "" {
				tag = f.Name
			}
			if body, ok := raw[tag]; ok {
				if err := decodeValue(body, v.Field(i)); err != nil {
					return err
				}
			}
		}
		return nil
	}
	return json.Unmarshal(data, v.Addr().Interface())
}
func setString(v reflect.Value, name, value string) {
	if f := v.FieldByName(name); f.IsValid() && f.CanSet() && f.Kind() == reflect.String {
		f.SetString(value)
	}
}
func decodeResource(data []byte, v reflect.Value, d definition) error {
	// An operation result is explicit; only credential-bearing kinds accept one.
	raw, finish, err := unwrapResult(data, v)
	if err != nil {
		return err
	}
	m, err := registry.DecodeJSON(raw)
	if err != nil {
		return err
	}
	if m.Kind != d.Kind || m.APIVersion != APIVersion {
		return fmt.Errorf("unexpected resource type: want %s/%s", APIVersion, d.Kind)
	}
	if m.Metadata.Version > math.MaxInt64 {
		return fmt.Errorf("resource version exceeds supported range")
	}
	v.SetZero()
	if f := v.FieldByName("ID"); f.IsValid() {
		setString(v, "ID", string(m.Metadata.UID))
	} else {
		setString(v, "UserID", string(m.Metadata.UID))
	}
	setString(v, "Name", string(m.Metadata.Name))
	setString(v, "AccountID", string(m.Metadata.Account))
	setString(v, "ProjectID", string(m.Metadata.Project))
	if f := v.FieldByName("Revision"); f.IsValid() {
		f.SetInt(int64(m.Metadata.Version))
	}
	if f := v.FieldByName("Labels"); f.IsValid() {
		f.Set(reflect.ValueOf(m.Metadata.Labels))
	}
	for name, t := range map[string]*time.Time{"CreatedAt": m.Metadata.CreatedAt, "UpdatedAt": m.Metadata.UpdatedAt} {
		if f := v.FieldByName(name); f.IsValid() && t != nil {
			f.Set(reflect.ValueOf(*t))
		}
	}
	var state State
	if m.Status != nil {
		body, _ := json.Marshal(m.Status)
		if err := json.Unmarshal(body, &state); err != nil {
			return err
		}
	}
	setString(v, "Status", state.Phase)
	setString(v, "Authority", state.Authority)
	if state.LastModifiedBy != nil {
		a := state.LastModifiedBy
		if f := v.FieldByName("LastModifiedBy"); f.IsValid() {
			f.Set(reflect.ValueOf(*a))
		}
		if f := v.FieldByName("Actor"); f.IsValid() {
			f.Set(reflect.ValueOf(model.Principal{Type: a.Type, UserID: a.UserID, AgentID: a.AgentID}))
		}
	}
	if f := v.FieldByName("Links"); f.IsValid() {
		links := map[string]string{}
		for name, link := range m.Links {
			links[name] = link.Reference
		}
		f.Set(reflect.ValueOf(links))
	}
	var spec, status map[string]json.RawMessage
	body, _ := json.Marshal(m.Spec)
	if err := json.Unmarshal(body, &spec); err != nil {
		return err
	}
	body, _ = json.Marshal(m.Status)
	if err := json.Unmarshal(body, &status); err != nil {
		return err
	}
	for _, f := range d.Fields {
		if f.Section == "input" {
			continue
		}
		fields := spec
		if f.Section == "status" {
			fields = status
		}
		if raw, ok := fields[f.Name]; ok {
			if err := decodeValue(raw, v.FieldByName(f.GoName)); err != nil {
				return fmt.Errorf("%s.%s: %w", f.Section, f.Name, err)
			}
		}
	}
	return finish(v)
}

// Contains reports whether a value's type contains identity resources. Clients
// can preserve a product's own codecs while canonicalizing identity aggregates.
func Contains(value any) bool { return containsType(reflect.TypeOf(value), map[reflect.Type]bool{}) }
func containsType(t reflect.Type, seen map[reflect.Type]bool) bool {
	if t == nil {
		return false
	}
	t = base(t)
	if seen[t] {
		return false
	}
	seen[t] = true
	if _, ok := definitions[t]; ok {
		return true
	}
	if t.Kind() == reflect.Slice || t.Kind() == reflect.Array || t.Kind() == reflect.Map {
		return containsType(t.Elem(), seen)
	}
	if t.Kind() == reflect.Struct {
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			if f.IsExported() && f.Tag.Get("json") != "-" && containsType(f.Type, seen) {
				return true
			}
		}
	}
	return false
}
