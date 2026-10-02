package identity

import (
	"context"
	"errors"
	"time"

	e "github.com/sre-norns/wyrd/identity/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (s *Service) CreateStepUp(ctx context.Context, account e.AccountID, action e.SystemAction) (out e.StepUpAuthorization, err error) {
	err = mutation(ctx, s.db, func(tx *gorm.DB) error {
		if !systemAuthority(ctx, tx) {
			return forbidden()
		}
		if action.Operation != "purge" && action.Operation != "approve-purge" {
			return invalid("Unsupported step-up action.")
		}
		a, err := load[e.Account](tx, string(account))
		if err != nil {
			return err
		}
		if a.Status != "archived" && a.Status != "deletion-pending" {
			return conflict("account-not-archived")
		}
		if err := recentAuthentication(ctx, tx, s.config.Purge.StepUpMaxAgeSeconds, s.config.Purge.AssuranceMethod); err != nil {
			return err
		}
		session, err := load[e.Session](tx, principal(ctx).CredentialID)
		if err != nil {
			return err
		}
		out = e.StepUpAuthorization{SystemRecord: newSystemRecord(ctx, "ready"), TargetAccountID: account, Action: action.Operation, SessionID: session.ID, AuthenticatedAt: session.AuthenticatedAt, AssuranceMethod: session.AuthenticationMethod, ExpiresAt: session.AuthenticatedAt.Add(time.Duration(s.config.Purge.StepUpMaxAgeSeconds) * time.Second)}
		if err := tx.Create(&out).Error; err != nil {
			return err
		}
		return recordSystemAudit(ctx, tx, account, string(account), "create-step-up", "succeeded", e.SystemAction{}, false)
	})
	return
}

func consumeStepUp(ctx context.Context, tx *gorm.DB, account e.AccountID, action, id string, maxAge int64) (out e.StepUpAuthorization, err error) {
	if id == "" {
		return out, conflict("step-up-required")
	}
	out, err = load[e.StepUpAuthorization](tx, id)
	if err != nil {
		return out, conflict("invalid-step-up")
	}
	t, err := now(tx)
	if err != nil {
		return out, err
	}
	if out.ActorID != principal(ctx).UserID || out.SessionID != principal(ctx).CredentialID || out.TargetAccountID != account || out.Action != action || out.ConsumedAt != nil || !out.ExpiresAt.After(t) || t.Sub(out.AuthenticatedAt) > time.Duration(maxAge)*time.Second {
		return out, conflict("invalid-step-up")
	}
	out.ConsumedAt = &t
	out.Status = "consumed"
	touchSystem(ctx, &out.SystemRecord, t)
	err = tx.Save(&out).Error
	return
}

func (s *Service) RequestAccountDeletion(ctx context.Context, account e.AccountID, action e.SystemAction) (out e.AccountDeletionRequest, err error) {
	err = mutation(ctx, s.db, func(tx *gorm.DB) error {
		if !systemAuthority(ctx, tx) {
			return forbidden()
		}
		if !s.config.Purge.Enabled {
			return conflict("purge-disabled")
		}
		if err := requireReason(action); err != nil {
			return err
		}
		a, err := load[e.Account](tx, string(account))
		if err != nil {
			return err
		}
		if a.Status != "archived" {
			return conflict("account-not-archived")
		}
		if err := systemPrecondition(ctx, a.Revision); err != nil {
			return err
		}
		if action.Confirmation != string(account) || !action.RecoveryAcknowledged {
			return invalid("Confirm the exact account ID and acknowledge the recovery delay.")
		}
		preview, err := consumePreview(ctx, tx, account, string(account), "purge", action.PreviewID, a.Revision)
		if err != nil {
			return err
		}
		if preview.Counts["stored_resources"] > s.config.Purge.MaxResourceCount {
			return conflict("purge-size-limit")
		}
		step, err := consumeStepUp(ctx, tx, account, "purge", action.StepUpID, s.config.Purge.StepUpMaxAgeSeconds)
		if err != nil {
			return err
		}
		out = e.AccountDeletionRequest{SystemRecord: newSystemRecord(ctx, "pending"), TargetAccountID: account, Reason: action.Reason, Reference: action.Reference, Counts: preview.Counts, ApprovalMode: s.config.Purge.ApprovalMode, ExecuteAfter: time.Now().UTC().Add(time.Duration(s.config.Purge.RecoveryDelaySeconds) * time.Second), Approvals: []e.AccountDeletionApproval{}}
		if out.ApprovalMode == "single" {
			out.Status = "approved"
			approval := e.AccountDeletionApproval{SystemRecord: newSystemRecord(ctx, "approved"), RequestID: out.ID, UserID: principal(ctx).UserID, AuthenticatedAt: step.AuthenticatedAt, AssuranceMethod: step.AssuranceMethod}
			if err := tx.Create(&approval).Error; err != nil {
				return err
			}
			out.Approvals = append(out.Approvals, approval)
		}
		if err := tx.Create(&out).Error; err != nil {
			return err
		}
		a.Status = "deletion-pending"
		a.Revision++
		a.Actor = mutationActor(ctx)
		a.UpdatedAt = time.Now().UTC()
		if err := tx.Save(&a).Error; err != nil {
			return err
		}
		return systemAudit(ctx, tx, account, out.ID, "request-account-deletion", "succeeded", action)
	})
	return
}

