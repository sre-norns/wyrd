package mail

import (
	"bytes"
	"context"
	"embed"
	"fmt"
	html "html/template"
	"text/template"
)

//go:embed templates/*
var templates embed.FS

type Data struct{ Subject, ProductName, Instruction, URL, Inviter, Account, Project, Role, Expires, Provider, Footer string }
type Message struct{ Subject, Text, HTML string }

// Mailer permits product notifications to use the same configured transport.
type Mailer interface {
	Send(context.Context, string, Message) error
}
type Sender func(context.Context, string, string, string) error

func (s Sender) Send(ctx context.Context, to string, m Message) error {
	return s(ctx, to, m.Subject, m.Text)
}

// Render produces text and HTML from embedded templates. HTML values are escaped.
func Render(name string, d Data) (Message, error) {
	var m Message
	source, err := templates.ReadFile("templates/" + name + ".txt")
	if err != nil {
		return m, fmt.Errorf("unknown mail template: %s", name)
	}
	t, err := template.New(name).Parse(string(source))
	if err != nil {
		return m, err
	}
	var subject, body bytes.Buffer
	if err = t.ExecuteTemplate(&subject, "subject", d); err != nil {
		return m, err
	}
	if err = t.ExecuteTemplate(&body, "body", d); err != nil {
		return m, err
	}
	source, err = templates.ReadFile("templates/" + name + ".html")
	if err != nil {
		return m, err
	}
	h, err := html.New(name).Parse(string(source))
	if err != nil {
		return m, err
	}
	var rich bytes.Buffer
	if err = h.Execute(&rich, d); err != nil {
		return m, err
	}
	return Message{Subject: subject.String(), Text: body.String(), HTML: rich.String()}, nil
}
