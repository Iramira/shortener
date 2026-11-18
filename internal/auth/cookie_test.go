package auth

import (
	"encoding/hex"
	"errors"
	"net/http/httptest"
	"testing"
)

func TestSignHMACSHA256(t *testing.T) {
	// RFC 4231, test case 1. HMAC zero-pads the key to the hash block size.
	c := &Cookies{}
	for i := 0; i < 20; i++ {
		c.key[i] = 0x0b
	}
	const want = "b0344c61d8db38535ca8afceaf0bf12b881dc200c9833da726e9376c2e32cff7"
	if got := hex.EncodeToString(c.sign("Hi There")); got != want {
		t.Fatalf("signature = %s, want %s", got, want)
	}
}

func TestCookieSignedByAnotherKeyIsRejected(t *testing.T) {
	signer := &Cookies{key: [32]byte{1}, secure: true}
	anotherSigner := &Cookies{key: [32]byte{2}}
	w := httptest.NewRecorder()
	id, err := signer.Issue(w)
	if err != nil {
		t.Fatal(err)
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].Secure {
		t.Fatalf("expected one secure cookie, got %v", cookies)
	}
	r := httptest.NewRequest("GET", "/", nil)
	r.AddCookie(cookies[0])
	if got, err := signer.Read(r); err != nil || got != id {
		t.Fatalf("Read = (%q, %v), want (%q, nil)", got, err, id)
	}
	if _, err := anotherSigner.Read(r); !errors.Is(err, ErrInvalidSignature) {
		t.Fatalf("Read with another key: %v, want %v", err, ErrInvalidSignature)
	}
}
