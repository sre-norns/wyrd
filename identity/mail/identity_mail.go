package mail

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/mail"
	"net/smtp"
	"os"
	"strings"
	"time"
)

type Config struct {
	Provider             string
	MailgunDomain        string
	MailgunAPIKey        string
	MailgunRegion        string
	SMTPAddress          string
	SMTPUsername         string
	SMTPPassword         string
	From                 string
	DevelopmentDirectory string
	Development          bool
}

func New(cfg Config) (func(context.Context, string, string, string) error, error) {
	provider, err := Provider(cfg)
	if err != nil {
		return nil, err
	}
	if provider == "mailgun" {
		return NewMailgun(cfg, nil)
	}
	if cfg.DevelopmentDirectory != "" {
		if !cfg.Development || cfg.SMTPAddress != "" {
			return nil, fmt.Errorf("mail directory requires development mode and cannot be combined with SMTP")
		}
		if err := os.MkdirAll(cfg.DevelopmentDirectory, 0700); err != nil {
			return nil, err
		}
		info, err := os.Stat(cfg.DevelopmentDirectory)
		if err != nil {
			return nil, err
		}
		if !info.IsDir() || info.Mode().Perm()&0077 != 0 {
			return nil, fmt.Errorf("development mail directory must be private (0700)")
		}
		return func(ctx context.Context, to, subject, body string) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			file, err := os.CreateTemp(cfg.DevelopmentDirectory, "account-*.eml")
			if err != nil {
				return err
			}
			defer file.Close()
			_, err = fmt.Fprintf(file, "To: %s\nSubject: %s\n\n%s", to, subject, body)
			return err
		}, nil
	}
	if cfg.SMTPAddress == "" {
		if cfg.SMTPUsername != "" || cfg.SMTPPassword != "" || cfg.From != "" {
			return nil, fmt.Errorf("SMTP settings require an SMTP address")
		}
		return nil, nil
	}
	host, _, err := net.SplitHostPort(cfg.SMTPAddress)
	if err != nil || host == "" {
		return nil, fmt.Errorf("SMTP address must contain host and port")
	}
	from, err := mail.ParseAddress(cfg.From)
	if err != nil || from.Address != cfg.From || strings.ContainsAny(cfg.From, "\r\n") {
		return nil, fmt.Errorf("SMTP sender must be an email address")
	}
	if (cfg.SMTPUsername == "") != (cfg.SMTPPassword == "") {
		return nil, fmt.Errorf("SMTP username and password must be supplied together")
	}
	return func(ctx context.Context, to, subject, body string) error {
		dialer := net.Dialer{Timeout: 10 * time.Second}
		conn, err := dialer.DialContext(ctx, "tcp", cfg.SMTPAddress)
		if err != nil {
			return err
		}
		defer conn.Close()
		deadline := time.Now().Add(20 * time.Second)
		if limit, ok := ctx.Deadline(); ok && limit.Before(deadline) {
			deadline = limit
		}
		if err := conn.SetDeadline(deadline); err != nil {
			return err
		}
		client, err := smtp.NewClient(conn, host)
		if err != nil {
			return err
		}
		defer client.Close()
		if err := client.StartTLS(&tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}); err != nil {
			return err
		}
		if cfg.SMTPUsername != "" {
			if err := client.Auth(smtp.PlainAuth("", cfg.SMTPUsername, cfg.SMTPPassword, host)); err != nil {
				return err
			}
		}
		if err := client.Mail(cfg.From); err != nil {
			return err
		}
		if err := client.Rcpt(to); err != nil {
			return err
		}
		writer, err := client.Data()
		if err != nil {
			return err
		}
		message := "From: " + cfg.From + "\r\nTo: " + to + "\r\nSubject: " + subject + "\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n" + strings.ReplaceAll(body, "\n", "\r\n")
		if _, err := writer.Write([]byte(message)); err != nil {
			return err
		}
		if err := writer.Close(); err != nil {
			return err
		}
		return client.Quit()
	}, nil
}

// Provider selects the mail backend. Auto selection preserves configurations created before explicit providers existed.
func Provider(cfg Config) (string, error) {
	smtpSet := cfg.SMTPAddress != "" || cfg.SMTPUsername != "" || cfg.SMTPPassword != ""
	mailgunSet := cfg.MailgunDomain != "" || cfg.MailgunAPIKey != "" || (cfg.MailgunRegion != "" && cfg.MailgunRegion != "us")
	developmentSet := cfg.DevelopmentDirectory != ""
	configured := 0
	for _, enabled := range []bool{smtpSet, mailgunSet, developmentSet} {
		if enabled {
			configured++
		}
	}
	if configured > 1 {
		return "", fmt.Errorf("configure only one account mail provider: SMTP, Mailgun, or development")
	}
	provider := cfg.Provider
	if provider == "" || provider == "auto" {
		switch {
		case smtpSet:
			provider = "smtp"
		case mailgunSet:
			provider = "mailgun"
		case developmentSet:
			provider = "development"
		default:
			provider = ""
		}
	}
	switch provider {
	case "":
		if cfg.From != "" {
			return "", fmt.Errorf("mail sender requires a configured provider")
		}
	case "smtp":
		if !smtpSet || mailgunSet || developmentSet {
			return "", fmt.Errorf("SMTP provider requires SMTP settings only")
		}
	case "mailgun":
		if smtpSet || developmentSet {
			return "", fmt.Errorf("email provider Mailgun cannot use SMTP or development settings")
		}
	case "development":
		if !developmentSet || smtpSet || mailgunSet {
			return "", fmt.Errorf("development provider requires a mail directory only")
		}
	default:
		return "", fmt.Errorf("unsupported account mail provider")
	}
	return provider, nil
}
