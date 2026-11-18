package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
)

const CookieName = "user_id"

var (
	ErrMalformed        = errors.New("malformed user cookie")
	ErrInvalidSignature = errors.New("invalid cookie signature")
)

type Cookies struct {
	key    [32]byte
	secure bool
}

func New(secure bool) (*Cookies, error) {
	c := &Cookies{secure: secure}
	if _, err := rand.Read(c.key[:]); err != nil {
		return nil, err
	}
	return c, nil
}

func (c *Cookies) Read(r *http.Request) (string, error) {
	cookie, err := r.Cookie(CookieName)
	if err != nil {
		return "", err
	}
	id, signature, ok := strings.Cut(cookie.Value, ":")
	if !ok || id == "" || signature == "" || strings.Contains(signature, ":") {
		return "", ErrMalformed
	}
	actual, err := hex.DecodeString(signature)
	if err != nil || !hmac.Equal(c.sign(id), actual) {
		return "", ErrInvalidSignature
	}
	return id, nil
}

func (c *Cookies) Issue(w http.ResponseWriter) (string, error) {
	var rawID [16]byte
	if _, err := rand.Read(rawID[:]); err != nil {
		return "", err
	}
	id := hex.EncodeToString(rawID[:])
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    id + ":" + hex.EncodeToString(c.sign(id)),
		Path:     "/",
		HttpOnly: true,
		Secure:   c.secure,
		SameSite: http.SameSiteLaxMode,
	})
	return id, nil
}

func (c *Cookies) sign(id string) []byte {
	mac := hmac.New(sha256.New, c.key[:])
	mac.Write([]byte(id))
	return mac.Sum(nil)
}
