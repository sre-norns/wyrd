package identity

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	e "github.com/sre-norns/wyrd/identity/model"
	"github.com/sre-norns/wyrd/identity/resource"
	"github.com/sre-norns/wyrd/pkg/manifest"
	"gorm.io/gorm"
)

func systemRecord(r e.Resource) e.SystemRecord {
	return e.SystemRecord{ID: r.ID, Revision: r.Revision, Status: r.Status, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt, ActorID: r.Actor.UserID, LastModifiedBy: publicActor(r.Actor)}
}

func newSystemRecord(ctx context.Context, status string) e.SystemRecord {
	t := time.Now().UTC()
	return e.SystemRecord{ID: newID(), Revision: 1, Status: status, CreatedAt: t, UpdatedAt: t, ActorID: principal(ctx).UserID, LastModifiedBy: publicActor(mutationActor(ctx))}
}

func accountImpact(db *gorm.DB, account e.AccountID) (map[string]int64, string, error) {
	counts := map[string]int64{}
	hashes := []string{}
	var total int64
	for _, table := range accountTables(db) {
		var value struct {
			Count  int64
			Digest string
		}
		if err := db.Raw("SELECT count(*) AS count, md5(COALESCE(string_agg(md5(row_to_json(r)::text), ',' ORDER BY md5(row_to_json(r)::text)), '')) AS digest FROM "+table+" r WHERE account_id = ?", account).Scan(&value).Error; err != nil {
			return nil, "", err
		}
		counts[table] = value.Count
		total += value.Count
		hashes = append(hashes, table+":"+value.Digest)
	}
	counts["stored_resources"] = total
	for key, table := range map[string]string{"active_members": "account_memberships", "active_sessions": "sessions", "active_tokens": "agent_identity_tokens"} {
		if !db.Migrator().HasTable(table) {
			continue
		}
		query := db.Table(table).Where("account_id = ? AND status = 'active'", account)
		if key == "active_sessions" {
			query = query.Where("refresh_expires_at > clock_timestamp()")
		}
		if key == "active_tokens" {
			query = query.Where("expires_at IS NULL OR expires_at > clock_timestamp()")
		}
		var n int64
		if err := query.Count(&n).Error; err != nil {
			return nil, "", err
		}
		counts[key] = n
	}
	if f := extensions(db).ImpactCounts; f != nil {
		if err := f(db, account, counts); err != nil {
			return nil, "", err
		}
	}
	data, _ := json.Marshal(counts)
	hashes = append(hashes, string(data))
	return counts, digest(strings.Join(hashes, "|")), nil
}

func accountProjection(db *gorm.DB, account e.Account) (out e.SystemAccount, err error) {
	out = e.SystemAccount{SystemRecord: systemRecord(account.Resource), Name: account.Name, Description: account.Description, OwnerSetup: "missing", SupportStatus: "none", LifecycleReason: account.LifecycleReason, LifecycleAt: account.LifecycleAt, Limits: []e.Limit{}}
	out.Counts, _, err = accountImpact(db, e.AccountID(account.ID))
	if err != nil {
		return
	}
	var owners int64
	err = db.Model(&e.AccountMembership{}).Where("account_id = ? AND role = 'owner' AND status = 'active'", account.ID).Count(&owners).Error
	if err != nil {
		return
	}
	if owners > 0 {
		out.OwnerSetup = "ready"
	} else {
		var pending int64
		err = db.Model(&e.AccountInvitation{}).Where("account_id = ? AND role = 'owner' AND status = 'pending' AND expires_at > clock_timestamp()", account.ID).Count(&pending).Error
		if err != nil {
			return
		}
		if pending > 0 {
			out.OwnerSetup = "invited"
		}
	}
	if db.Migrator().HasTable("limits") {
		err = db.Where("account_id = ? AND project_id = ''", account.ID).Order("name").Find(&out.Limits).Error
	}
	if err != nil {
		return
	}
	for i := range out.Limits {
		if err = effectiveLimit(db, &out.Limits[i]); err != nil {
			return
		}
		out.Limits[i].Actor = e.Principal{}
		out.Limits[i].Labels = nil
		out.Limits[i].Links = nil
		if out.Limits[i].Usage > out.Limits[i].Effective {
			out.OverLimit = true
		}
	}
	var recoveries int64
	err = db.Model(&e.OwnerRecovery{}).Where("target_account_id = ? AND status = 'pending'", account.ID).Count(&recoveries).Error
	if recoveries > 0 {
		out.SupportStatus = "owner-recovery-pending"
	}
	return
}

func (s *Service) SystemConfiguration(ctx context.Context) (e.SystemConfiguration, error) {
	if !systemAuthority(ctx, database(ctx, s.db)) {
		return e.SystemConfiguration{}, forbidden()
	}
	config, _, err := s.ServiceConfig().Get(ctx)
	return e.SystemConfiguration{ServiceConfiguration: config, Purge: s.config.Purge}, err
}