func (s *Service) DeletionRequests(ctx context.Context, q e.SystemQuery) (out e.SystemPage[e.AccountDeletionRequest], err error) {
	db := database(ctx, s.db)
	if !systemAuthority(ctx, db) {
		return out, forbidden()
	}
	query := db.Model(&e.AccountDeletionRequest{})
	if q.Status != "" {
		query = query.Where("status = ?", q.Status)
	}
	if q.AccountID != "" {
		query = query.Where("target_account_id = ?", q.AccountID)
	}
	items, page, err := systemPageOf(query, q, systemNewestFirst[e.AccountDeletionRequest]())
	if err != nil {
		return out, err
	}
	for i := range items {
		if err = decorateDeletion(db, &items[i]); err != nil {
			return
		}
	}
	return systemPage(db, items, page)
}

func decorateDeletion(db *gorm.DB, item *e.AccountDeletionRequest) error {
	item.Approvals = []e.AccountDeletionApproval{}
	return db.Where("request_id = ?", item.ID).Order("created_at").Find(&item.Approvals).Error
}

func (s *Service) DeletionRequest(ctx context.Context, id string) (out e.AccountDeletionRequest, err error) {
	db := database(ctx, s.db)
	if !systemAuthority(ctx, db) {
		return out, forbidden()
	}
	out, err = load[e.AccountDeletionRequest](db, id)
	if err == nil {
		err = decorateDeletion(db, &out)
	}
	return
}

func (s *Service) ApproveAccountDeletion(ctx context.Context, id string, action e.SystemAction) (out e.AccountDeletionRequest, err error) {
	err = mutation(ctx, s.db, func(tx *gorm.DB) error {
		if !systemAuthority(ctx, tx) {
			return forbidden()
		}
		if !s.config.Purge.Enabled {
			return conflict("purge-disabled")
		}
		if err := requireReason(action); err != nil {
			return err
		}
		out, err = load[e.AccountDeletionRequest](tx, id)
		if err != nil {
			return err
		}
		if err := systemPrecondition(ctx, out.Revision); err != nil {
			return err
		}
		if out.Status != "pending" || out.ApprovalMode != "dual" {
			return conflict("approval-not-pending")
		}
		if out.ActorID == principal(ctx).UserID {
			return conflict("independent-approval-required")
		}
		if action.Confirmation != string(out.TargetAccountID) {
			return invalid("Confirm the exact account ID.")
		}
		account, err := load[e.Account](tx, string(out.TargetAccountID))
		if err != nil {
			return err
		}
		if account.Status != "deletion-pending" {
			return conflict("invalid-account-transition")
		}
		step, err := consumeStepUp(ctx, tx, out.TargetAccountID, "approve-purge", action.StepUpID, s.config.Purge.StepUpMaxAgeSeconds)
		if err != nil {
			return err
		}
		approval := e.AccountDeletionApproval{SystemRecord: newSystemRecord(ctx, "approved"), RequestID: out.ID, UserID: principal(ctx).UserID, AuthenticatedAt: step.AuthenticatedAt, AssuranceMethod: step.AssuranceMethod}
		if err := tx.Create(&approval).Error; err != nil {
			return err
		}
		out.Status = "approved"
		touchSystem(ctx, &out.SystemRecord, time.Now().UTC())
		if err := tx.Save(&out).Error; err != nil {
			return err
		}
		if err := systemAudit(ctx, tx, out.TargetAccountID, out.ID, "approve-account-deletion", "succeeded", action); err != nil {
			return err
		}
		return decorateDeletion(tx, &out)
	})
	return
}

