package main

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type GoogleClaims struct {
	Issuer        string `json:"iss"`
	Audience      string `json:"aud"`
	Subject       string `json:"sub"`
	Email         string `json:"email"`
	EmailVerified bool   `json:"email_verified"`
	Name          string `json:"name"`
	GivenName     string `json:"given_name"`
	Picture       string `json:"picture"`
	ExpiresAt     int64  `json:"exp"`
	IssuedAt      int64  `json:"iat"`
}

type GoogleVerifier struct {
	clientID string
	http     *http.Client
	mu       sync.RWMutex
	keys     map[string]*rsa.PublicKey
	expires  time.Time
}

type AuthStore struct {
	mu           sync.RWMutex
	sessions     map[string]Session
	oauth        map[string]OAuthAttempt
	verifier     *GoogleVerifier
	cookieSecure bool
	sessionsPath string
}

func NewGoogleVerifier(clientID string) *GoogleVerifier {
	return &GoogleVerifier{clientID: clientID, http: &http.Client{Timeout: 10 * time.Second}, keys: map[string]*rsa.PublicKey{}}
}

func (v *GoogleVerifier) refresh(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://www.googleapis.com/oauth2/v1/certs", nil)
	if err != nil {
		return err
	}
	response, err := v.http.Do(req)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("google certificates returned %s", response.Status)
	}
	var values map[string]string
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&values); err != nil {
		return err
	}
	keys := make(map[string]*rsa.PublicKey, len(values))
	for id, value := range values {
		block, _ := pem.Decode([]byte(value))
		if block == nil {
			continue
		}
		certificate, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			continue
		}
		key, ok := certificate.PublicKey.(*rsa.PublicKey)
		if ok {
			keys[id] = key
		}
	}
	if len(keys) == 0 {
		return errors.New("google returned no RSA certificates")
	}
	maxAge := 30 * time.Minute
	for _, part := range strings.Split(response.Header.Get("Cache-Control"), ",") {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, "max-age=") {
			if parsed, err := time.ParseDuration(strings.TrimPrefix(part, "max-age=") + "s"); err == nil {
				maxAge = parsed
			}
		}
	}
	v.mu.Lock()
	v.keys = keys
	v.expires = time.Now().Add(maxAge)
	v.mu.Unlock()
	return nil
}

func decodeSegment(value string, target any) error {
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, target)
}

func (v *GoogleVerifier) Verify(ctx context.Context, token string) (GoogleClaims, error) {
	var claims GoogleClaims
	parts := strings.Split(token, ".")
	if len(parts) != 3 || len(token) > 8192 {
		return claims, errors.New("invalid Google credential")
	}
	var header struct {
		Algorithm string `json:"alg"`
		KeyID     string `json:"kid"`
	}
	if err := decodeSegment(parts[0], &header); err != nil || header.Algorithm != "RS256" || header.KeyID == "" {
		return claims, errors.New("invalid Google credential header")
	}
	v.mu.RLock()
	key := v.keys[header.KeyID]
	expired := time.Now().After(v.expires)
	v.mu.RUnlock()
	if key == nil || expired {
		if err := v.refresh(ctx); err != nil {
			return claims, err
		}
		v.mu.RLock()
		key = v.keys[header.KeyID]
		v.mu.RUnlock()
	}
	if key == nil {
		return claims, errors.New("Google signing key not found")
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return claims, errors.New("invalid Google credential signature")
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err := rsa.VerifyPKCS1v15(key, crypto.SHA256, digest[:], signature); err != nil {
		return claims, errors.New("Google credential signature check failed")
	}
	if err := decodeSegment(parts[1], &claims); err != nil {
		return claims, errors.New("invalid Google credential payload")
	}
	now := time.Now().Unix()
	validIssuer := claims.Issuer == "accounts.google.com" || claims.Issuer == "https://accounts.google.com"
	if !validIssuer || claims.Audience != v.clientID || claims.Subject == "" || claims.ExpiresAt <= now || claims.IssuedAt > now+60 || !claims.EmailVerified {
		return claims, errors.New("Google credential claims check failed")
	}
	return claims, nil
}

func NewAuthStore(clientID string, secure bool, sessionPaths ...string) *AuthStore {
	sessionsPath := ""
	if len(sessionPaths) > 0 {
		sessionsPath = sessionPaths[0]
	}
	store := &AuthStore{sessions: map[string]Session{}, oauth: map[string]OAuthAttempt{}, verifier: NewGoogleVerifier(clientID), cookieSecure: secure, sessionsPath: sessionsPath}
	if sessionsPath == "" {
		return store
	}
	data, err := os.ReadFile(sessionsPath)
	if err != nil {
		return store
	}
	if err := json.Unmarshal(data, &store.sessions); err != nil || store.sessions == nil {
		store.sessions = map[string]Session{}
	}
	now := time.Now()
	for token, session := range store.sessions {
		if now.After(session.ExpiresAt) {
			delete(store.sessions, token)
		}
	}
	return store
}

func (a *AuthStore) saveSessionsLocked() error {
	if a.sessionsPath == "" {
		return nil
	}
	data, err := json.Marshal(a.sessions)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(a.sessionsPath), 0o700); err != nil {
		return err
	}
	temporary := a.sessionsPath + ".tmp"
	if err := os.WriteFile(temporary, data, 0o600); err != nil {
		return err
	}
	return os.Rename(temporary, a.sessionsPath)
}

func randomToken(size int) (string, error) {
	value := make([]byte, size)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

func (a *AuthStore) Create(user User) (string, error) {
	token, err := randomToken(32)
	if err != nil {
		return "", err
	}
	a.mu.Lock()
	a.sessions[token] = Session{User: user, ExpiresAt: time.Now().Add(7 * 24 * time.Hour)}
	saveError := a.saveSessionsLocked()
	a.mu.Unlock()
	if saveError != nil {
		return "", saveError
	}
	return token, nil
}

func (a *AuthStore) User(request *http.Request) (User, bool) {
	cookie, err := request.Cookie("tutu_session")
	if err != nil {
		return User{}, false
	}
	a.mu.RLock()
	session, ok := a.sessions[cookie.Value]
	a.mu.RUnlock()
	if !ok || time.Now().After(session.ExpiresAt) {
		if ok {
			a.mu.Lock()
			delete(a.sessions, cookie.Value)
			a.saveSessionsLocked()
			a.mu.Unlock()
		}
		return User{}, false
	}
	return session.User, true
}

func (a *AuthStore) SetCookie(response http.ResponseWriter, token string) {
	http.SetCookie(response, &http.Cookie{Name: "tutu_session", Value: token, Path: "/", MaxAge: 604800, HttpOnly: true, Secure: a.cookieSecure, SameSite: http.SameSiteLaxMode})
}

func (a *AuthStore) Logout(response http.ResponseWriter, request *http.Request) {
	if cookie, err := request.Cookie("tutu_session"); err == nil {
		a.mu.Lock()
		delete(a.sessions, cookie.Value)
		a.saveSessionsLocked()
		a.mu.Unlock()
	}
	http.SetCookie(response, &http.Cookie{Name: "tutu_session", Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: a.cookieSecure, SameSite: http.SameSiteLaxMode})
}
