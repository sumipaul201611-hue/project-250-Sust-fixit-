package main

import (
	"database/sql"
	"errors"
	"log"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

type User struct {
	ID           int64  `json:"id"`
	StudentID    string `json:"student_id"`
	Name         string `json:"name"`
	Email        string `json:"email"`
	Department   string `json:"department"`
	Role         string `json:"role"`
	PasswordHash string `json:"-"` // never sent to the browser
}

// ---------- JWT ----------

type claims struct {
	UserID int64  `json:"uid"`
	Role   string `json:"role"`
	jwt.RegisteredClaims
}

func (a *App) makeToken(u *User) (string, error) {
	now := time.Now()
	c := claims{
		UserID: u.ID,
		Role:   u.Role,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   strconv.FormatInt(u.ID, 10),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(24 * time.Hour)),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, c).SignedString(a.jwtSecret)
}

var errUnauthorized = errors.New("unauthorized")

// userFromRequest checks the "Authorization: Bearer <token>" header.
// The role is read fresh from the database, so a changed role or deleted user takes effect at once.
func (a *App) userFromRequest(r *http.Request) (*User, error) {
	h := r.Header.Get("Authorization")
	if len(h) < 8 || !strings.EqualFold(h[:7], "bearer ") {
		return nil, errUnauthorized
	}
	c := &claims{}
	tok, err := jwt.ParseWithClaims(strings.TrimSpace(h[7:]), c,
		func(t *jwt.Token) (any, error) { return a.jwtSecret, nil },
		jwt.WithValidMethods([]string{"HS256"}), // blocks the "alg: none" trick
	)
	if err != nil || !tok.Valid {
		return nil, errUnauthorized
	}
	u, err := a.userByID(c.UserID)
	if err != nil {
		return nil, errUnauthorized
	}
	return u, nil
}

type authedHandler func(http.ResponseWriter, *http.Request, *User)

// auth = "must be logged in"
func (a *App) auth(h authedHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, err := a.userFromRequest(r)
		if err != nil {
			writeErr(w, http.StatusUnauthorized, "Please log in to continue")
			return
		}
		h(w, r, u)
	}
}

// admin = "must be logged in AND be an admin"
func (a *App) admin(h authedHandler) http.HandlerFunc {
	return a.auth(func(w http.ResponseWriter, r *http.Request, u *User) {
		if u.Role != "admin" {
			writeErr(w, http.StatusForbidden, "Admin access required")
			return
		}
		h(w, r, u)
	})
}

// ---------- users in the database ----------

const userCols = "id, student_id, name, email, department, role, password_hash"

func scanUser(row interface{ Scan(...any) error }) (*User, error) {
	u := &User{}
	if err := row.Scan(&u.ID, &u.StudentID, &u.Name, &u.Email, &u.Department, &u.Role, &u.PasswordHash); err != nil {
		return nil, err
	}
	return u, nil
}

func (a *App) userByID(id int64) (*User, error) {
	return scanUser(a.db.QueryRow("SELECT "+userCols+" FROM users WHERE id = ?", id))
}

func (a *App) userByEmail(email string) (*User, error) {
	return scanUser(a.db.QueryRow("SELECT "+userCols+" FROM users WHERE email = ?", email))
}

