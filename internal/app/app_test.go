package app

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"shortener/internal/auth"
	"shortener/internal/storage"
)

const baseURL = "http://localhost:8080"

func newHandler(t *testing.T) http.Handler {
	t.Helper()
	cookies, err := auth.New(false)
	if err != nil {
		t.Fatal(err)
	}
	return New(baseURL, storage.New(), cookies)
}

func request(handler http.Handler, method, path, body string, cookie *http.Cookie) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	if cookie != nil {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	return w
}

func requireStatus(t *testing.T, w *httptest.ResponseRecorder, want int) {
	t.Helper()
	if w.Code != want {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, want, w.Body.String())
	}
}

func sessionCookie(t *testing.T, w *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	cookies := w.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != auth.CookieName {
		t.Fatalf("expected one user cookie, got %v", cookies)
	}
	return cookies[0]
}

func shorten(t *testing.T, h http.Handler, original string, cookie *http.Cookie) (string, *httptest.ResponseRecorder) {
	t.Helper()
	w := request(h, http.MethodPost, "/api/shorten", fmt.Sprintf(`{"url":%q}`, original), cookie)
	requireStatus(t, w, http.StatusOK)
	var response struct {
		Result string `json:"result"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(response.Result, baseURL+"/") || len(response.Result) == len(baseURL)+1 {
		t.Fatalf("invalid short URL: %q", response.Result)
	}
	return response.Result, w
}

func readLinks(t *testing.T, h http.Handler, cookie *http.Cookie) []userURL {
	t.Helper()
	w := request(h, http.MethodGet, "/api/user/urls", "", cookie)
	requireStatus(t, w, http.StatusOK)
	var links []userURL
	if err := json.Unmarshal(w.Body.Bytes(), &links); err != nil {
		t.Fatal(err)
	}
	return links
}

func TestShortenRedirectAndList(t *testing.T) {
	h := newHandler(t)
	const first = "https://example.com/path?q=1#part"
	const second = "https://example.org/second"
	short1, w := shorten(t, h, first, nil)
	if w.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("unexpected response Content-Type: %q", w.Header().Get("Content-Type"))
	}
	cookie := sessionCookie(t, w)
	if !cookie.HttpOnly || cookie.Path != "/" || cookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("unexpected cookie attributes: %v", cookie)
	}
	short2, w := shorten(t, h, second, cookie)
	if w.Header().Get("Set-Cookie") != "" || short1 == short2 {
		t.Fatal("the existing session should be reused and link IDs should be distinct")
	}

	w = request(h, http.MethodGet, strings.TrimPrefix(short1, baseURL), "", nil)
	requireStatus(t, w, http.StatusTemporaryRedirect)
	if got := w.Header().Get("Location"); got != first {
		t.Fatalf("Location = %q, want %q", got, first)
	}
	requireStatus(t, request(h, http.MethodGet, "/not-found", "", nil), http.StatusNotFound)

	links := readLinks(t, h, cookie)
	want := []userURL{{ShortURL: short1, OriginalURL: first}, {ShortURL: short2, OriginalURL: second}}
	if len(links) != len(want) || links[0] != want[0] || links[1] != want[1] {
		t.Fatalf("links = %#v, want %#v", links, want)
	}

	otherShort, w := shorten(t, h, first, nil)
	otherCookie := sessionCookie(t, w)
	if cookie.Value == otherCookie.Value || short1 == otherShort {
		t.Fatal("new users and their links must have distinct IDs")
	}
	otherLinks := readLinks(t, h, otherCookie)
	if len(otherLinks) != 1 || otherLinks[0] != (userURL{ShortURL: otherShort, OriginalURL: first}) {
		t.Fatalf("another user received unexpected links: %#v", otherLinks)
	}
}

func TestListWithMissingOrInvalidCookie(t *testing.T) {
	h := newHandler(t)
	const path = "/api/user/urls"
	_, w := shorten(t, h, "https://example.com/private-list", nil)
	original := sessionCookie(t, w)
	id, signature, _ := strings.Cut(original.Value, ":")
	replacement := "0"
	if id[0] == '0' {
		replacement = "1"
	}
	tampered := &http.Cookie{Name: auth.CookieName, Value: replacement + id[1:] + ":" + signature}

	for _, tc := range []struct {
		name    string
		cookie  *http.Cookie
		want    int
		renewal bool
	}{
		{"missing cookie", nil, http.StatusNoContent, true},
		{"tampered ID", tampered, http.StatusNoContent, true},
		{"empty cookie", &http.Cookie{Name: auth.CookieName}, http.StatusUnauthorized, false},
		{"missing ID", &http.Cookie{Name: auth.CookieName, Value: ":" + signature}, http.StatusUnauthorized, false},
		{"invalid format", &http.Cookie{Name: auth.CookieName, Value: "not-signed"}, http.StatusUnauthorized, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := request(h, http.MethodGet, path, "", tc.cookie)
			requireStatus(t, w, tc.want)
			if tc.want == http.StatusNoContent && w.Body.Len() != 0 {
				t.Fatalf("204 must not contain a body: %q", w.Body.String())
			}
			if !tc.renewal {
				if w.Header().Get("Set-Cookie") != "" {
					t.Fatal("malformed cookie should not be renewed by list")
				}
				return
			}
			fresh := sessionCookie(t, w)
			if fresh.Value == original.Value || (tc.cookie != nil && fresh.Value == tc.cookie.Value) {
				t.Fatal("expected a new session")
			}
			w = request(h, http.MethodGet, path, "", fresh)
			requireStatus(t, w, http.StatusNoContent)
			if w.Body.Len() != 0 || w.Header().Get("Set-Cookie") != "" {
				t.Fatal("an existing empty session should return 204 without a new cookie")
			}
		})
	}

	_, w = shorten(t, h, "https://example.org", tampered)
	fresh := sessionCookie(t, w)
	if fresh.Value == tampered.Value || fresh.Value == original.Value {
		t.Fatal("shorten must replace an invalid cookie with a new identity")
	}
	if links := readLinks(t, h, original); len(links) != 1 {
		t.Fatalf("tampered request changed the original user's list: %#v", links)
	}
}

func TestShortenRejectsBadRequests(t *testing.T) {
	h := newHandler(t)
	for _, tc := range []struct {
		name string
		body string
		want int
	}{
		{"empty body", "", http.StatusBadRequest},
		{"broken JSON", `{"url":`, http.StatusBadRequest},
		{"missing URL", `{}`, http.StatusBadRequest},
		{"wrong field type", `{"url":1}`, http.StatusBadRequest},
		{"relative URL", `{"url":"/relative"}`, http.StatusBadRequest},
		{"unsupported scheme", `{"url":"ftp://example.com"}`, http.StatusBadRequest},
		{"second JSON value", `{"url":"https://example.com"} {}`, http.StatusBadRequest},
		{"oversized body", `{"url":"https://example.com/` + strings.Repeat("a", maxBodyBytes) + `"}`, http.StatusRequestEntityTooLarge},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := request(h, http.MethodPost, "/api/shorten", tc.body, nil)
			requireStatus(t, w, tc.want)
			if w.Header().Get("Set-Cookie") != "" {
				t.Fatal("an invalid body should not start a session")
			}
		})
	}
}

func TestShortenRejectsWrongMethodAndMediaType(t *testing.T) {
	h := newHandler(t)
	const path = "/api/shorten"
	requireStatus(t, request(h, http.MethodGet, path, "", nil), http.StatusMethodNotAllowed)

	r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"url":"https://example.com"}`))
	r.Header.Set("Content-Type", "text/plain")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	requireStatus(t, w, http.StatusUnsupportedMediaType)
}