func (s *Service) SystemAccounts(ctx context.Context, q e.SystemQuery) (out e.SystemPage[e.SystemAccount], err error) {
	if !systemAuthority(ctx, database(ctx, s.db)) {
		return out, forbidden()
	}
	out.Items = []e.SystemAccount{}
	err = database(ctx, s.db).Transaction(func(tx *gorm.DB) error {
		query := tx.Model(&e.Account{})
		if q.Search != "" {
			query = query.Where("name ILIKE ? OR id ILIKE ?", "%"+q.Search+"%", "%"+q.Search+"%")
		}
		if q.Status != "" {
			query = query.Where("status = ?", q.Status)
		}
		keys := NewestFirst[e.Account]()
		// Derived filters are applied in Go, after the query; SQL cannot count them.
		keys.NoTotal = q.OwnerSetup != "" || q.LimitState != ""

		// Apply derived filters before pagination so a page never hides later matches.
		var items []e.SystemAccount
		var accounts []e.Account
		batchQuery := q
		page := manifest.Page{}
		for first := true; ; first = false {
			batch, batchPage, err := systemPageOf(query.Session(&gorm.Session{}), batchQuery, keys)
			if err != nil {
				return err
			}
			if first {
				page = manifest.Page{Limit: batchPage.Limit, Total: batchPage.Total}
			}
			for _, account := range batch {
				item, err := accountProjection(tx, account)
				if err != nil {
					return err
				}
				if q.OwnerSetup != "" && item.OwnerSetup != q.OwnerSetup {
					continue
				}
				if q.LimitState == "over-limit" && !item.OverLimit {
					continue
				}
				if q.LimitState == "within-limit" && item.OverLimit {
					continue
				}
				items = append(items, item)
				accounts = append(accounts, account)
				if uint(len(items)) > page.Limit {
					break
				}
			}
			if uint(len(items)) > page.Limit || batchPage.Next == "" {
				break
			}
			batchQuery.Cursor = batchPage.Next
		}
		if uint(len(items)) > page.Limit {
			items = items[:page.Limit]
			var err error
			if page.Next, err = keys.Cursor(&accounts[page.Limit-1]); err != nil {
				return err
			}
		}
		var err error
		out, err = systemPage(tx, items, page)
		return err
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return
}

func (s *Service) SystemAccount(ctx context.Context, id e.AccountID) (out e.SystemAccount, err error) {
	if !systemAuthority(ctx, database(ctx, s.db)) {
		return out, forbidden()
	}
	err = database(ctx, s.db).Transaction(func(tx *gorm.DB) error {
		account, err := load[e.Account](tx, string(id))
		if err != nil {
			return err
		}
		out, err = accountProjection(tx, account)
		return err
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return
}

func (s *Service) SystemMemberships(ctx context.Context, account e.AccountID, q e.SystemQuery) (out e.SystemPage[e.SystemMembership], err error) {
	db := database(ctx, s.db)
	if !systemAuthority(ctx, db) {
		return out, forbidden()
	}
	if _, err = load[e.Account](db, string(account)); err != nil {
		return
	}
	items, page, err := systemPageOf(db.Model(&e.AccountMembership{}).Where("account_id = ?", account), q, NewestFirst[e.AccountMembership]())
	if err != nil {
		return out, err
	}
	projected := []e.SystemMembership{}
	var owners int64
	if err = db.Model(&e.AccountMembership{}).Where("account_id = ? AND role = 'owner' AND status = 'active'", account).Count(&owners).Error; err != nil {
		return
	}
	for _, item := range items {
		email, _, err := userDisplay(db, item.UserID)
		if err != nil {
			return out, err
		}
		projected = append(projected, e.SystemMembership{SystemRecord: systemRecord(item.Resource), AccountID: account, UserID: item.UserID, Email: email, Role: item.Role, ActiveOwners: owners})
	}
	return systemPage(db, projected, page)
}

func (s *Service) SystemInvitations(ctx context.Context, account e.AccountID, q e.SystemQuery) (out e.SystemPage[e.SystemInvitation], err error) {
	db := database(ctx, s.db)
	if !systemAuthority(ctx, db) {
		return out, forbidden()
	}
	if _, err = load[e.Account](db, string(account)); err != nil {
		return
	}
	query := db.Model(&e.AccountInvitation{}).Where("account_id = ?", account)
	if q.Status != "" {
		query = query.Where("("+invitationStatusSQL+") = ?", q.Status)
	}
	if q.Limit < 0 {
		return out, invalid("Limit must not be negative.")
	}
	items, page, err := invitationPage(query, manifest.SearchQuery{Cursor: q.Cursor, Limit: uint(q.Limit)}, q.Sort, q.Direction)
	if err != nil {
		return out, err
	}
	if out, err = systemPage(db, []e.SystemInvitation{}, page); err != nil {
		return out, err
	}
	for _, item := range items {
		if item.Status == "pending" && !item.ExpiresAt.After(out.GeneratedAt) {
			item.Status = "expired"
		}
		if err = decorateInvitation(db, &item); err != nil {
			return out, err
		}
		out.Items = append(out.Items, SystemInvitationProjection(item))
	}
	return
}

func (s *Service) SystemActivity(ctx context.Context, q e.SystemQuery) (out e.SystemPage[e.SystemActivity], err error) {
	db := database(ctx, s.db)
	if !systemAuthority(ctx, db) {
		return out, forbidden()
	}
	query := db.Model(&e.SystemActivity{})
	for column, value := range map[string]string{"target_account_id": string(q.AccountID), "kind": q.Kind, "action": q.Action, "outcome": q.Outcome, "actor_id": q.ActorID, "request_id": q.RequestID} {
		if value != "" {
			query = query.Where(column+" = ?", value)
		}
	}
	if q.From != nil {
		query = query.Where("created_at >= ?", q.From)
	}
	if q.Till != nil {
		query = query.Where("created_at <= ?", q.Till)
	}
	items, page, err := systemPageOf(query, q, systemNewestFirst[e.SystemActivity]())
	if err != nil {
		return out, err
	}
	return systemPage(db, items, page)
}

func systemPrecondition(ctx context.Context, revision int64) error {
	return precondition(ctx, &e.Resource{Revision: revision})
}

func requireReason(action e.SystemAction) error {
	if strings.TrimSpace(action.Reason) == "" {
		return invalidFields("A reason is required.", map[string]string{"reason": "Enter a reason."})
	}
	if len(action.Reason) > 2000 || len(action.Reference) > 500 {
		return invalid("Reason or reference is too long.")
	}
	return nil
}

func systemAudit(ctx context.Context, db *gorm.DB, account e.AccountID, target, operation, outcome string, action e.SystemAction) error {
	return recordSystemAudit(ctx, db, account, target, operation, outcome, action, true)
}

func recordSystemAudit(ctx context.Context, db *gorm.DB, account e.AccountID, target, operation, outcome string, action e.SystemAction, projectAccount bool) error {
	event := e.SystemActivity{SystemRecord: newSystemRecord(ctx, "recorded"), TargetAccountID: account, Kind: "audit", Action: operation, TargetID: target, Outcome: outcome, Reason: action.Reason, Reference: action.Reference, RequestID: request(ctx).ID, SessionScope: principal(ctx).Scope, ChangeIDs: []string{}}
	if outcome == "succeeded" {
		change := event
		change.ID = newID()
		change.Kind = "change"
		if err := db.Create(&change).Error; err != nil {
			return err
		}
		event.ChangeIDs = append(event.ChangeIDs, change.ID)
	}
	if principal(ctx).Scope == e.ScopeSystem {
		var session e.Session
		if err := db.Where("id = ?", principal(ctx).CredentialID).First(&session).Error; err != nil {
			return err
		}
		event.AssuranceMethod = session.AuthenticationMethod
	}
	if err := db.Create(&event).Error; err != nil {
		return err
	}
	if projectAccount {
		wire, err := resource.Encode(event)
		if err != nil {
			return err
		}
		snapshot, err := json.Marshal(wire)
		if err != nil {
			return err
		}
		return audit(ctx, db, Audit{Snapshot: snapshot, Principal: principal(ctx), Action: operation, Target: &e.Resource{ID: target, AccountID: account, Revision: systemTargetRevision(db, target)}, Outcome: outcome, RequestID: request(ctx).ID, SystemAction: &action})
	}

	return nil
}

func safeSystemFailure(err error) string {
	if p, ok := errors.AsType[*Problem](err); ok {
		return p.Code
	}
	return "operation-failed"
}

func rejectedSystemAudit(ctx context.Context, db *gorm.DB, cause error) error {
	req := request(ctx)
	if req.ID != "" {
		var n int64
		if err := db.Model(&e.SystemActivity{}).Where("request_id = ? AND kind = 'audit'", req.ID).Count(&n).Error; err != nil {
			return err
		}
		if n > 0 {
			return nil
		}
	}
	target, action := req.Target, req.Method+" "+req.Target
	if !strings.HasPrefix(req.Target, "/v1/system/") {
		target = ""
		action = req.Method + " protected-resource"
	}
	return recordSystemAudit(ctx, db, cachedAccount(db, req.Target), target, action, safeSystemFailure(cause), e.SystemAction{Reason: req.SupportReason, Reference: req.SupportReference}, false)
}

func systemTargetRevision(db *gorm.DB, id string) int64 {
	for _, table := range []string{"accounts", "account_memberships", "owner_recoveries", "account_deletion_requests", "limits", "research_roles", "review_policies"} {
		if !db.Migrator().HasTable(table) {
			continue
		}
		var row struct{ Revision int64 }
		if db.Table(table).Select("revision").Where("id = ?", id).Take(&row).Error == nil {
			return row.Revision
		}
	}
	return 1
}
