# SUST FixIt

**Report once, support many times.**

A centralized campus problem-reporting platform. Students report problems
(broken fans, water leaks, potholes, dirty areas) with a photo and exact
location. Other students upvote existing reports instead of filing
duplicates. Admins track and resolve issues, posting a before/after photo.

## Scope (by design)

This build sticks to 6 core features, on purpose — a smaller, fully working
system demos better than a bigger, half-working one:

1. JWT login / register (restricted to your university email domain)
2. Report a problem — photo + building + exact location + category
3. Public issue feed — filter, search, sort by newest or most-upvoted
4. Upvote — one vote per student per report, toggle on/off
5. Admin status tracking — Reported → Verified → In progress → Resolved
6. Before/after resolution photo

Plus one "smart" feature that needs no AI: a **duplicate-suggestion** check.
While a student is filling in the report form, the app matches their title +
building + location + category against existing open reports and suggests
upvoting instead, live, before they submit.

**Deliberately left out** (future scope, not needed to demo the core idea):
AI-based image classification, points/leaderboard, real-time notifications,
cloud image storage, a full campus map, more than 5 categories, and a
weighted priority-scoring algorithm. Any of these makes a good "future work"
slide — none of them is needed to show problem → report → upvote → resolve.

## Tech stack

- **Backend:** Go, standard library only (`net/http`'s Go 1.22 router — no
  Gin/Fiber needed for a project this size)
- **Auth:** JWT (`golang-jwt/jwt/v5`), bcrypt password hashing
- **Database:** SQLite (`modernc.org/sqlite`, pure Go — no C compiler or
  separate DB server to install)
- **Frontend:** Plain HTML/CSS/JS (no build step, no framework)
- **Photo storage:** local disk under `backend/uploads/`

Everything runs from a single Go binary plus a folder of static files — no
Docker, no external database, nothing else to install beyond Go itself.

## Project structure

```
sust-fixit/
├── backend/
│   ├── main.go        # server setup, routing, middleware
│   ├── db.go           # SQLite connection + schema
│   ├── driver.go       # the one file that imports the sqlite driver
│   ├── auth.go         # JWT issuing/checking, register/login, rate limiting
│   ├── reports.go      # feed, create/delete, upvote, duplicate-check, admin status
│   ├── seed.go          # optional demo data (-seed flag)
│   ├── go.mod
│   └── .env.example
└── frontend/
    ├── index.html       # landing page with live stats
    ├── login.html / register.html
    ├── feed.html         # public feed with filters
    ├── report.html       # submission form with live duplicate-suggestion
    ├── dashboard.html    # "my reports" with a status timeline
    ├── admin.html        # KPI cards + table + status-update modal
    ├── css/style.css
    └── js/api.js, feed-card.js
```

## Running it

Requires Go 1.22+ (for the standard-library router used here).

```bash
cd backend
cp .env.example .env        # then edit JWT_SECRET, ADMIN_PASSWORD, etc.
go mod tidy                  # downloads golang-jwt, x/crypto, modernc.org/sqlite
go run . -seed                # -seed loads demo reports on first run only
```

Then open **http://localhost:8080** — the Go server serves both the API
(under `/api/...`) and the `frontend/` folder, so there's nothing else to
start.

The first run also creates an admin account from `ADMIN_EMAIL` /
`ADMIN_PASSWORD` in your `.env` (defaults: `admin@sust.edu` / `admin12345` —
**change this before showing it to anyone**). With `-seed`, three demo
students are created too — see the terminal output for the login to use
(password `student123`).

### Environment variables

| Variable | Default | Purpose |
|---|---|---|
| `PORT` | `8080` | HTTP port |
| `DB_PATH` | `fixit.db` | SQLite file path |
| `UPLOAD_DIR` | `uploads` | Where photos are stored |
| `JWT_SECRET` | *(dev fallback, insecure)* | Signs login tokens — **set a real one** |
| `ALLOWED_EMAIL_DOMAIN` | `student.sust.edu` | Registration is restricted to `...@this`; leave empty to allow any email |
| `ADMIN_EMAIL` / `ADMIN_PASSWORD` | `admin@sust.edu` / `admin12345` | First admin account, created on first startup |

## API

All endpoints are JSON except where noted; authenticated ones expect
`Authorization: Bearer <token>`.

| Method | Path | Auth | Purpose |
|---|---|---|---|
| GET | `/api/meta` | – | Building/category/status lists (for form dropdowns) |
| GET | `/api/stats` | – | Counts by status/category (for the KPI cards) |
| POST | `/api/auth/register` | – | Create a student account |
| POST | `/api/auth/login` | – | Get a JWT |
| GET | `/api/auth/me` | ✅ | Current user |
| GET | `/api/reports` | optional | Feed — `?status=&category=&building=&q=&sort=new\|upvotes&mine=1&limit=&offset=` |
| GET | `/api/reports/{id}` | optional | One report |
| GET | `/api/reports/similar` | optional | Duplicate-suggestion — `?title=&building=&category=&location=` |
| POST | `/api/reports` | ✅ | Create (multipart: `title, description, category, building, location, image`) |
| DELETE | `/api/reports/{id}` | ✅ | Delete own report (only while status is `reported`; admin can always delete) |
| POST | `/api/reports/{id}/upvote` | ✅ | Toggle your upvote |
| PATCH | `/api/admin/reports/{id}/status` | ✅ admin | `status=verified\|in_progress\|resolved` (multipart `resolved_image` required when resolving) |

## Notes on the implementation

- **Duplicate suggestion** (`handleSimilar` in `reports.go`) is plain word
  overlap on title/location plus a category match, restricted to open
  reports in the same building — no AI/ML involved, matching the "simple
  search/location matching" the brief called for.
- **Photos** are validated by sniffing the real file bytes
  (`http.DetectContentType`), not by trusting the filename or the
  browser-supplied content type, and stored under a random name — a
  student can't overwrite another photo or upload a disguised file.
- **Upvotes** use a composite primary key `(user_id, report_id)`, so a
  double-vote is impossible at the database level, not just in application
  logic.
- **Everything is a single SQLite file.** That's a deliberate simplification
  for a two-person course project — see `db.go` if you'd rather point this
  at Postgres later; the SQL is close enough to portable that it's a small
  change, not a rewrite.

## Presentation flow (5-7 minutes)

1. Register/login as a student (or use the seeded `rafi@student.sust.edu` /
   `student123`) → submit a report with a photo. Start typing a title that
   matches a seeded demo report (e.g. "broken fan") to show the
   duplicate-suggestion box appear live.
2. Open a second browser (or incognito) as another student → upvote the
   same report → refresh the first tab to show the count update.
3. Log in as admin → show the KPI cards and the table sorted by upvotes →
   change a report's status through Verified → In progress → Resolved,
   uploading an "after" photo at the resolve step.
4. Back in the student dashboard, show the status timeline reflect the
   change.
