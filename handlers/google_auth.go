package handlers

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/vant-xyz/backend-code/db"
	"github.com/vant-xyz/backend-code/services"
	"github.com/vant-xyz/backend-code/utils"
)

func GoogleLogin(c *gin.Context) {
	state := utils.RandomAlphanumeric(16)
	c.SetCookie("oauth_state", state, 600, "/", "", false, true)

	params := url.Values{}
	params.Set("client_id", os.Getenv("GOOGLE_CLIENT_ID"))
	params.Set("redirect_uri", os.Getenv("GOOGLE_REDIRECT_URI"))
	params.Set("response_type", "code")
	params.Set("scope", "openid email profile")
	params.Set("state", state)
	params.Set("access_type", "online")

	c.Redirect(http.StatusTemporaryRedirect, "https://accounts.google.com/o/oauth2/v2/auth?"+params.Encode())
}

func GoogleCallback(c *gin.Context) {
	frontendURL := os.Getenv("FRONTEND_URL")
	if frontendURL == "" {
		frontendURL = "http://localhost:3000"
	}

	fail := func(reason string) {
		c.Redirect(http.StatusTemporaryRedirect, frontendURL+"/auth/callback?error="+url.QueryEscape(reason))
	}

	state, _ := c.Cookie("oauth_state")
	if state == "" || state != c.Query("state") {
		fail("invalid_state")
		return
	}

	code := c.Query("code")
	if code == "" {
		fail("missing_code")
		return
	}

	accessToken, err := exchangeGoogleCode(code)
	if err != nil {
		log.Printf("[GoogleCallback] token exchange: %v", err)
		fail("token_exchange_failed")
		return
	}

	info, err := fetchGoogleUserInfo(accessToken)
	if err != nil {
		log.Printf("[GoogleCallback] userinfo: %v", err)
		fail("userinfo_failed")
		return
	}

	user, err := db.GetUserByEmail(c.Request.Context(), info.Email)
	if err != nil {
		if !strings.Contains(err.Error(), "user not found:") {
			log.Printf("[GoogleCallback] get user by email: %v", err)
			fail("account_lookup_failed")
			return
		}
		wallet, err := services.GenerateWallet(info.Email)
		if err != nil {
			log.Printf("[GoogleCallback] generate wallet: %v", err)
			fail("wallet_failed")
			return
		}
		user, err = db.CreateOAuthUser(c.Request.Context(), info.Email, info.Name, info.Picture, "google", wallet)
		if err != nil {
			log.Printf("[GoogleCallback] create oauth user: %v", err)
			fail("account_error")
			return
		}
		go func(email, sol, base string) {
			if err := services.NotifyIndexerWhitelist(email, sol, base); err != nil {
				log.Printf("[GoogleCallback] indexer notify: %v", err)
			}
		}(user.Email, wallet.SolPublicKey, wallet.BasePublicKey)
	}

	jwt, err := services.GenerateJWT(user.Email)
	if err != nil {
		fail("token_failed")
		return
	}

	c.Redirect(http.StatusTemporaryRedirect, frontendURL+"/auth/callback?token="+jwt)
}

type googleTokenResp struct {
	AccessToken string `json:"access_token"`
}

type googleUserInfo struct {
	Email   string `json:"email"`
	Name    string `json:"name"`
	Picture string `json:"picture"`
}

func exchangeGoogleCode(code string) (string, error) {
	body := url.Values{}
	body.Set("code", code)
	body.Set("client_id", os.Getenv("GOOGLE_CLIENT_ID"))
	body.Set("client_secret", os.Getenv("GOOGLE_CLIENT_SECRET"))
	body.Set("redirect_uri", os.Getenv("GOOGLE_REDIRECT_URI"))
	body.Set("grant_type", "authorization_code")

	resp, err := http.PostForm("https://oauth2.googleapis.com/token", body)
	if err != nil {
		return "", fmt.Errorf("POST token: %w", err)
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("token endpoint %d: %s", resp.StatusCode, data)
	}

	var tok googleTokenResp
	if err := json.Unmarshal(data, &tok); err != nil {
		return "", fmt.Errorf("unmarshal token: %w", err)
	}
	return tok.AccessToken, nil
}

func fetchGoogleUserInfo(accessToken string) (*googleUserInfo, error) {
	req, _ := http.NewRequest("GET", "https://www.googleapis.com/oauth2/v3/userinfo", nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET userinfo: %w", err)
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("userinfo %d: %s", resp.StatusCode, data)
	}

	var info googleUserInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return nil, fmt.Errorf("unmarshal userinfo: %w", err)
	}
	return &info, nil
}
