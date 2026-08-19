package main

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type OAuthAttempt struct {
	Verifier  string
	Next      string
	ExpiresAt time.Time
}

func (a *AuthStore) CreateOAuthAttempt(next string) (string, string, error) {
	state, err := randomToken(24)
	if err != nil {
		return "", "", err
	}
	verifier, err := randomToken(48)
	if err != nil {
		return "", "", err
	}
	if !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") {
		next = "/"
	}
	a.mu.Lock()
	for key, attempt := range a.oauth {
		if time.Now().After(attempt.ExpiresAt) {
			delete(a.oauth, key)
		}
	}
	a.oauth[state] = OAuthAttempt{Verifier: verifier, Next: next, ExpiresAt: time.Now().Add(10 * time.Minute)}
	a.mu.Unlock()
	return state, verifier, nil
}

func (a *AuthStore) ConsumeOAuthAttempt(state string) (OAuthAttempt, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	attempt, exists := a.oauth[state]
	delete(a.oauth, state)
	return attempt, exists && time.Now().Before(attempt.ExpiresAt)
}

func (server *Server) redirectURI(request *http.Request) string {
	base := server.publicBaseURL
	if base == "" {
		scheme := request.Header.Get("X-Forwarded-Proto")
		if scheme != "https" {
			scheme = "http"
		}
		base = scheme + "://" + request.Host
	}
	return strings.TrimRight(base, "/") + "/api/auth/google/callback"
}

func (server *Server) googleStart(response http.ResponseWriter, request *http.Request) {
	if server.googleClientID == "" || server.googleClientSecret == "" {
		server.oauthFailure(response, request, "Google OAuth не настроен")
		return
	}
	next := request.URL.Query().Get("next")
	state, verifier, err := server.auth.CreateOAuthAttempt(next)
	if err != nil {
		server.oauthFailure(response, request, "Не удалось начать вход")
		return
	}
	digest := sha256.Sum256([]byte(verifier))
	values := url.Values{
		"client_id":             {server.googleClientID},
		"redirect_uri":          {server.redirectURI(request)},
		"response_type":         {"code"},
		"scope":                 {"openid email profile"},
		"state":                 {state},
		"code_challenge":        {base64.RawURLEncoding.EncodeToString(digest[:])},
		"code_challenge_method": {"S256"},
		"prompt":                {"select_account"},
	}
	http.Redirect(response, request, "https://accounts.google.com/o/oauth2/v2/auth?"+values.Encode(), http.StatusFound)
}

func (server *Server) googleCallback(response http.ResponseWriter, request *http.Request) {
	if request.URL.Query().Get("error") != "" {
		server.oauthFailure(response, request, "Google отменил вход")
		return
	}
	attempt, ok := server.auth.ConsumeOAuthAttempt(request.URL.Query().Get("state"))
	if !ok {
		server.oauthFailure(response, request, "Сессия Google OAuth истекла")
		return
	}
	values := url.Values{
		"code":          {request.URL.Query().Get("code")},
		"client_id":     {server.googleClientID},
		"client_secret": {server.googleClientSecret},
		"redirect_uri":  {server.redirectURI(request)},
		"grant_type":    {"authorization_code"},
		"code_verifier": {attempt.Verifier},
	}
	tokenRequest, err := http.NewRequestWithContext(request.Context(), http.MethodPost, "https://oauth2.googleapis.com/token", strings.NewReader(values.Encode()))
	if err != nil {
		server.oauthFailure(response, request, "Не удалось завершить вход")
		return
	}
	tokenRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	tokenResponse, err := (&http.Client{Timeout: 15 * time.Second}).Do(tokenRequest)
	if err != nil {
		server.oauthFailure(response, request, "Google временно недоступен")
		return
	}
	defer tokenResponse.Body.Close()
	var payload struct {
		IDToken string `json:"id_token"`
	}
	decodeError := json.NewDecoder(io.LimitReader(tokenResponse.Body, 1<<20)).Decode(&payload)
	if tokenResponse.StatusCode != http.StatusOK || decodeError != nil || payload.IDToken == "" {
		server.oauthFailure(response, request, "Google не подтвердил вход")
		return
	}
	claims, err := server.auth.verifier.Verify(request.Context(), payload.IDToken)
	if err != nil {
		server.logger.Warn("google oauth rejected", "error", err)
		server.oauthFailure(response, request, "Google не подтвердил вход")
		return
	}
	user := googleUser(claims)
	token, err := server.auth.Create(user)
	if err != nil {
		server.oauthFailure(response, request, "Не удалось создать сессию")
		return
	}
	server.auth.SetCookie(response, token)
	http.Redirect(response, request, attempt.Next, http.StatusFound)
}

func googleUser(claims GoogleClaims) User {
	name := claims.Name
	if name == "" {
		name = claims.GivenName
	}
	if name == "" {
		name = claims.Email
	}
	return User{ID: claims.Subject, Email: claims.Email, Name: name, Picture: claims.Picture}
}

func (server *Server) oauthFailure(response http.ResponseWriter, request *http.Request, message string) {
	values := url.Values{"auth_error": {message}}
	http.Redirect(response, request, "/?"+values.Encode(), http.StatusFound)
}
