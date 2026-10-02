package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"github.com/sre-norns/wyrd/identity/model"
)

// SystemClient serves the shared identity operations mounted by httpapi.MountSystem.
// Mutations take the resource that was read, carrying its version as If-Match.
type SystemClient struct{ *Client }

func (c *Client) System() *SystemClient { return &SystemClient{c} }

// SystemSearchValues encodes the explicit system query DTO (not a resource).
func SystemSearchValues(query model.SystemQuery) (url.Values, error) {
	data, err := json.Marshal(query)
	if err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	if err = json.Unmarshal(data, &fields); err != nil {
		return nil, err
	}
	values := url.Values{}
	for key, raw := range fields {
		var value string
		if err := json.Unmarshal(raw, &value); err != nil {
			value = string(raw)
		}
		values.Set(key, value)
	}
	return values, nil
}

func systemRead[T any](ctx context.Context, c *Client, path string, query model.SystemQuery) (value T, err error) {
	values, err := SystemSearchValues(query)
	if err != nil {
		return value, err
	}
	value, _, err = Resource[T](c).Request(ctx, http.MethodGet, path, values, nil)
	return
}

func systemWrite[T any](ctx context.Context, c *Client, method, path string, record *model.SystemRecord, command any) (value T, err error) {
	if record != nil {
		options := Options(ctx)
		if record.ID == "" {
			return value, &Problem{Status: 422, Code: "missing-resource-id", Detail: "Read the resource before updating it."}
		}
		if options.IfMatch == "" {
			if record.Revision <= 0 {
				return value, &Problem{Status: 428, Code: "precondition-required", Detail: "Read the resource version before updating it."}
			}
			options.IfMatch = fmt.Sprintf("\"%d\"", record.Revision)
		}
		ctx = WithRequestOptions(ctx, options)
	}
	value, _, err = Resource[T](c).Request(ctx, method, path, nil, command)
	return
}

func (c *SystemClient) Accounts(ctx context.Context, query model.SystemQuery) (model.SystemPage[model.SystemAccount], error) {
	return systemRead[model.SystemPage[model.SystemAccount]](ctx, c.Client, ResourcePath("v1/system", "accounts"), query)
}

func (c *SystemClient) Account(ctx context.Context, id string) (model.SystemAccount, error) {
	return systemRead[model.SystemAccount](ctx, c.Client, ResourcePath("v1/system", "accounts", id), model.SystemQuery{})
}

func (c *SystemClient) Memberships(ctx context.Context, id string, query model.SystemQuery) (model.SystemPage[model.SystemMembership], error) {
	return systemRead[model.SystemPage[model.SystemMembership]](ctx, c.Client, ResourcePath("v1/system", "accounts", id, "memberships"), query)
}

func (c *SystemClient) Invitations(ctx context.Context, id string, query model.SystemQuery) (model.SystemPage[model.SystemInvitation], error) {
	return systemRead[model.SystemPage[model.SystemInvitation]](ctx, c.Client, ResourcePath("v1/system", "accounts", id, "invitations"), query)
}

func (c *SystemClient) ImpactPreviews(ctx context.Context, id string, query model.SystemQuery) (model.SystemPage[model.ImpactPreview], error) {
	return systemRead[model.SystemPage[model.ImpactPreview]](ctx, c.Client, ResourcePath("v1/system", "accounts", id, "impact-previews"), query)
}

func (c *SystemClient) OwnerRecoveries(ctx context.Context, id string, query model.SystemQuery) (model.SystemPage[model.OwnerRecovery], error) {
	return systemRead[model.SystemPage[model.OwnerRecovery]](ctx, c.Client, ResourcePath("v1/system", "accounts", id, "owner-recoveries"), query)
}

func (c *SystemClient) OwnerRecovery(ctx context.Context, id string) (model.OwnerRecovery, error) {
	return systemRead[model.OwnerRecovery](ctx, c.Client, ResourcePath("v1/system", "owner-recoveries", id), model.SystemQuery{})
}

func (c *SystemClient) DeletionRequests(ctx context.Context, query model.SystemQuery) (model.SystemPage[model.AccountDeletionRequest], error) {
	return systemRead[model.SystemPage[model.AccountDeletionRequest]](ctx, c.Client, ResourcePath("v1/system", "deletion-requests"), query)
}

func (c *SystemClient) DeletionRequest(ctx context.Context, id string) (model.AccountDeletionRequest, error) {
	return systemRead[model.AccountDeletionRequest](ctx, c.Client, ResourcePath("v1/system", "deletion-requests", id), model.SystemQuery{})
}

func (c *SystemClient) Activity(ctx context.Context, query model.SystemQuery) (model.SystemPage[model.SystemActivity], error) {
	return systemRead[model.SystemPage[model.SystemActivity]](ctx, c.Client, ResourcePath("v1/system", "activity"), query)
}

func (c *SystemClient) AuditEvents(ctx context.Context, query model.SystemQuery) (model.SystemPage[model.SystemActivity], error) {
	return systemRead[model.SystemPage[model.SystemActivity]](ctx, c.Client, ResourcePath("v1/system", "audit-events"), query)
}

