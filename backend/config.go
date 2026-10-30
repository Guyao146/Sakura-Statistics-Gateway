package main

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode"
)

type Config struct {
	Addr, DatabaseURL, Username, Password, StaticDir, PublicOrigin string
	Timezone                                                       *time.Location
	SecureCookie                                                   bool
}

func loadConfig() (Config, error) {
	c := Config{Addr: listenAddr(), DatabaseURL: os.Getenv("DATABASE_URL"), Username: os.Getenv("ADMIN_USERNAME"), Password: os.Getenv("ADMIN_PASSWORD"), StaticDir: env("STATIC_DIR", "./web/dist"), PublicOrigin: os.Getenv("PUBLIC_ORIGIN"), SecureCookie: true}
	var err error
	c.SecureCookie, err = strconv.ParseBool(env("SESSION_SECURE", env("COOKIE_SECURE", "true")))
	if err != nil {
		return c, errors.New("SESSION_SECURE/COOKIE_SECURE must be a boolean")
	}
	if port := os.Getenv("PORT"); port != "" {
		n, e := strconv.Atoi(strings.TrimPrefix(port, ":"))
		if e != nil || n < 1 || n > 65535 {
			return c, errors.New("PORT must be between 1 and 65535")
		}
	}
	c.Timezone, err = time.LoadLocation(env("STATISTICS_TIMEZONE", "Asia/Shanghai"))
	if err != nil {
		return c, err
	}
	if c.DatabaseURL == "" || strings.TrimSpace(c.Username) == "" {
		return c, errors.New("DATABASE_URL and ADMIN_USERNAME are required")
	}
	if c.PublicOrigin != "" && !validOrigin(c.PublicOrigin) {
		return c, errors.New("PUBLIC_ORIGIN must be an exact http(s) origin")
	}
	if !strongPassword(c.Password) {
		return c, errors.New("ADMIN_PASSWORD must contain at least 12 characters (at most 72 UTF-8 bytes), upper/lowercase letters, a number and a symbol")
	}
	return c, nil
}
func listenAddr() string {
	if port := os.Getenv("PORT"); port != "" {
		return ":" + strings.TrimPrefix(port, ":")
	}
	return env("ADDR", ":8080")
}
func env(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
func strongPassword(s string) bool {
	var upper, lower, digit, symbol bool
	for _, r := range s {
		upper = upper || unicode.IsUpper(r)
		lower = lower || unicode.IsLower(r)
		digit = digit || unicode.IsDigit(r)
		symbol = symbol || (!unicode.IsLetter(r) && !unicode.IsDigit(r) && !unicode.IsSpace(r))
	}
	return len(s) <= 72 && len([]rune(s)) >= 12 && upper && lower && digit && symbol
}
func randomID() string {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}
func validOrigin(s string) bool {
	u, e := url.Parse(s)
	return e == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != "" && u.User == nil && u.RawQuery == "" && u.Fragment == "" && (u.Path == "" || u.Path == "/") && s == u.Scheme+"://"+u.Host
}
