package identity

import identitymail "github.com/sre-norns/wyrd/identity/mail"

func renderMail(c *Config, name string, d identitymail.Data) identitymail.Message {
	d.ProductName = c.ProductName
	d.Footer = c.MailFooter
	m, err := identitymail.Render(name, d)
	if err != nil {
		panic(err)
	}
	return m
}