func (c *SystemClient) Changes(ctx context.Context, query model.SystemQuery) (model.SystemPage[model.SystemActivity], error) {
	return systemRead[model.SystemPage[model.SystemActivity]](ctx, c.Client, ResourcePath("v1/system", "changes"), query)
}

func (c *SystemClient) Configuration(ctx context.Context) (model.SystemConfiguration, error) {
	return systemRead[model.SystemConfiguration](ctx, c.Client, ResourcePath("v1/system", "configuration"), model.SystemQuery{})
}

func (c *SystemClient) CreateAccount(ctx context.Context, command model.SystemAccountCreate) (model.SystemAccountCreated, error) {
	return systemWrite[model.SystemAccountCreated](ctx, c.Client, "POST", ResourcePath("v1/system", "accounts"), nil, command)
}

func (c *SystemClient) CreateImpactPreview(ctx context.Context, id string, command model.SystemAction) (model.ImpactPreview, error) {
	return systemWrite[model.ImpactPreview](ctx, c.Client, "POST", ResourcePath("v1/system", "accounts", id, "impact-previews"), nil, command)
}

func (c *SystemClient) CreateStepUp(ctx context.Context, id string, command model.SystemAction) (model.StepUpAuthorization, error) {
	return systemWrite[model.StepUpAuthorization](ctx, c.Client, "POST", ResourcePath("v1/system", "accounts", id, "step-up-authorizations"), nil, command)
}

func (c *SystemClient) ChangeLifecycle(ctx context.Context, value model.SystemAccount, command model.SystemAction) (model.SystemAccount, error) {
	return systemWrite[model.SystemAccount](ctx, c.Client, "PATCH", ResourcePath("v1/system", "accounts", value.ID), &value.SystemRecord, command)
}

func (c *SystemClient) RevokeMembership(ctx context.Context, value model.SystemMembership, command model.SystemAction) (model.SystemMembership, error) {
	return systemWrite[model.SystemMembership](ctx, c.Client, "PATCH", ResourcePath("v1/system", "account-memberships", value.ID), &value.SystemRecord, command)
}

func (c *SystemClient) CreateOwnerRecovery(ctx context.Context, value model.SystemAccount, command model.SystemAction) (model.OwnerRecovery, error) {
	return systemWrite[model.OwnerRecovery](ctx, c.Client, "POST", ResourcePath("v1/system", "accounts", value.ID, "owner-recoveries"), &value.SystemRecord, command)
}

func (c *SystemClient) CompleteOwnerRecovery(ctx context.Context, value model.OwnerRecovery, command model.SystemAction) (model.OwnerRecovery, error) {
	return systemWrite[model.OwnerRecovery](ctx, c.Client, "PATCH", ResourcePath("v1/system", "owner-recoveries", value.ID), &value.SystemRecord, command)
}

func (c *SystemClient) RequestDeletion(ctx context.Context, value model.SystemAccount, command model.SystemAction) (model.AccountDeletionRequest, error) {
	return systemWrite[model.AccountDeletionRequest](ctx, c.Client, "POST", ResourcePath("v1/system", "accounts", value.ID, "deletion-requests"), &value.SystemRecord, command)
}

func (c *SystemClient) ApproveDeletion(ctx context.Context, value model.AccountDeletionRequest, command model.SystemAction) (model.AccountDeletionRequest, error) {
	return systemWrite[model.AccountDeletionRequest](ctx, c.Client, "POST", ResourcePath("v1/system", "deletion-requests", value.ID, "approvals"), &value.SystemRecord, command)
}

func (c *SystemClient) ChangeDeletionRequest(ctx context.Context, value model.AccountDeletionRequest, command model.SystemAction) (model.AccountDeletionRequest, error) {
	return systemWrite[model.AccountDeletionRequest](ctx, c.Client, "PATCH", ResourcePath("v1/system", "deletion-requests", value.ID), &value.SystemRecord, command)
}

func (c *SystemClient) RequestInvitationDelivery(ctx context.Context, value model.SystemInvitation, command model.SystemAction) (model.SystemInvitation, error) {
	return systemWrite[model.SystemInvitation](ctx, c.Client, "POST", ResourcePath("v1/system", "account-invitations", value.ID, "deliveries"), &value.SystemRecord, command)
}

func (c *SystemClient) RevokeInvitation(ctx context.Context, value model.SystemInvitation, command model.SystemAction) (model.SystemInvitation, error) {
	return systemWrite[model.SystemInvitation](ctx, c.Client, "POST", ResourcePath("v1/system", "account-invitations", value.ID, "revocations"), &value.SystemRecord, command)
}

func (c *SystemClient) CreateFirstOwnerInvitation(ctx context.Context, value model.SystemAccount, command model.FirstOwnerInvitation) (model.SystemInvitationCreated, error) {
	return systemWrite[model.SystemInvitationCreated](ctx, c.Client, "POST", ResourcePath("v1/system", "accounts", value.ID, "owner-invitations"), &value.SystemRecord, command)
}
