package model

import "time"

type InvitationEmailDelivery struct {
	State         string     `json:"state"`
	Attempts      int        `json:"attempts"`
	LastAttemptAt *time.Time `json:"last_attempt_at,omitempty"`
	SentAt        *time.Time `json:"sent_at,omitempty"`
	NextRetryAt   *time.Time `json:"next_retry_at,omitempty"`
	FailureCode   string     `json:"failure_code,omitempty"`
}

type FirstOwnerInvitation struct {
	Email     string `json:"email"`
	Delivery  string `json:"delivery,omitempty"`
	Reason    string `json:"reason"`
	Reference string `json:"reference,omitempty"`
}

type SystemInvitationCreated struct {
	SystemInvitation
	Token string `json:"token,omitempty"`
}