func (s *Service) ChangeDeletionRequest(ctx context.Context, id string, action e.SystemAction) (out e.AccountDeletionRequest, err error) {
	err = mutation(ctx, s.db, func(tx *gorm.DB) error {
		if !systemAuthority(ctx, tx) {
			return forbidden()
		}
		if err := requireReason(action); err != nil {
			return err
		}
		out, err = load[e.AccountDeletionRequest](tx, id)
		if err != nil {
			return err
		}
		if err := systemPrecondition(ctx, out.Revision); err != nil {
			return err
		}
		if action.Operation == "retry" {
			if !s.config.Purge.Enabled || out.Status != "failed" || out.Attempts >= s.config.Purge.RetryLimit {
				return conflict("retry-unavailable")
			}
			if _, err := consumeStepUp(ctx, tx, out.TargetAccountID, "approve-purge", action.StepUpID, s.config.Purge.StepUpMaxAgeSeconds); err != nil {
				return err
			}
			if action.Confirmation != string(out.TargetAccountID) {
				return invalid("Confirm the exact account ID.")
			}
			out.Status = "approved"
			out.FailureCode = ""
		} else if action.Operation == "cancel" {
			if out.Status != "pending" && out.Status != "approved" && out.Status != "failed" {
				return conflict("cancellation-unavailable")
			}
			account, err := load[e.Account](tx, string(out.TargetAccountID))
			if err != nil {
				return err
			}
			if account.Status != "deletion-pending" {
				return conflict("invalid-account-transition")
			}
			account.Status = "archived"
			account.Revision++
			account.Actor = mutationActor(ctx)
			account.UpdatedAt = time.Now().UTC()
			if err := tx.Save(&account).Error; err != nil {
				return err
			}
			out.Status = "cancelled"
			out.CancelReason = action.Reason
		} else {
			return invalid("Use cancel or retry.")
		}
		touchSystem(ctx, &out.SystemRecord, time.Now().UTC())
		if err := tx.Save(&out).Error; err != nil {
			return err
		}
		if err := systemAudit(ctx, tx, out.TargetAccountID, id, action.Operation+"-account-deletion", "succeeded", action); err != nil {
			return err
		}
		return decorateDeletion(tx, &out)
	})
	return
}

