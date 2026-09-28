package identity

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"time"

	identitymail "github.com/sre-norns/wyrd/identity/mail"

	e "github.com/sre-norns/wyrd/identity/model"
	"gorm.io/gorm"
)

func (s *Service) RunInvitationMailWorker(ctx context.Context) {
	if !s.config.InvitationEmailEnabled || s.config.SendIdentityMail == nil {
		return
	}
	ticker := time.NewTicker(s.config.InvitationWorkerInterval)
	defer ticker.Stop()
	for {
		// Bound each batch so shutdown and other workers can make progress.
		for range 32 {
			worked, err := s.runWorker(ctx, InvitationMailWorker, s.ProcessInvitationMail)
			if err != nil || !worked || ctx.Err() != nil {
				break
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Service) ProcessInvitationMail(ctx context.Context) (worked bool, err error) {
	if _, err = invitationMode(s.config, "email"); err != nil {
		return false, err
	}
	var job invitationDelivery
	var invitation e.AccountInvitation
	var account e.Account
	var token, inviter string
	err = mutation(ctx, s.db, func(tx *gorm.DB) error {
		t, err := now(tx)
		if err != nil {
			return err
		}
		if err = tx.Where("expires_at <= ?", t).Delete(&invitationContinuation{}).Error; err != nil {
			return err
		}
		err = tx.Where("(state = 'queued' AND next_retry_at <= ?) OR (state = 'sending' AND lease_until <= ?)", t, t).Order("created_at, id").First(&job).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		worked = true
		invitation, err = load[e.AccountInvitation](tx, job.InvitationID)
		if err != nil {
			return err
		}
		account, err = load[e.Account](tx, string(job.AccountID))
		if err != nil {
			return err
		}
		if invitation.Status != "pending" || !invitation.ExpiresAt.After(t) || job.Generation != invitation.Generation {
			job.State, job.NextRetryAt, job.LeaseUntil = "cancelled", nil, nil
			return saveInvitationDelivery(ctx, tx, &job, &invitation)
		}
		_, _, recipientErr := invitationRecipient(tx, invitation)
		if recipientErr != nil {
			var p *Problem
			if !errors.As(recipientErr, &p) {
				return recipientErr
			}
		}
		if account.Status != "active" || recipientErr != nil || job.Attempts >= s.config.InvitationMaxAttempts {
			job.State, job.FailureCode, job.NextRetryAt, job.LeaseUntil = "failed", "delivery-unavailable", nil, nil
			return saveInvitationDelivery(ctx, tx, &job, &invitation)
		}
		token = secret()
		// A reclaimed or retried attempt invalidates the previous attempt's link.
		if err = tx.Where("owner_id = ? AND kind = 'invitation'", invitation.ID).Delete(&credential{}).Error; err != nil {
			return err
		}
		if err = tx.Where("invitation_id = ?", invitation.ID).Delete(&invitationContinuation{}).Error; err != nil {
			return err
		}
		invitation.Generation++
		job.Generation = invitation.Generation
		if err = tx.Model(&invitation).Update("generation", invitation.Generation).Error; err != nil {
			return err
		}
		if err = tx.Create(&credential{ID: newID(), OwnerID: invitation.ID, Kind: "invitation", Verifier: digest(token)}).Error; err != nil {
			return err
		}
		job.Attempts++
		job.State, job.Claim, job.LastAttemptAt, job.NextRetryAt, job.FailureCode = "sending", newID(), &t, nil, ""
		lease := t.Add(time.Minute)
		job.LeaseUntil = &lease
		inviter = "Account administrator"
		if invitation.Actor.Scope == e.ScopeSystem {
			inviter = "Service administrator"
		} else if u, err := load[user](tx, invitation.Actor.UserID); err == nil {
			inviter = u.Email
		}
		return tx.Save(&job).Error
	})
	if err != nil || token == "" {
		return worked, err
	}
	target := strings.TrimRight(s.config.Issuer, "/") + "/oauth/invitations/start?" + url.Values{"invitation_id": {invitation.ID}, "token": {token}}.Encode()
	message := renderMail(s.config, "invitation", identitymail.Data{Inviter: inviter, Account: account.Name, Role: invitation.Role, Expires: invitation.ExpiresAt.Format(time.RFC3339), URL: target})
	body := message.Text
	sendCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	sendErr := s.config.SendIdentityMail(sendCtx, invitation.Email, message.Subject, body)
	cancel()
	// If shutdown prevents this update, lease recovery resumes the durable job.
	err = mutation(ctx, s.db, func(tx *gorm.DB) error {
		var current invitationDelivery
		if err := tx.Where("id = ? AND state = 'sending' AND claim = ? AND generation = ?", job.ID, job.Claim, job.Generation).First(&current).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return err
		}
		i, err := load[e.AccountInvitation](tx, job.InvitationID)
		if err != nil {
			return err
		}
		if i.Generation != job.Generation || i.Status != "pending" {
			return nil
		}
		t, err := now(tx)
		if err != nil {
			return err
		}
		current.LeaseUntil = nil
		if sendErr == nil {
			current.State, current.SentAt = "sent", &t
		} else {
			current.FailureCode = "delivery-unconfirmed"
			current.State = "failed"
			if current.Attempts < s.config.InvitationMaxAttempts {
				delay := time.Minute
				if current.Attempts > 1 {
					delay = 5 * time.Minute
				}
				next := t.Add(delay)
				if next.Before(i.ExpiresAt) {
					current.State, current.NextRetryAt = "queued", &next
				}
			}
		}
		return saveInvitationDelivery(ctx, tx, &current, &i)
	})
	return worked, err
}

func saveInvitationDelivery(ctx context.Context, tx *gorm.DB, job *invitationDelivery, i *e.AccountInvitation) error {
	if err := tx.Save(job).Error; err != nil {
		return err
	}
	actor := WithPrincipal(ctx, i.Actor)
	if err := record(actor, tx, i, "invitation-email-"+job.State, i.ID); err != nil {
		return err
	}
	if i.Actor.Scope == e.ScopeSystem {
		outcome := "succeeded"
		if job.FailureCode != "" {
			outcome = "failed"
		}
		// Worker attribution survives expiration or removal of the initiating session.
		event := e.SystemActivity{SystemRecord: newSystemRecord(actor, "recorded"), TargetAccountID: i.AccountID, Kind: "audit", Action: "invitation-email-" + job.State, TargetID: i.ID, Outcome: outcome, SessionScope: e.ScopeSystem, ChangeIDs: []string{}}
		return tx.Create(&event).Error
	}
	return nil
}
