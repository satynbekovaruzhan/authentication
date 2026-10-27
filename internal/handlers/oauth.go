package handlers

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"

	"forum-authentication/internal/repository"
)

type oauthProvider struct{ Name, ClientID, ClientSecret, AuthURL, TokenURL, UserURL string }

func providers() map[string]oauthProvider {
	return map[string]oauthProvider{
		"google": {"google", os.Getenv("GOOGLE_CLIENT_ID"), os.Getenv("GOOGLE_CLIENT_SECRET"), "https://accounts.google.com/o/oauth2/v2/auth", "https://oauth2.googleapis.com/token", "https://openidconnect.googleapis.com/v1/userinfo"},
		"github": {"github", os.Getenv("GITHUB_CLIENT_ID"), os.Getenv("GITHUB_CLIENT_SECRET"), "https://github.com/login/oauth/authorize", "https://github.com/login/oauth/access_token", "https://api.github.com/user"},
	}
}

func (h *Handler) OAuthStart(w http.ResponseWriter, r *http.Request) {
	p, ok := providers()[r.PathValue("provider")]
	if !ok {
		http.Error(w, "unsupported OAuth provider", http.StatusNotFound)
		return
	}
	if p.ClientID == "" || p.ClientSecret == "" {
		http.Error(w, fmt.Sprintf("%s OAuth is not configured: set %s_CLIENT_ID and %s_CLIENT_SECRET", p.Name, strings.ToUpper(p.Name), strings.ToUpper(p.Name)), http.StatusNotImplemented)
		return
	}
	stateBytes := make([]byte, 32)
	if _, err := rand.Read(stateBytes); err != nil {
		http.Error(w, "server error", 500)
		return
	}
	state := base64.RawURLEncoding.EncodeToString(stateBytes)
	http.SetCookie(w, &http.Cookie{Name: "oauth_state", Value: state, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: 600})
	callback := callbackURL(r, p.Name)
	query := url.Values{"client_id": {p.ClientID}, "redirect_uri": {callback}, "response_type": {"code"}, "state": {state}}
	if p.Name == "google" {
		query.Set("scope", "openid email profile")
		query.Set("access_type", "online")
	} else {
		query.Set("scope", "read:user user:email")
		query.Set("prompt", "select_account")
	}
	http.Redirect(w, r, p.AuthURL+"?"+query.Encode(), http.StatusFound)
}

func (h *Handler) OAuthCallback(w http.ResponseWriter, r *http.Request) {
	p, ok := providers()[r.PathValue("provider")]
	if !ok {
		http.NotFound(w, r)
		return
	}
	stateCookie, err := r.Cookie("oauth_state")
	if err != nil || stateCookie.Value == "" || stateCookie.Value != r.URL.Query().Get("state") {
		http.Error(w, "invalid OAuth state", http.StatusBadRequest)
		return
	}
	if oauthErr := r.URL.Query().Get("error"); oauthErr != "" {
		http.Error(w, "OAuth denied: "+oauthErr, http.StatusUnauthorized)
		return
	}
	accessToken, err := exchangeCode(r.Context(), p, r.URL.Query().Get("code"), callbackURL(r, p.Name))
	if err != nil {
		log.Printf("oauth exchange: %v", err)
		http.Error(w, "OAuth exchange failed", 502)
		return
	}
	identity, err := fetchIdentity(r.Context(), p, accessToken)
	if err != nil {
		log.Printf("oauth identity: %v", err)
		http.Error(w, "OAuth identity failed", 502)
		return
	}
	user, err := h.Repo.UserByOAuth(p.Name, identity.ID)
	if err == repository.ErrNotFound {
		if identity.Email == "" {
			http.Error(w, "provider did not return an email", 422)
			return
		}
		username := sanitizeUsername(identity.Username) + "_" + p.Name
		base := username
		for suffix := 1; ; suffix++ {
			exists, checkErr := h.Repo.UsernameExists(username)
			if checkErr != nil {
				http.Error(w, "account lookup failed", 500)
				return
			}
			if !exists {
				break
			}
			username = fmt.Sprintf("%s_%d", base, suffix)
		}
		id, createErr := h.Repo.CreateOAuthUser(repository.OAuthAccount{Provider: p.Name, ProviderID: identity.ID, Email: identity.Email, Username: username})
		if createErr != nil {
			log.Printf("oauth create user: %v", createErr)
			http.Error(w, "account creation failed", 500)
			return
		}
		user, err = h.Repo.UserByID(id)
	}
	if err != nil {
		log.Printf("oauth account: %v", err)
		http.Error(w, "authentication failed", 500)
		return
	}
	if err := h.createSession(w, user.ID); err != nil {
		http.Error(w, "session creation failed", 500)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "oauth_state", Value: "", Path: "/", MaxAge: -1})
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

type oauthIdentity struct{ ID, Email, Username string }

func exchangeCode(ctx context.Context, p oauthProvider, code, redirectURI string) (string, error) {
	form := url.Values{"client_id": {p.ClientID}, "client_secret": {p.ClientSecret}, "code": {code}, "redirect_uri": {redirectURI}}
	if p.Name == "google" {
		form.Set("grant_type", "authorization_code")
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, p.TokenURL, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var data struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return "", err
	}
	if resp.StatusCode >= 300 || data.AccessToken == "" {
		return "", fmt.Errorf("token endpoint returned %s", resp.Status)
	}
	return data.AccessToken, nil
}
func fetchIdentity(ctx context.Context, p oauthProvider, token string) (oauthIdentity, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, p.UserURL, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	if p.Name == "github" {
		req.Header.Set("Authorization", "token "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return oauthIdentity{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return oauthIdentity{}, fmt.Errorf("identity endpoint returned %s", resp.Status)
	}
	var raw map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return oauthIdentity{}, err
	}
	id, _ := raw["sub"].(string)
	email, _ := raw["email"].(string)
	username, _ := raw["name"].(string)
	if p.Name == "github" {
		id = fmt.Sprint(raw["id"])
		username, _ = raw["login"].(string)
		if email == "" {
			email, _ = raw["email"].(string)
		}
	}
	return oauthIdentity{ID: id, Email: email, Username: username}, nil
}
func callbackURL(r *http.Request, provider string) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + r.Host + "/auth/" + provider + "/callback"
}
func sanitizeUsername(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	for _, c := range value {
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '_' || c == '-' {
			b.WriteRune(c)
		}
	}
	if b.Len() == 0 {
		return "user"
	}
	return b.String()
}
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
