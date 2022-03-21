package mail

import (
	"fmt"
	"net/smtp"
	"strings"
	"time"
)

type MailBody struct {
	Name     string
	Resource string
	Value    float64
	ID       int64
}

type MailObject struct {
	ServerAddress string
	From          string
	Subject       string
	Body          string
	To            string
}

func FormatEpoch(epoch int64) string {
	if epoch == 0 {
		return ""
	}
	return fmt.Sprintf("%v", time.Unix(epoch, 0))
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

func CreateSubject() string {
	return "Kingraptor - High Resource Utilization Notification!"
}

func CreateBody(mailBodies []MailBody) string {
	body := ""
	for _, mailItem := range mailBodies {
		body += "     NE Name: " + mailItem.Name + "\n" +
			"     Resource: " + mailItem.Resource + "\n" +
			"     Value: " + fmt.Sprintf("%v", mailItem.Value) + "\n" +
			"     Date & time: " + fmt.Sprintf("%v", FormatEpoch(mailItem.ID)) + "\n" +
			"     =========================================================================\n"
	}
	return body
}
