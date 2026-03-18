package services

import (
	"bytes"
	"fmt"
	"html/template"
	"net/smtp"
	"os"
	"path/filepath"

	"github.com/vant-xyz/backend-code/models"
)

func SendWaitlistEmail(toEmail, referralCode string) error {
	from := os.Getenv("MAIL_GMAIL")
	password := os.Getenv("MAIL_APP_PASSWORD")
	smtpHost := "smtp.gmail.com"
	smtpPort := "587"

	tmplPath := filepath.Join("templates", "waitlist.html")
	tmpl, err := template.ParseFiles(tmplPath)
	if err != nil {
		return fmt.Errorf("failed to parse template: %v", err)
	}

	data := struct {
		ReferralCode string
	}{
		ReferralCode: referralCode,
	}

	var body bytes.Buffer
	mimeHeaders := "MIME-version: 1.0;\nContent-Type: text/html; charset=\"UTF-8\";\n\n"
	body.Write([]byte(fmt.Sprintf("Subject: You're on the Vant waitlist! \n%s\n\n", mimeHeaders)))

	err = tmpl.Execute(&body, data)
	if err != nil {
		return fmt.Errorf("failed to execute template: %v", err)
	}

	auth := smtp.PlainAuth("", from, password, smtpHost)

	err = smtp.SendMail(smtpHost+":"+smtpPort, auth, from, []string{toEmail}, body.Bytes())
	if err != nil {
		return fmt.Errorf("failed to send email: %v", err)
	}

	return nil
}

func SendTransactionEmail(toEmail string, tx models.Transaction) error {
	from := os.Getenv("MAIL_GMAIL")
	password := os.Getenv("MAIL_APP_PASSWORD")
	smtpHost := "smtp.gmail.com"
	smtpPort := "587"

	tmplPath := filepath.Join("templates", "transaction.html")
	tmpl, err := template.ParseFiles(tmplPath)
	if err != nil {
		return fmt.Errorf("failed to parse template: %v", err)
	}

	var body bytes.Buffer
	mimeHeaders := "MIME-version: 1.0;\nContent-Type: text/html; charset=\"UTF-8\";\n\n"
	body.Write([]byte(fmt.Sprintf("Subject: Your Vant Transaction: %s\n%s\n\n", tx.ID, mimeHeaders)))

	err = tmpl.Execute(&body, tx)
	if err != nil {
		return fmt.Errorf("failed to execute template: %v", err)
	}

	auth := smtp.PlainAuth("", from, password, smtpHost)

	err = smtp.SendMail(smtpHost+":"+smtpPort, auth, from, []string{toEmail}, body.Bytes())
	if err != nil {
		return fmt.Errorf("failed to send email: %v", err)
	}

	return nil
}
