package app

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log"
	"mime"
	"net/http"
	"net/url"
	"strings"

	"shortener/internal/auth"
	"shortener/internal/storage"
)

const maxBodyBytes = 16 << 10

type app struct {
	baseURL string
	store   *storage.Memory
	cookies *auth.Cookies
}

type userURL struct {
	ShortURL    string `json:"short_url"`
	OriginalURL string `json:"original_url"`
}

func New(baseURL string, store *storage.Memory, cookies *auth.Cookies) http.Handler {
	a := &app{baseURL: strings.TrimRight(baseURL, "/"), store: store, cookies: cookies}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/shorten", a.shorten)
	mux.HandleFunc("GET /{id}", a.redirect)
	mux.HandleFunc("GET /api/user/urls", a.userURLs)
	return mux
}

func (a *app) shorten(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if value := r.Header.Get("Content-Type"); value != "" {
		mediaType, _, err := mime.ParseMediaType(value)
		if err != nil || mediaType != "application/json" {
			http.Error(w, "expected application/json", http.StatusUnsupportedMediaType)
			return
		}
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	decoder := json.NewDecoder(r.Body)
	var input struct {
		URL string `json:"url"`
	}
	if err := decoder.Decode(&input); err != nil {
		badJSON(w, err)
		return
	}
	// A valid body contains exactly one JSON value.
	if err := decoder.Decode(new(json.RawMessage)); err != io.EOF {
		badJSON(w, err)
		return
	}
	original := strings.TrimSpace(input.URL)
	parsed, err := url.Parse(original)
	if err != nil || parsed.Hostname() == "" ||
		(parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.User != nil {
		http.Error(w, "url must be an absolute HTTP or HTTPS URL without user info", http.StatusBadRequest)
		return
	}

	userID, err := a.cookies.Read(r)
	if err != nil {
		userID, err = a.cookies.Issue(w)
		if err != nil {
			internalError(w, err)
			return
		}
	}
	for attempt := 0; attempt < 8; attempt++ {
		var random [8]byte
		if _, err := rand.Read(random[:]); err != nil {
			internalError(w, err)
			return
		}
		id := base64.RawURLEncoding.EncodeToString(random[:])
		if a.store.Add(userID, storage.Link{ID: id, OriginalURL: original}) {
			writeJSON(w, struct {
				Result string `json:"result"`
			}{Result: a.shortURL(id)})
			return
		}
	}
	internalError(w, errors.New("could not allocate a unique link ID"))
}

func (a *app) redirect(w http.ResponseWriter, r *http.Request) {
	original, ok := a.store.Lookup(r.PathValue("id"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	http.Redirect(w, r, original, http.StatusTemporaryRedirect)
}

func (a *app) userURLs(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	userID, err := a.cookies.Read(r)
	switch {
	case errors.Is(err, auth.ErrMalformed):
		http.Error(w, "malformed user cookie", http.StatusUnauthorized)
		return
	case errors.Is(err, http.ErrNoCookie), errors.Is(err, auth.ErrInvalidSignature):
		if _, err := a.cookies.Issue(w); err != nil {
			internalError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	case err != nil:
		internalError(w, err)
		return
	}

	links := a.store.List(userID)
	if len(links) == 0 {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	result := make([]userURL, 0, len(links))
	for _, link := range links {
		result = append(result, userURL{
			ShortURL:    a.shortURL(link.ID),
			OriginalURL: link.OriginalURL,
		})
	}
	writeJSON(w, result)
}

func (a *app) shortURL(id string) string {
	return a.baseURL + "/" + id
}

func badJSON(w http.ResponseWriter, err error) {
	var limit *http.MaxBytesError
	if errors.As(err, &limit) {
		http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
		return
	}
	http.Error(w, "expected a single JSON object with a url field", http.StatusBadRequest)
}

func internalError(w http.ResponseWriter, err error) {
	log.Printf("shortener: %v", err)
	http.Error(w, "internal server error", http.StatusInternalServerError)
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		log.Printf("write JSON response: %v", err)
	}
}
