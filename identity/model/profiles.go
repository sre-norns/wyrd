package model

import (
	"context"
	"time"

	"github.com/sre-norns/wyrd/pkg/manifest"
)

type PersonalProfile struct {
	LastModifiedBy ResourceActor `json:"-"`
	UserID         string        `json:"user_id"`
	Email          string        `json:"email"`
	DisplayName    *string       `json:"display_name"`
	Status         string        `json:"status"`
	Revision       int64         `json:"revision"`
}

type ProfileAccount struct {
	AccountID string `json:"account_id"`
	Name      string `json:"name"`
	Role      string `json:"role"`
	Status    string `json:"status"`
}

type PersonalProfileService interface {
	Get(ctx context.Context) (resource PersonalProfile, exists bool, commError error)
	Update(ctx context.Context, profile PersonalProfile) (resource PersonalProfile, commError error)
	AccessibleAccounts(ctx context.Context, query manifest.SearchQuery) (accounts []ProfileAccount, page manifest.Page, commError error)
}

const (
	SignInEmail  = "email"
	SignInGoogle = "google"
	SignInGitHub = "github"
)

type SignInMethod struct {
	LastModifiedBy ResourceActor `json:"-"`
	ID             string        `json:"id"`
	Method         string        `json:"method"`
	Status         string        `json:"status"`
	CreatedAt      time.Time     `json:"created_at"`
	LastUsedAt     *time.Time    `json:"last_used_at,omitempty"`
	Revision       int64         `json:"revision"`
}

type SignInMethodService interface {
	List(ctx context.Context) (methods []SignInMethod, commError error)
	Get(ctx context.Context, id string) (resource SignInMethod, exists bool, commError error)
	// Revoke removes a Google or GitHub method. The revision of method is the
	// required precondition.
	Revoke(ctx context.Context, method SignInMethod) (resource SignInMethod, commError error)
}
