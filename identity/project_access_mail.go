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

type projectAccessMail struct {
	ID            string      `gorm:"primaryKey"`
	AccountID     e.AccountID `gorm:"index"`
	MembershipID  string
	GrantRevision int64
	CreatedAt     time.Time
	State         string `gorm:"index"`
	Claim         string
	Attempts      int
	NextAttemptAt time.Time
	LeaseUntil    *time.Time
}

func (p *projectMembershipsService) grant(ctx context.Context, membership e.ProjectMembership, createOnly bool) (out e.ProjectMembership, created bool, err error) {
	err = mutation(ctx, p.db, func(tx *gorm.DB) error {
		previousStatus := ""
		if membership.ID != "" {
			old, err := load[e.ProjectMembership](tx, membership.ID)
			if err != nil {
				return err
			}
			previousStatus = old.Status
		}
		child := context.WithValue(ctx, databaseKey, tx)
		var err error
		if createOnly {
			out, err = create(child, tx, membership)
			created = err == nil
		} else {
			out, created, err = upsert(child, tx, membership)
		}
		if err != nil {
			return err
		}
		if out.Status != "active" {
			return tx.Model(&projectAccessMail{}).Where("membership_id = ? AND state IN ('queued','sending')", out.ID).Update("state", "cancelled").Error
		}
		if p.config.ProjectAccessEmailEnabled && p.config.SendIdentityMail != nil && out.Status == "active" && previousStatus != "active" && out.UserID != principal(ctx).UserID {
			t, err := now(tx)
			if err != nil {
				return err
			}
			// A later regrant replaces an unsent notification for the same membership.
			if err = tx.Model(&projectAccessMail{}).Where("membership_id = ? AND state IN ('queued','sending')", out.ID).Update("state", "cancelled").Error; err != nil {
				return err
			}
			return tx.Create(&projectAccessMail{ID: newID(), AccountID: out.AccountID, MembershipID: out.ID, GrantRevision: out.Revision, CreatedAt: t, State: "queued", NextAttemptAt: t}).Error
		}
		return nil
	})
	return
}

func (s *Service) RunProjectAccessMailWorker(ctx context.Context) {
	if !s.config.ProjectAccessEmailEnabled || s.config.SendIdentityMail == nil {
		return
	}
	ticker := time.NewTicker(s.config.InvitationWorkerInterval)
	defer ticker.Stop()
	for {
		for range 32 {
			worked, err := s.runWorker(ctx, ProjectAccessMailWorker, s.ProcessProjectAccessMail)
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

func (s *Service) ProcessProjectAccessMail(ctx context.Context) (worked bool, err error) {
	if !s.config.ProjectAccessEmailEnabled || s.config.SendIdentityMail == nil {
		return false, nil
	}
	var job projectAccessMail
	var recipient, body string
	err = mutation(ctx, s.db, func(tx *gorm.DB) error {
		t, err := now(tx)
		if err != nil {
			return err
		}
		err = tx.Where("(state = 'queued' AND next_attempt_at <= ?) OR (state = 'sending' AND lease_until <= ?)", t, t).Order("created_at,id").First(&job).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		worked = true
		member, err := load[e.ProjectMembership](tx, job.MembershipID)
		if err != nil {
			return err
		}
		project, err := load[e.Project](tx, string(member.ProjectID))
		if err != nil {
			return err
		}
		account, err := load[e.Account](tx, string(job.AccountID))
		if err != nil {
			return err
		}
		user, err := load[user](tx, member.UserID)
		if err != nil {
			return err
		}
		var count int64
		if err = tx.Model(&e.AccountMembership{}).Where("account_id = ? AND user_id = ? AND status = 'active'", job.AccountID, member.UserID).Count(&count).Error; err != nil {
			return err
		}
		if member.Status != "active" || account.Status != "active" || user.Status != "active" || count == 0 || project.Status == "archived" {
			job.State = "cancelled"
			return tx.Save(&job).Error
		}
		if job.Attempts >= s.config.InvitationMaxAttempts {
			job.State = "failed"
			return tx.Save(&job).Error
		}
		job.Attempts++
		job.State = "sending"
		job.Claim = newID()
		lease := t.Add(time.Minute)
		job.LeaseUntil = &lease
		target := strings.TrimRight(s.config.Issuer, "/") + "/project-access?" + url.Values{"account": {string(job.AccountID)}, "project": {project.ID}}.Encode()
		recipient = user.Email
		body = renderMail(s.config, "project-access", identitymail.Data{Project: project.Name, Account: account.Name, URL: target}).Text
		return tx.Save(&job).Error
	})
	if err != nil || recipient == "" {
		return worked, err
	}
	sendCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	sendErr := s.config.SendIdentityMail(sendCtx, recipient, "Project access granted on "+s.config.ProductName, body)
	cancel()
	err = mutation(ctx, s.db, func(tx *gorm.DB) error {
		var current projectAccessMail
		if err := tx.Where("id = ? AND state = 'sending' AND claim = ?", job.ID, job.Claim).First(&current).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return err
		}
		current.LeaseUntil = nil
		if sendErr == nil {
			current.State = "sent"
		} else {
			current.State = "failed"
			if current.Attempts < s.config.InvitationMaxAttempts {
				current.State = "queued"
				t, err := now(tx)
				if err != nil {
					return err
				}
				delay := time.Minute
				if current.Attempts > 1 {
					delay = 5 * time.Minute
				}
				current.NextAttemptAt = t.Add(delay)
			}
		}
		return tx.Save(&current).Error
	})
	return worked, err
}
