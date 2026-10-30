package main

import (
	"crypto/sha256"
	"encoding/hex"
	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/bcrypt"
	"net/http"
	"net/url"
	"time"
)

const cookieName = "sakura_session"

func tokenHash(token string) string {
	v := sha256.Sum256([]byte(token))
	return hex.EncodeToString(v[:])
}
func sameOrigin(r *http.Request) bool { return originMatches(r, "") }
func originMatches(r *http.Request, publicOrigin string) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		if ref := r.Header.Get("Referer"); ref != "" {
			u, e := url.Parse(ref)
			if e == nil {
				origin = u.Scheme + "://" + u.Host
			}
		}
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	expected := scheme + "://" + r.Host
	if publicOrigin != "" {
		expected = publicOrigin
	}
	return validOrigin(origin) && origin == expected
}
func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	if !originMatches(r, s.cfg.PublicOrigin) {
		fail(w, 403, "origin_forbidden", "A matching Origin or Referer is required")
		return
	}
	if !s.limit.allow("login", time.Now()) {
		fail(w, 429, "rate_limited", "Too many login attempts")
		return
	}
	var in struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !decode(w, r, &in) {
		return
	}
	var hash string
	err := s.db.QueryRow(r.Context(), "SELECT password_hash FROM admins WHERE username=$1", in.Username).Scan(&hash)
	if err != nil && err != pgx.ErrNoRows {
		s.internal(w, err)
		return
	}
	if err == pgx.ErrNoRows {
		hash = string(s.dummyHash)
	}
	passwordErr := bcrypt.CompareHashAndPassword([]byte(hash), []byte(in.Password))
	if err != nil || passwordErr != nil {
		fail(w, 401, "invalid_credentials", "Invalid username or password")
		return
	}
	token := randomID()
	expires := time.Now().Add(24 * time.Hour)
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		s.internal(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	if _, err = tx.Exec(r.Context(), "DELETE FROM auth_sessions WHERE expires_at < now()"); err != nil {
		s.internal(w, err)
		return
	}
	if _, err = tx.Exec(r.Context(), "INSERT INTO auth_sessions(token_hash,username,expires_at) VALUES($1,$2,$3)", tokenHash(token), in.Username, expires); err != nil {
		s.internal(w, err)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		s.internal(w, err)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: token, Path: "/", HttpOnly: true, Secure: s.cfg.SecureCookie, SameSite: http.SameSiteStrictMode, Expires: expires, MaxAge: 86400})
	jsonResponse(w, 200, map[string]string{"username": in.Username})
}
func (s *Server) username(r *http.Request) (string, error) {
	c, err := r.Cookie(cookieName)
	if err != nil {
		return "", err
	}
	var username string
	err = s.db.QueryRow(r.Context(), "SELECT username FROM auth_sessions WHERE token_hash=$1 AND expires_at>now()", tokenHash(c.Value)).Scan(&username)
	return username, err
}
func (s *Server) admin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" && r.Method != "HEAD" && !originMatches(r, s.cfg.PublicOrigin) {
			fail(w, 403, "origin_forbidden", "A matching Origin or Referer is required")
			return
		}
		if _, err := s.username(r); err != nil {
			if err != pgx.ErrNoRows && err != http.ErrNoCookie {
				s.internal(w, err)
				return
			}
			fail(w, 401, "unauthorized", "Sign in required")
			return
		}
		next(w, r)
	}
}
func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	username, err := s.username(r)
	if err != nil {
		s.internal(w, err)
		return
	}
	jsonResponse(w, 200, map[string]string{"username": username})
}
func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	c, _ := r.Cookie(cookieName)
	if c != nil {
		if _, err := s.db.Exec(r.Context(), "DELETE FROM auth_sessions WHERE token_hash=$1", tokenHash(c.Value)); err != nil {
			s.internal(w, err)
			return
		}
	}
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: "", Path: "/", HttpOnly: true, Secure: s.cfg.SecureCookie, SameSite: http.SameSiteStrictMode, MaxAge: -1, Expires: time.Unix(1, 0)})
	jsonResponse(w, 200, map[string]bool{"ok": true})
}
