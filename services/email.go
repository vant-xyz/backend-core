package services

import (
	"bytes"
	"fmt"
	"html/template"
	"log"
	"net/smtp"
	"os"
	"path/filepath"
	"strings"

	"github.com/vant-xyz/backend-code/models"
)

func SendWaitlistEmail(toEmail, referralCode string) error {
	from := os.Getenv("MAIL_GMAIL")
	password := os.Getenv("MAIL_APP_PASSWORD")

	if from == "" || password == "" {
		return fmt.Errorf("email credentials not configured (MAIL_GMAIL or MAIL_APP_PASSWORD is empty)")
	}

	smtpHost := "smtp.gmail.com"
	smtpPort := "587"

	tmplPath := filepath.Join("templates", "waitlist.html")
	tmpl, err := template.ParseFiles(tmplPath)
	if err != nil {
		return fmt.Errorf("failed to parse waitlist template at %s: %w", tmplPath, err)
	}

	data := struct {
		ReferralCode string
	}{
		ReferralCode: referralCode,
	}

	var body bytes.Buffer
	mimeHeaders := "MIME-version: 1.0;\nContent-Type: text/html; charset=\"UTF-8\";\n\n"
	body.Write([]byte(fmt.Sprintf("Subject: You're on the Vant waitlist! \n%s\n\n", mimeHeaders)))

	if err = tmpl.Execute(&body, data); err != nil {
		return fmt.Errorf("failed to execute waitlist template: %w", err)
	}

	auth := smtp.PlainAuth("", from, password, smtpHost)
	if err = smtp.SendMail(smtpHost+":"+smtpPort, auth, from, []string{toEmail}, body.Bytes()); err != nil {
		return fmt.Errorf("failed to send waitlist email to %s: %w", toEmail, err)
	}

	return nil
}

// TransactionEmailData wraps transaction data for email template
type TransactionEmailData struct {
	models.Transaction
	UserEmail    string
	Asset        string
	Chain        string
	ExplorerLink string
	ExplorerName string
	Greeting     string
}

func getAssetAndChain(currency string) (asset, chain string) {
	currency = strings.ToLower(currency)

	switch currency {
	case "sol":
		return "SOL", "Solana"
	case "eth_base":
		return "ETH", "Base"
	case "usdc_sol":
		return "USDC", "Solana"
	case "usdc_base":
		return "USDC", "Base"
	case "usdt_sol":
		return "USDT", "Solana"
	case "usdg_sol":
		return "USDG", "Solana"
	case "demo_sol":
		return "SOL", "Solana (Demo)"
	case "demo_usdc_sol":
		return "USDC", "Solana (Demo)"
	case "demo_naira", "naira", "ngn":
		return "NGN", "Fiat"
	default:
		if strings.HasSuffix(currency, "_sol") {
			return strings.ToUpper(strings.TrimSuffix(currency, "_sol")), "Solana"
		} else if strings.HasSuffix(currency, "_base") {
			return strings.ToUpper(strings.TrimSuffix(currency, "_base")), "Base"
		}
		return strings.ToUpper(currency), "Unknown"
	}
}

func getExplorerLink(txHash, chain string) (link, name string) {
	if txHash == "" {
		return "", ""
	}
	chain = strings.ToLower(chain)
	if strings.Contains(chain, "solana") {
		return "https://solscan.io/tx/" + txHash, "Solscan"
	} else if strings.Contains(chain, "base") {
		return "https://basescan.org/tx/" + txHash, "Basescan"
	}
	return "", ""
}

func generateGreeting(email, txType, asset string) string {
	localPart := strings.Split(email, "@")[0]
	// Capitalize first letter only — strings.Title is deprecated
	name := localPart
	if len(localPart) > 0 {
		name = strings.ToUpper(localPart[:1]) + localPart[1:]
	}
	name = strings.ReplaceAll(name, ".", " ")

	var action string
	switch txType {
	case "deposit":
		action = "received a new deposit of"
	case "sell":
		action = "sold assets for"
	case "faucet":
		action = "received demo funds of"
	case "withdrawal":
		action = "withdrew"
	case "wager":
		action = "participated in a wager for"
	default:
		action = "completed a transaction of"
	}

	return fmt.Sprintf(
		"Hey %s, you've just %s %s into your Vant wallet. Log in to your Vant account to explore trading opportunities and express your beliefs in crypto assets and more. Check out the details of your transaction below.",
		name, action, asset,
	)
}

func SendTransactionEmail(toEmail string, tx models.Transaction) error {
	log.Printf("[Email] Sending %s transaction email to %s (txID: %s)", tx.Type, toEmail, tx.ID)

	from := os.Getenv("MAIL_GMAIL")
	password := os.Getenv("MAIL_APP_PASSWORD")

	if from == "" || password == "" {
		return fmt.Errorf("[Email] credentials not configured: MAIL_GMAIL or MAIL_APP_PASSWORD is empty")
	}

	smtpHost := "smtp.gmail.com"
	smtpPort := "587"

	tmplPath := filepath.Join("templates", "transaction.html")
	log.Printf("[Email] Loading template from %s", tmplPath)

	tmpl, err := template.ParseFiles(tmplPath)
	if err != nil {
		return fmt.Errorf("[Email] failed to parse transaction template at %s: %w", tmplPath, err)
	}

	asset, chain := getAssetAndChain(tx.Currency)
	explorerLink, explorerName := getExplorerLink(tx.TxHash, chain)
	greeting := generateGreeting(toEmail, tx.Type, asset)

	data := TransactionEmailData{
		Transaction:  tx,
		UserEmail:    toEmail,
		Asset:        asset,
		Chain:        chain,
		ExplorerLink: explorerLink,
		ExplorerName: explorerName,
		Greeting:     greeting,
	}

	var body bytes.Buffer
	mimeHeaders := "MIME-version: 1.0;\nContent-Type: text/html; charset=\"UTF-8\";\n\n"
	body.Write([]byte(fmt.Sprintf("Subject: Your Vant Transaction: %s\n%s\n\n", tx.ID, mimeHeaders)))

	if err = tmpl.Execute(&body, data); err != nil {
		return fmt.Errorf("[Email] failed to execute transaction template: %w", err)
	}

	auth := smtp.PlainAuth("", from, password, smtpHost)

	log.Printf("[Email] Sending via SMTP to %s", toEmail)
	if err = smtp.SendMail(smtpHost+":"+smtpPort, auth, from, []string{toEmail}, body.Bytes()); err != nil {
		return fmt.Errorf("[Email] SMTP send failed to %s: %w", toEmail, err)
	}

	log.Printf("[Email] Successfully sent %s email to %s (txID: %s)", tx.Type, toEmail, tx.ID)
	return nil
}