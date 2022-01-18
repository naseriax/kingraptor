package mail

import (
	"fmt"
	"net/smtp"
	"strings"
)

type MailObject struct {
	ServerAddress string
	From          string
	Subject       string
	Body          string
	To            string
}

func MailConstructor(addr, from, subject, body, to string) error {
	m := MailObject{
		ServerAddress: addr,
		From:          from,
		Subject:       subject,
		Body:          body,
		To:            to,
	}
	err := m.SendMail()
	if err != nil {
		return err
	}

	return nil
}

func (m *MailObject) ComposeMessage() string {
	return "To: " + m.To + "\r\n" +
		"From: " + m.From + "\r\n" +
		"Subject: " + m.Subject + "\r\n" +
		"Content-Type: text/plain; charset=\"UTF-8\"\r\n" +
		"Content-Transfer-Encoding: 8bit\r\n" +
		"\r\n" + m.Body + "\r\n"
}

func (m *MailObject) ConnectToMailServer() (*smtp.Client, error) {
	r := strings.NewReplacer("\r\n", "", "\r", "", "\n", "", "%0a", "", "%0d", "")

	c, err := smtp.Dial(m.ServerAddress)
	if err != nil {
		return nil, err
	}

	if err = c.Mail(r.Replace(m.From)); err != nil {
		return nil, err
	}

	m.To = r.Replace(m.To)
	if err = c.Rcpt(m.To); err != nil {
		return nil, err
	}
	return c, nil
}

func (m *MailObject) SendMail() error {
	c, err := m.ConnectToMailServer()
	if err != nil {
		return err
	}
	defer c.Close()

	w, err := c.Data()
	if err != nil {
		return err
	}

	msg := m.ComposeMessage()

	_, err = w.Write([]byte(msg))
	if err != nil {
		return err
	}

	err = w.Close()
	if err != nil {
		return err
	}

	return c.Quit()
}

func CreateSubject(name, resource string, value float64, method string) string {
	if method == "single" {
		return fmt.Sprintf("Kingraptor - High %v Utilization (%v) (30 Minutes period): %v", resource, value, name)
	} else {
		return "Kingraptor - High disk Utilization"
	}
}

func CreateBody(name, resource string, value float64, method string) string {
	body := ""
	if method == "single" {
		body += "     NE Name: " + name + "\n" +
			"     Resource: " + resource + "\n" +
			"     Value: " + fmt.Sprintf("%v", value) + "\n" +
			"     =========================================================================\n"
	}
	return body
}
