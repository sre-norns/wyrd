package identity

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	e "github.com/sre-norns/wyrd/identity/model"
	"gorm.io/gorm"
)

type HTTPOutcome struct {
	// AccountID is typed ownership of a successful create result, not a field parsed from its wire body.
	AccountID e.AccountID
	Status    int
	Body      []byte
	Header    http.Header
}

func (s *Service) TransactHTTP(ctx context.Context, token, key, body string, fn func(context.Context) HTTPOutcome) (out HTTPOutcome, err error) {
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lock(tx); err != nil {
			return err
		}
		ctx = context.WithValue(ctx, databaseKey, tx)
		p, err := s.Authenticate(ctx, token)
		if err != nil {
			return err
		}
		ctx = WithPrincipal(ctx, p)
		req := request(ctx)
		cached := req.Method == http.MethodPost && !extensions(s.db).UncachedPostPaths[req.Target]
		recordID := digest(string(p.Scope) + ":" + p.CredentialID + ":" + p.Type + ":" + p.UserID + ":" + string(p.AgentID) + ":" + string(p.AccountID) + ":" + req.Method + ":" + req.Target + ":" + key)
		if cached {
			if key == "" {
				return invalid("Idempotency-Key is required.")
			}
			if len(key) > 200 {
				return invalid("Idempotency-Key exceeds 200 bytes.")
			}
			var r idempotencyRecord
			err := tx.Where("id = ?", recordID).First(&r).Error
			if err == nil {
				if r.Digest != digest(body) {
					return conflict("idempotency-conflict")
				}
				out = HTTPOutcome{AccountID: r.AccountID, Status: r.Status, Body: r.Body}
				return json.Unmarshal(r.Headers, &out.Header)
			}
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
		}
		out = fn(ctx)
		if cached {
			headers, err := json.Marshal(out.Header)
			if err != nil {
				return err
			}
			persisted := redactSecrets(out.Body)
			account := p.AccountID
			if p.Scope == e.ScopeSystem {
				account = cachedAccount(tx, req.Target)
				if out.Status >= 200 && out.Status < 300 && out.AccountID != "" {
					account = out.AccountID
				}
			}
			return tx.Create(&idempotencyRecord{AccountID: account, ID: recordID, Digest: digest(body), Status: out.Status, Body: persisted, Headers: headers}).Error
		}
		return nil
	})
	if err != nil {
		if auditErr := s.AuditRejected(ctx, err); auditErr != nil {
			return out, auditErr
		}
	}
	return
}

func redactSecrets(body []byte) []byte {
	var v any
	if json.Unmarshal(body, &v) != nil {
		return body
	}
	var strip func(any, string)
	strip = func(v any, parent string) {
		switch obj := v.(type) {
		case map[string]any:
			for k, value := range obj {
				if parent == "metadata" && k == "labels" {
					continue
				}
				if k == "token" || k == "lease_token" || k == "access_token" || k == "refresh_token" || k == "invitationToken" || k == "leaseToken" || k == "accessToken" || k == "refreshToken" {
					delete(obj, k)
				} else {
					strip(value, k)
				}
			}
		case []any:
			for _, value := range obj {
				strip(value, parent)
			}
		}
	}
	strip(v, "")
	out, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	return out
}

type idempotencyRecord struct {
	AccountID  e.AccountID `gorm:"index"`
	ID         string      `gorm:"primaryKey"`
	Digest     string
	Status     int
	Body       []byte
	Headers    []byte
	ResourceID string
}

func (s *Service) AuditRejected(ctx context.Context, cause error) error {
	if f := extensions(s.db).HTTPRejected; f != nil {
		return f(ctx, s.db.WithContext(ctx), cause)
	}
	return nil
}