func (a *App) createUser(studentID, name, email, password, department, role string) (*User, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	res, err := a.db.Exec(
		`INSERT INTO users (student_id, name, email, password_hash, department, role, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		studentID, name, email, string(hash), department, role, nowStr())
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return a.userByID(id)
}

// ensureAdmin creates the first admin account on startup if it does not exist.
func (a *App) ensureAdmin(email, password string) error {
	email = strings.ToLower(email)
	if _, err := a.userByEmail(email); err == nil {
		return nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if _, err := a.createUser("ADMIN", "Campus Admin", email, password, "Administration", "admin"); err != nil {
		return err
	}
	log.Printf("Created admin account: %s", email)
	return nil
}

// ---------- handlers ----------

var (
	studentIDRe = regexp.MustCompile(`^[A-Za-z0-9]{4,20}$`)
	emailRe     = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)
)

func (a *App) handleRegister(w http.ResponseWriter, r *http.Request) {
	if !a.limiter.allow(clientIP(r)) {
		writeErr(w, http.StatusTooManyRequests, "Too many attempts. Please wait a minute.")
		return
	}
	var in struct {
		StudentID  string `json:"student_id"`
		Name       string `json:"name"`
		Email      string `json:"email"`
		Password   string `json:"password"`
		Department string `json:"department"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	in.StudentID = strings.TrimSpace(in.StudentID)
	in.Name = strings.TrimSpace(in.Name)
	in.Email = strings.ToLower(strings.TrimSpace(in.Email))
	in.Department = strings.TrimSpace(in.Department)

	switch {
	case !studentIDRe.MatchString(in.StudentID):
		writeErr(w, http.StatusBadRequest, "Student ID must be 4-20 letters or digits")
	case utf8.RuneCountInString(in.Name) < 2 || utf8.RuneCountInString(in.Name) > 100:
		writeErr(w, http.StatusBadRequest, "Enter your full name")
	case !emailRe.MatchString(in.Email):
		writeErr(w, http.StatusBadRequest, "Enter a valid email address")
	case a.allowedDomain != "" && !strings.HasSuffix(in.Email, "@"+a.allowedDomain):
		writeErr(w, http.StatusBadRequest, "Use your university email (ending in @"+a.allowedDomain+")")
	case len(in.Password) < 8:
		writeErr(w, http.StatusBadRequest, "Password must be at least 8 characters")
	case len(in.Password) > 72: // bcrypt only reads the first 72 bytes
		writeErr(w, http.StatusBadRequest, "Password must be at most 72 characters")
	case utf8.RuneCountInString(in.Department) > 50:
		writeErr(w, http.StatusBadRequest, "Department name is too long")
	default:
		var exists int
		err := a.db.QueryRow("SELECT COUNT(*) FROM users WHERE email = ? OR student_id = ?", in.Email, in.StudentID).Scan(&exists)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "Could not create the account")
			return
		}
		if exists > 0 {
			writeErr(w, http.StatusConflict, "An account with this email or student ID already exists")
			return
		}
		u, err := a.createUser(in.StudentID, in.Name, in.Email, in.Password, in.Department, "student")
		if err != nil {
			log.Println("register:", err)
			writeErr(w, http.StatusInternalServerError, "Could not create the account")
			return
		}
		a.respondWithToken(w, http.StatusCreated, u)
	}
}

func (a *App) handleLogin(w http.ResponseWriter, r *http.Request) {
	if !a.limiter.allow(clientIP(r)) {
		writeErr(w, http.StatusTooManyRequests, "Too many attempts. Please wait a minute.")
		return
	}
	var in struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	u, err := a.userByEmail(strings.ToLower(strings.TrimSpace(in.Email)))
	// Same message for "no such user" and "wrong password" so nobody can probe which emails exist.
	if err != nil || bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(in.Password)) != nil {
		writeErr(w, http.StatusUnauthorized, "Wrong email or password")
		return
	}
	a.respondWithToken(w, http.StatusOK, u)
}

func (a *App) respondWithToken(w http.ResponseWriter, status int, u *User) {
	tok, err := a.makeToken(u)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "Could not create a login token")
		return
	}
	writeJSON(w, status, map[string]any{"token": tok, "user": u})
}

func (a *App) handleMe(w http.ResponseWriter, r *http.Request, u *User) {
	writeJSON(w, http.StatusOK, map[string]any{"user": u})
}

// ---------- simple rate limiter (brute-force protection) ----------

type rateLimiter struct {
	mu     sync.Mutex
	hits   map[string][]time.Time
	max    int
	window time.Duration
}

func newRateLimiter(max int, window time.Duration) *rateLimiter {
	return &rateLimiter{hits: map[string][]time.Time{}, max: max, window: window}
}

func (l *rateLimiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	recent := l.hits[key][:0]
	for _, t := range l.hits[key] {
		if now.Sub(t) < l.window {
			recent = append(recent, t)
		}
	}
	if len(recent) >= l.max {
		l.hits[key] = recent
		return false
	}
	l.hits[key] = append(recent, now)
	return true
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func nowStr() string { return time.Now().UTC().Format(time.RFC3339) }
