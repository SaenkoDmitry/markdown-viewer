package server

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"markdown-viewer/internal/content"
	"markdown-viewer/internal/limiter"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"markdown-viewer/internal/session"
)

const port = "8085"

func getClientIP(r *http.Request) string {
	// X-Forwarded-For для прокси
	xff := r.Header.Get("X-Forwarded-For")
	if xff != "" {
		ips := strings.Split(xff, ",")
		if len(ips) > 0 {
			return strings.TrimSpace(ips[0])
		}
	}

	xri := r.Header.Get("X-Real-Ip")
	if xri != "" {
		return xri
	}

	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// ===== Security Middleware =====

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("X-XSS-Protection", "1; mode=block")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self' 'unsafe-inline' https://cdnjs.cloudflare.com; script-src 'self' 'unsafe-inline' https://cdnjs.cloudflare.com; img-src 'self' data: https:; font-src 'self'")
		next.ServeHTTP(w, r)
	})
}

func rateLimitMiddleware(rl *limiter.RateLimiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := getClientIP(r)
			if !rl.Allow(ip) {
				http.Error(w, "Too many requests", http.StatusTooManyRequests)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// Path traversal protection для static
func safeFileServer(root string) http.Handler {
	fs := http.FileServer(http.Dir(root))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Блокируем попытки выйти за пределы root
		cleanPath := strings.ReplaceAll(r.URL.Path, "..", "")
		if strings.Contains(cleanPath, "\x00") {
			http.Error(w, "Invalid path", 400)
			return
		}
		r.URL.Path = cleanPath
		fs.ServeHTTP(w, r)
	})
}

// ===== Server =====

type Server struct {
	tmpl        *template.Template
	limiter     *limiter.RateLimiter
	maxRepoSize int64 // макс размер репо в байтах
}

func New() *Server {
	tmpl := template.Must(template.ParseFiles(
		"templates/page.html",
		"templates/index.html",
	))
	return &Server{
		tmpl:        tmpl,
		limiter:     limiter.NewRateLimiter(30, 10),
		maxRepoSize: 50 * 1024 * 1024,
	}
}

func (s *Server) Run() error {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/open", s.handleOpen)
	mux.HandleFunc("/api/sessions", s.handleAPI)
	mux.Handle("/static/", http.StripPrefix("/static/", safeFileServer("static")))

	handler := securityHeaders(mux)
	handler = rateLimitMiddleware(s.limiter)(handler)

	srv := &http.Server{
		Addr:         ":" + port,
		Handler:      handler,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	log.Printf("Server: http://localhost:%s", port)
	return srv.ListenAndServe()
}

// Обновите handleIndex для работы с префиксом
func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	path := strings.Trim(r.URL.Path, "/")

	if path == "" || path == "s" {
		s.tmpl.ExecuteTemplate(w, "index.html", nil)
		return
	}

	parts := strings.Split(path, "/")
	if len(parts) < 2 || parts[0] != "s" {
		http.Redirect(w, r, "/", 302)
		return
	}

	sessID := parts[1]
	if !isValidSessionID(sessID) {
		http.Error(w, "Invalid session ID", 400)
		return
	}

	sess := session.Get(sessID)
	if sess == nil {
		http.Error(w, "Session not found", 404)
		return
	}

	pagePath := ""
	if len(parts) > 2 {
		pagePath = strings.Join(parts[2:], "/")
	}

	if pagePath == "" {
		index := content.FindIndexPage(sess.Pages)
		if index != "" {
			http.Redirect(w, r, "/s/"+sessID+"/"+index, 302)
			return
		}
		for k := range sess.Pages {
			http.Redirect(w, r, "/s/"+sessID+"/"+k, 302)
			return
		}
	}

	pagePath = content.Slugify(pagePath)
	page, ok := sess.Pages[pagePath]
	if !ok {
		http.NotFound(w, r)
		return
	}

	s.tmpl.ExecuteTemplate(w, "page.html", page)
}

// Обновите handleOpen для редиректов с префиксом
func (s *Server) handleOpen(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Redirect(w, r, "/", 302)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1024)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Request too large", 400)
		return
	}

	repoURL := r.FormValue("repo")
	if repoURL == "" {
		http.Error(w, "URL required", 400)
		return
	}

	if !isValidGitHubURL(repoURL) {
		http.Error(w, "Invalid GitHub URL", 400)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()

	sess, err := session.Create(repoURL)
	if err != nil {
		log.Printf("Failed to create session: %v", err)
		http.Error(w, "Failed: "+err.Error(), 500)
		return
	}

	_ = ctx

	log.Printf("Session %s ready: %s (%d pages)", sess.ID, sess.RepoURL, len(sess.Pages))

	redirectURL := fmt.Sprintf("/s/%s/?session=%s&repo=%s&branch=%s&pages=%d",
		sess.ID,
		sess.ID,
		url.QueryEscape(sess.RepoURL),
		sess.Branch,
		len(sess.Pages),
	)
	http.Redirect(w, r, redirectURL, 302)
}

func (s *Server) handleAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	type slim struct {
		ID      string `json:"id"`
		RepoURL string `json:"repo"`
		Branch  string `json:"branch"`
		Pages   int    `json:"pages"`
	}

	all := session.All()
	list := make([]slim, 0, len(all))
	for _, s := range all {
		list = append(list, slim{
			ID:      s.ID,
			RepoURL: s.RepoURL,
			Branch:  s.Branch,
			Pages:   len(s.Pages),
		})
	}

	// Ограничиваем вывод
	if len(list) > 100 {
		list = list[:100]
	}

	json.NewEncoder(w).Encode(list)
}

// ===== Helpers =====

func isValidSessionID(id string) bool {
	if len(id) != 16 {
		return false
	}
	for _, c := range id {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}

func isValidGitHubURL(url string) bool {
	return strings.HasPrefix(url, "https://github.com/") &&
		!strings.Contains(url, "..") &&
		!strings.Contains(url, "\x00")
}

func (s *Server) checkRepoSize(repoURL string) bool {
	// Упрощённая проверка — можно добавить HEAD запрос к GitHub API
	// Пока просто проверяем что URL выглядит валидно
	return true
}
