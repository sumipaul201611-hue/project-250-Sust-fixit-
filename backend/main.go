// SUST FixIt - campus problem reporting.
// Report once, support many times.
package main

import (
	"bufio"
	"database/sql"
	"encoding/json"
	"flag"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

type App struct {
	db            *sql.DB
	jwtSecret     []byte
	uploadDir     string
	allowedDomain string // e.g. "student.sust.edu"; empty = any email
	limiter       *rateLimiter
}

func main() {
	seed := flag.Bool("seed", false, "load demo data (only if there are no reports yet)")
	flag.Parse()

	loadDotEnv(".env")

	secret := getenv("JWT_SECRET", "")
	if secret == "" {
		secret = "dev-only-secret-change-me"
		log.Println("WARNING: JWT_SECRET is not set. Using an insecure development secret.")
	}

	app := &App{
		jwtSecret:     []byte(secret),
		uploadDir:     getenv("UPLOAD_DIR", "uploads"),
		allowedDomain: strings.ToLower(getenv("ALLOWED_EMAIL_DOMAIN", "student.sust.edu")),
		limiter:       newRateLimiter(30, time.Minute),
	}

	if err := os.MkdirAll(app.uploadDir, 0o755); err != nil {
		log.Fatal(err)
	}
	db, err := openDB(getenv("DB_PATH", "fixit.db"))
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	app.db = db

	if err := app.ensureAdmin(getenv("ADMIN_EMAIL", "admin@sust.edu"), getenv("ADMIN_PASSWORD", "admin12345")); err != nil {
		log.Fatal(err)
	}
	if *seed {
		if err := app.seedDemoData(); err != nil {
			log.Fatal(err)
		}
	}

	frontend := findFrontend()
	addr := ":" + getenv("PORT", "8080")
	log.Printf("SUST FixIt running on http://localhost%s (frontend: %s)", addr, frontend)
	srv := &http.Server{
		Addr:              addr,
		Handler:           app.routes(frontend),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      60 * time.Second,
	}
	log.Fatal(srv.ListenAndServe())
}

func (a *App) routes(frontendDir string) http.Handler {
	mux := http.NewServeMux()

	// Public
	mux.HandleFunc("GET /api/meta", a.handleMeta)
	mux.HandleFunc("GET /api/stats", a.handleStats)
	mux.HandleFunc("POST /api/auth/register", a.handleRegister)
	mux.HandleFunc("POST /api/auth/login", a.handleLogin)
	mux.HandleFunc("GET /api/reports", a.handleListReports) // public feed (login optional)
	mux.HandleFunc("GET /api/reports/similar", a.handleSimilar)
	mux.HandleFunc("GET /api/reports/{id}", a.handleGetReport)

	// Logged-in students
	mux.HandleFunc("GET /api/auth/me", a.auth(a.handleMe))
	mux.HandleFunc("POST /api/reports", a.auth(a.handleCreateReport))
	mux.HandleFunc("POST /api/reports/{id}/upvote", a.auth(a.handleToggleUpvote))
	mux.HandleFunc("DELETE /api/reports/{id}", a.auth(a.handleDeleteReport))

	// Admin only
	mux.HandleFunc("PATCH /api/admin/reports/{id}/status", a.admin(a.handleUpdateStatus))

	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeErr(w, http.StatusNotFound, "Not found")
	})

	// Uploaded photos (no directory listing, never executed as scripts)
	uploads := http.StripPrefix("/uploads/", http.FileServer(noListFS{http.Dir(a.uploadDir)}))
	mux.HandleFunc("GET /uploads/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; sandbox")
		uploads.ServeHTTP(w, r)
	})

	// Frontend
	mux.Handle("/", http.FileServer(http.Dir(frontendDir)))

	return recoverer(logRequests(securityHeaders(mux)))
}

// ---------- small helpers ----------

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func readJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		writeErr(w, http.StatusBadRequest, "Invalid request body")
		return false
	}
	return true
}

func getenv(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

// loadDotEnv reads KEY=VALUE lines from a file. Real environment variables win.
func loadDotEnv(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k, v = strings.TrimSpace(k), strings.Trim(strings.TrimSpace(v), `"'`)
		if os.Getenv(k) == "" {
			os.Setenv(k, v)
		}
	}
}

func findFrontend() string {
	if d := os.Getenv("FRONTEND_DIR"); d != "" {
		return d
	}
	for _, d := range []string{"../frontend", "frontend"} {
		if st, err := os.Stat(d); err == nil && st.IsDir() {
			return d
		}
	}
	log.Fatal("frontend folder not found (set FRONTEND_DIR)")
	return ""
}

// noListFS hides directory listings.
type noListFS struct{ fs http.FileSystem }

func (n noListFS) Open(name string) (http.File, error) {
	f, err := n.fs.Open(name)
	if err != nil {
		return nil, err
	}
	if st, err := f.Stat(); err != nil || st.IsDir() {
		f.Close()
		return nil, os.ErrNotExist
	}
	return f, nil
}

// ---------- middleware ----------

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "same-origin")
		next.ServeHTTP(w, r)
	})
}

func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("%s %s (%s)", r.Method, r.URL.Path, time.Since(start).Round(time.Millisecond))
	})
}

func recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				log.Printf("panic: %v", rec)
				writeErr(w, http.StatusInternalServerError, "Something went wrong on the server")
			}
		}()
		next.ServeHTTP(w, r)
	})
}
