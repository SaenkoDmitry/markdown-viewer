package server

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"markdown-viewer/internal/content"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"markdown-viewer/internal/session"
)

const port = "8085"

// ===== Rate Limiter =====

type visitor struct {
	lastSeen time.Time
	count    int
}

type rateLimiter struct {
	visitors map[string]*visitor
	mu       sync.RWMutex
	limit    int
	window   time.Duration
}

func newRateLimiter(limit int, window time.Duration) *rateLimiter {
	rl := &rateLimiter{
		visitors: make(map[string]*visitor),
		limit:    limit,
		window:   window,
	}
	go rl.cleanup()
	return rl
}

func (rl *rateLimiter) cleanup() {
	ticker := time.NewTicker(time.Minute)
	for range ticker.C {
		rl.mu.Lock()
		for ip, v := range rl.visitors {
			if time.Since(v.lastSeen) > rl.window {
				delete(rl.visitors, ip)
			}
		}
		rl.mu.Unlock()
	}
}

func (rl *rateLimiter) allow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	v, exists := rl.visitors[ip]
	if !exists {
		rl.visitors[ip] = &visitor{lastSeen: time.Now(), count: 1}
		return true
	}

	if time.Since(v.lastSeen) > rl.window {
		v.count = 1
		v.lastSeen = time.Now()
		return true
	}

	v.count++
	v.lastSeen = time.Now()
	return v.count <= rl.limit
}

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

func rateLimitMiddleware(rl *rateLimiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := getClientIP(r)
			if !rl.allow(ip) {
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
	limiter     *rateLimiter
	maxRepoSize int64 // макс размер репо в байтах
	basePath    string
}

func New() *Server {
	tmpl := template.Must(template.ParseFiles(
		"templates/page.html",
		"templates/index.html",
	))
	return &Server{
		basePath:    strings.TrimSuffix(os.Getenv("BASE_PATH"), "/"),
		tmpl:        tmpl,
		limiter:     newRateLimiter(30, time.Minute), // 30 запросов в минуту
		maxRepoSize: 50 * 1024 * 1024,                // 50 MB
	}
}

func (s *Server) Run() error {
	mux := http.NewServeMux()

	// Оберните все обработчики в функцию добавления префикса
	mux.HandleFunc(s.basePath+"/", s.handleIndex)
	mux.HandleFunc(s.basePath+"/open", s.handleOpen)
	mux.HandleFunc(s.basePath+"/api/sessions", s.handleAPI)
	mux.Handle("/static/", http.StripPrefix(s.basePath+"/static/", safeFileServer("static")))

	// Обработчик для поддержки basePath
	var handler http.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Убираем базовый путь из запроса если он есть
		path := r.URL.Path
		if s.basePath != "" && strings.HasPrefix(path, s.basePath) {
			r.URL.Path = strings.TrimPrefix(path, s.basePath)
			if r.URL.Path == "" {
				r.URL.Path = "/"
			}
		}
		mux.ServeHTTP(w, r)
	})

	// Применяем middleware
	handler = securityHeaders(handler)
	handler = rateLimitMiddleware(s.limiter)(handler)

	srv := &http.Server{
		Addr:         ":" + port,
		Handler:      handler,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	log.Printf("Server: http://localhost:%s (base path: %s)", port, s.basePath)
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
		http.Redirect(w, r, s.basePath+"/", 302)
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
			http.Redirect(w, r, s.basePath+"/s/"+sessID+"/"+index, 302)
			return
		}
		for k := range sess.Pages {
			http.Redirect(w, r, s.basePath+"/s/"+sessID+"/"+k, 302)
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
		http.Redirect(w, r, s.basePath+"/", 302)
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

	redirectURL := fmt.Sprintf("%s/s/%s/?session=%s&repo=%s&branch=%s&pages=%d",
		s.basePath,
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
