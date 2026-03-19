package services

import (
	"bytes"
	"fmt"
	"html/template"
	"net/smtp"
	"os"
	"path/filepath"
	"strings"

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

// TransactionEmailData wraps transaction data for email template
type TransactionEmailData struct {
	models.Transaction
	UserEmail    string
	Asset        string // Clean asset name (USDC, SOL, ETH, etc.)
	Chain        string // Solana, Base
	ExplorerLink string
	ExplorerName string
	Greeting     string
}

// getAssetAndChain extracts clean asset name and chain from currency code
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
	case "demo_naira", "naira":
		return "NGN", "Fiat"
	default:
		// Fallback: try to strip chain suffix
		if strings.HasSuffix(currency, "_sol") {
			asset = strings.ToUpper(strings.TrimSuffix(currency, "_sol"))
			chain = "Solana"
		} else if strings.HasSuffix(currency, "_base") {
			asset = strings.ToUpper(strings.TrimSuffix(currency, "_base"))
			chain = "Base"
		} else {
			asset = strings.ToUpper(currency)
			chain = "Unknown"
		}
		return asset, chain
	}
}

// getExplorerLink builds the blockchain explorer link
func getExplorerLink(txHash, chain string) (link, name string) {
	chain = strings.ToLower(chain)
	if strings.Contains(chain, "solana") {
		return "https://solscan.io/tx/" + txHash, "Solscan"
	} else if strings.Contains(chain, "base") {
		return "https://basescan.org/tx/" + txHash, "Basescan"
	}
	return "", ""
}

// generateGreeting creates personalized greeting
func generateGreeting(email, txType, asset string) string {
	// Extract name from email (before @)
	localPart := strings.Split(email, "@")[0]
	
	// Capitalize first letter
	name := strings.Title(strings.ReplaceAll(localPart, ".", " "))
	
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
	
	return fmt.Sprintf("Hey %s, you've just %s %s into your Vant wallet. Log in to your Vant account to explore trading opportunities and express your beliefs in crypto assets and more. Check out the details of your deposit below.", name, action, asset)
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