func (s *Service) ExecuteDuePurge(ctx context.Context) (executed bool, err error) {
	if !s.config.Purge.Enabled {
		return false, nil
	}
	var claimed e.AccountDeletionRequest
	var executionError error
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lock(tx); err != nil {
			return err
		}
		err := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).Where("status = 'approved' AND execute_after <= clock_timestamp() AND attempts < ?", s.config.Purge.RetryLimit).Order("execute_after, id").First(&claimed).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		// The savepoint rolls back account deletion but retains the outer claim lock.
		executionError = tx.Transaction(func(tx *gorm.DB) error {
			account, err := load[e.Account](tx, string(claimed.TargetAccountID))
			if err != nil {
				return err
			}
			if account.Status != "deletion-pending" {
				return conflict("invalid-account-transition")
			}
			if err := decorateDeletion(tx, &claimed); err != nil {
				return err
			}
			if len(claimed.Approvals) == 0 {
				return conflict("approval-required")
			}
			if claimed.ApprovalMode == "dual" && claimed.Approvals[0].UserID == claimed.ActorID {
				return conflict("independent-approval-required")
			}
			counts, _, err := accountImpact(tx, claimed.TargetAccountID)
			if err != nil {
				return err
			}
			if counts["stored_resources"] > s.config.Purge.MaxResourceCount {
				return conflict("purge-size-limit")
			}
			claimed.Status = "executing"
			claimed.Attempts++
			touchSystem(ctx, &claimed.SystemRecord, time.Now().UTC())
			if err := tx.Save(&claimed).Error; err != nil {
				return err
			}
			tombstone := e.AccountPurgeTombstone{SystemRecord: newSystemRecord(ctx, "purged"), TargetAccountID: claimed.TargetAccountID, RequestID: claimed.ID, Reason: claimed.Reason, Reference: claimed.Reference, Counts: counts, ApproverIDs: []string{}}
			tombstone.ActorID = claimed.ActorID
			for _, approval := range claimed.Approvals {
				tombstone.ApproverIDs = append(tombstone.ApproverIDs, approval.UserID)
			}
			if err := tx.Create(&tombstone).Error; err != nil {
				return err
			}
			var members []string
			if err := tx.Raw("SELECT DISTINCT user_id FROM account_memberships WHERE account_id = ?", claimed.TargetAccountID).Scan(&members).Error; err != nil {
				return err
			}
			for _, table := range credentialOwnerTables(tx) {
				if err := tx.Exec("DELETE FROM credentials WHERE owner_id IN (SELECT "+credentialOwnerID(tx, table)+"::text FROM "+table+" WHERE account_id = ?)", claimed.TargetAccountID).Error; err != nil {
					return err
				}
			}
			for _, k := range extensions(tx).Kinds {
				if k.PurgeHook != nil {
					if err := k.PurgeHook(ctx, tx, claimed.TargetAccountID); err != nil {
						return err
					}
				}
			}
			for _, table := range accountTables(tx) {
				if err := tx.Exec("DELETE FROM "+table+" WHERE account_id = ?", claimed.TargetAccountID).Error; err != nil {
					return err
				}
			}
			for _, table := range auxiliaryTables(tx) {
				if err := tx.Exec("DELETE FROM "+table+" WHERE account_id = ?", claimed.TargetAccountID).Error; err != nil {
					return err
				}
			}
			for _, table := range []string{"impact_previews", "owner_recoveries", "step_up_authorizations"} {
				if err := tx.Exec("DELETE FROM "+table+" WHERE target_account_id = ?", claimed.TargetAccountID).Error; err != nil {
					return err
				}
			}
			if err := purgeProviderState(tx, claimed.TargetAccountID, members); err != nil {
				return err
			}
			if err := tx.Delete(&account).Error; err != nil {
				return err
			}
			// Account-visible events disappear. Retained system history contains only safe metadata.
			t, err := now(tx)
			if err != nil {
				return err
			}
			claimed.Status = "completed"
			claimed.CompletedAt = &t
			claimed.UpdatedAt = t
			claimed.Revision++
			claimed.LastModifiedBy = publicActor(mutationActor(ctx))
			if err := tx.Save(&claimed).Error; err != nil {
				return err
			}
			return recordSystemAudit(ctx, tx, claimed.TargetAccountID, claimed.ID, "execute-account-deletion", "succeeded", e.SystemAction{Reason: claimed.Reason, Reference: claimed.Reference}, false)
		})
		if executionError != nil {
			if err := tx.Model(&e.AccountDeletionRequest{}).Where("id = ?", claimed.ID).Updates(systemMutation(ctx, map[string]any{"status": "failed", "failure_code": safeSystemFailure(executionError), "attempts": gorm.Expr("attempts + 1"), "revision": gorm.Expr("revision + 1"), "updated_at": time.Now().UTC()})).Error; err != nil {
				return err
			}
			return recordSystemAudit(ctx, tx, claimed.TargetAccountID, claimed.ID, "execute-account-deletion", safeSystemFailure(executionError), e.SystemAction{}, false)
		}
		executed = true
		return nil
	})
	if err != nil {
		return false, err
	}
	if executionError != nil {
		return false, executionError
	}
	return
}

func (s *Service) RunPurgeWorker(ctx context.Context) {
	ticker := time.NewTicker(time.Duration(s.config.Purge.WorkerIntervalSeconds) * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_, _ = s.runWorker(ctx, PurgeWorker, s.ExecuteDuePurge)
		}
	}
}
