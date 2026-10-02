package resource

import (
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/sre-norns/wyrd/identity/model"
)

// EncodeResult is only for mutation responses. Read APIs use Encode, which
// excludes every one-time credential even if a domain value still holds one.
func EncodeResult(value any) (any, error) {
	v := indirect(reflect.ValueOf(value))
	if !v.IsValid() {
		return nil, nil
	}
	switch x := v.Interface().(type) {
	case model.Account:
		body, err := EncodeResource(x)
		if err != nil {
			return nil, err
		}
		out := map[string]any{"resource": body}
		if x.OwnerInvitation != nil {
			inv, err := EncodeResult(*x.OwnerInvitation)
			if err != nil {
				return nil, err
			}
			out["ownerInvitation"] = inv
		}
		return out, nil
	case model.AccountInvitation:
		body, err := EncodeResource(x)
		if err != nil {
			return nil, err
		}
		out := map[string]any{"resource": body}
		if x.Token != "" {
			out["token"] = x.Token
		}
		return out, nil
	case model.AgentIdentityToken:
		body, err := EncodeResource(x)
		if err != nil {
			return nil, err
		}
		out := map[string]any{"resource": body}
		if x.Token != "" {
			out["token"] = x.Token
		}
		return out, nil
	case model.OwnerRecovery:
		body, err := EncodeResource(x)
		if err != nil {
			return nil, err
		}
		out := map[string]any{"resource": body}
		if x.InvitationToken != "" {
			out["token"] = x.InvitationToken
		}
		return out, nil
	case model.SystemAccountCreated:
		body, err := EncodeResource(x.SystemAccount)
		if err != nil {
			return nil, err
		}
		out := map[string]any{"resource": body, "ownerInvitationId": x.OwnerInvitationID}
		if x.InvitationToken != "" {
			out["token"] = x.InvitationToken
		}
		if x.EmailDelivery != nil {
			out["emailDelivery"] = Delivery(*x.EmailDelivery)
		}
		return out, nil
	case model.SystemInvitationCreated:
		body, err := EncodeResource(x.SystemInvitation)
		if err != nil {
			return nil, err
		}
		out := map[string]any{"resource": body}
		if x.Token != "" {
			out["token"] = x.Token
		}
		return out, nil
	}
	return Encode(value)
}

func unwrapResult(data []byte, v reflect.Value) ([]byte, func(reflect.Value) error, error) {
	done := func(reflect.Value) error { return nil }
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return nil, done, err
	}
	raw, wrapped := fields["resource"]
	if !wrapped {
		return data, done, nil
	}
	allowed := map[string]bool{"resource": true}
	switch v.Type() {
	case reflect.TypeFor[model.Account]():
		allowed["ownerInvitation"] = true
	case reflect.TypeFor[model.AccountInvitation](), reflect.TypeFor[model.AgentIdentityToken](), reflect.TypeFor[model.OwnerRecovery]():
		allowed["token"] = true
	default:
		return nil, done, fmt.Errorf("unexpected operation result for %s", v.Type().Name())
	}
	for key := range fields {
		if !allowed[key] {
			return nil, done, fmt.Errorf("unknown operation-result field %q", key)
		}
	}
	return raw, func(v reflect.Value) error {
		if token, ok := fields["token"]; ok {
			field := "Token"
			if v.Type() == reflect.TypeFor[model.OwnerRecovery]() {
				field = "InvitationToken"
			}
			if err := json.Unmarshal(token, v.FieldByName(field).Addr().Interface()); err != nil {
				return err
			}
		}
		if invitation, ok := fields["ownerInvitation"]; ok {
			var item model.AccountInvitation
			if err := Decode(invitation, &item); err != nil {
				return err
			}
			v.FieldByName("OwnerInvitation").Set(reflect.ValueOf(&item))
		}
		return nil
	}, nil
}

func decodeCreatedResult(data []byte, v reflect.Value) (bool, error) {
	if v.Type() != reflect.TypeFor[model.SystemAccountCreated]() && v.Type() != reflect.TypeFor[model.SystemInvitationCreated]() {
		return false, nil
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return true, err
	}
	allowed := map[string]bool{"resource": true, "token": true}
	if v.Type() == reflect.TypeFor[model.SystemAccountCreated]() {
		allowed["ownerInvitationId"] = true
		allowed["emailDelivery"] = true
	}
	for key := range fields {
		if !allowed[key] {
			return true, fmt.Errorf("unknown operation-result field %q", key)
		}
	}
	body, ok := fields["resource"]
	if !ok {
		return true, fmt.Errorf("resource operation result is required")
	}
	var err error
	if v.Type() == reflect.TypeFor[model.SystemAccountCreated]() {
		var out model.SystemAccountCreated
		err = Decode(body, &out.SystemAccount)
		if err == nil {
			err = json.Unmarshal(fieldsOrNull(fields, "ownerInvitationId"), &out.OwnerInvitationID)
		}
		if err == nil {
			err = json.Unmarshal(fieldsOrNull(fields, "token"), &out.InvitationToken)
		}
		if delivery, ok := fields["emailDelivery"]; ok && err == nil {
			var d Delivery
			err = json.Unmarshal(delivery, &d)
			out.EmailDelivery = (*model.InvitationEmailDelivery)(&d)
		}
		v.Set(reflect.ValueOf(out))
	} else {
		var out model.SystemInvitationCreated
		err = Decode(body, &out.SystemInvitation)
		if err == nil {
			err = json.Unmarshal(fieldsOrNull(fields, "token"), &out.Token)
		}
		v.Set(reflect.ValueOf(out))
	}
	return true, err
}
func fieldsOrNull(fields map[string]json.RawMessage, key string) json.RawMessage {
	if value, ok := fields[key]; ok {
		return value
	}
	return json.RawMessage("null")
}

// OutcomeOwner supplies replay ownership before transport serialization. Hosts
// set HTTPOutcome.AccountID from this value when creating system accounts.
func OutcomeOwner(value any) string {
	v := indirect(reflect.ValueOf(value))
	if !v.IsValid() {
		return ""
	}
	switch x := v.Interface().(type) {
	case model.Account:
		return x.ID
	case model.SystemAccount:
		return x.ID
	case model.SystemAccountCreated:
		return x.ID
	}
	return ""
}
