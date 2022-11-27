// Package mail defines the interfaces for sending email
// messages.
package mail

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/smtp"
	"os"
	"path/filepath"

	"gitlab.com/NebulousLabs/errors"
)

// configFilename is the name of the mail client configuration
// file.
const configFilename = "mail.json"

// MailSender is an abstraction of a mail client.
type MailSender interface {
	SendHTML(to, subject string, body *bytes.Buffer) error
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
func (mc *mailClient) SendHTML(to, subject string, body *bytes.Buffer) error {
	// Authenticate.
	auth := smtp.PlainAuth("", mc.from, mc.password, mc.smtpHost)

	// Prepare the message.
	rec := []string{to}
	var b bytes.Buffer
	mimeHeaders := "MIME-version: 1.0;\nContent-Type: text/html; charset=\"UTF-8\";\n\n"
	_, err := b.Write([]byte(fmt.Sprintf("%s\n%s\n\n", subject, mimeHeaders)))
	if err != nil {
		return errors.AddContext(err, "unable to write the message header")
	}
	_, err = b.Write(body.Bytes())
	if err != nil {
		return errors.AddContext(err, "unable to write the message body")
	}

	// Send the email.
  return smtp.SendMail(mc.smtpHost+":"+mc.smtpPort, auth, mc.from, rec, b.Bytes())
}

// New returns an initialized mail client.
func New(configPath string) (MailSender, error) {
	// Open the configuration file.
	path := filepath.Join(configPath, configFilename)
	config, err := os.Open(path)
	if err != nil {
		return nil, errors.AddContext(err, "unable to open config file")
	}
	defer func() {
		err = errors.Compose(err, config.Close())
	}()

	// Read the client type.
	var typ string
	dec := json.NewDecoder(config)
	if err := dec.Decode(&typ); err != nil {
		return nil, errors.AddContext(err, "unable to parse client type")
	}

	// Initialize the client.
	switch typ {
		case "smtp":
			var data smtpConfigData
			if err := dec.Decode(&data); err != nil {
				return nil, errors.AddContext(err, "unable to read the configuration")
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
