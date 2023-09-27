// Package mail defines the interfaces for sending email
// messages.
package mail

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/smtp"
	"os"
	"path/filepath"

	"github.com/mike76-dev/sia-satellite/modules"
)

// configFilename is the name of the mail client configuration
// file.
const configFilename = "mail.json"

// MailSender is an abstraction of a mail client.
type MailSender interface {
	SendMail(from, to, subject string, body *bytes.Buffer) error
}

type (
	// configData contains the mail client fields stored on disk.
	smtpConfigData struct {
		From string `json:"from"`
		Host string `json:"host"`
		Port string `json:"port"`
	}

	// MailClient contains all necesssary fields for connecting
	// to a mail client.
	mailClient struct {
		from     string
		password string
		smtpHost string
		smtpPort string
	}
)

// SendMail sends the message contained in body using HTML format.
func (mc *mailClient) SendMail(from, to, subject string, body *bytes.Buffer) error {
	// Authenticate.
	auth := smtp.PlainAuth("", mc.from, mc.password, mc.smtpHost)

	// Prepare the message.
	rec := []string{to}
	var b bytes.Buffer
	mimeHeaders := "MIME-version: 1.0;\r\nContent-Type: text/html; charset=\"UTF-8\";\r\n\r\n"
	b.Write([]byte(fmt.Sprintf("From: %s\r\n", from)))
	b.Write([]byte(fmt.Sprintf("To: %s\r\n", to)))
	b.Write([]byte(fmt.Sprintf("Subject: %s\r\n", subject)))
	b.Write([]byte(mimeHeaders))
	b.Write(body.Bytes())
	b.Write([]byte("\r\n"))

	// Send the email.
	return smtp.SendMail(mc.smtpHost+":"+mc.smtpPort, auth, mc.from, rec, b.Bytes())
}

// New returns an initialized mail client.
func New(configPath string) (MailSender, error) {
	// Open the configuration file.
	path := filepath.Join(configPath, configFilename)
	config, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("unable to open config file: %s", err)
	}
	defer func() {
		err = modules.ComposeErrors(err, config.Close())
	}()

	// Read the client type.
	var typ string
	dec := json.NewDecoder(config)
	if err := dec.Decode(&typ); err != nil {
		return nil, fmt.Errorf("unable to parse client type: %s", err)
	}

	// Initialize the client.
	switch typ {
	case "smtp":
		var data smtpConfigData
		if err := dec.Decode(&data); err != nil {
			return nil, fmt.Errorf("unable to read the configuration: %s", err)
		}
		pw := os.Getenv("SATD_MAIL_PASSWORD")
		if pw == "" {
			return nil, errors.New("could not fetch mail password")
		}
		mc := &mailClient{
			from:     data.From,
			password: pw,
			smtpHost: data.Host,
			smtpPort: data.Port,
		}
		return mc, nil
	default:
		return nil, errors.New("unsupported mail client type")
	}
}
