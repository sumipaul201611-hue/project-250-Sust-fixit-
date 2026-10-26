package main

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const maxUpload = 5 << 20 // 5 MB per photo

// Fixed lists keep the data clean (and the frontend reads them from /api/meta).
var (
	buildings = []string{
		"IICT Building", "Academic Building A", "Academic Building B", "Academic Building C", "Academic Building D",
		"Central Library", "Cafeteria", "Auditorium", "Student Hall", "Shah Paran Hall", "Bijoy 24 Hall",
		"Syed Mujtaba Ali Hall", "First Lady Hall", "Begum Fazilatunnesa Mujib Hall", "Roads & Campus Grounds", "University Center & Cafeteria", "Other",
	}
	categories = []string{
		"Electrical",
		"Food & Dining",
		"Wi-Fi & Internet",
		"Plumbing & Water",
		"Roads & Infrastructure",
		"Furniture & Facilities",
		"Cleanliness & Sanitation",
		"Other",
	}
	statuses = []string{"reported", "verified", "in_progress", "resolved"}

	imageTypes = map[string]string{
		"image/jpeg": ".jpg", "image/png": ".png", "image/gif": ".gif", "image/webp": ".webp",
	}
)

func normalizeCategory(cat string) string {
	c := strings.TrimSpace(cat)
	switch {
	case strings.EqualFold(c, "Plumbing") || strings.EqualFold(c, "Plumbing & Water"):
		return "Plumbing & Water"
	case strings.EqualFold(c, "Infrastructure") || strings.EqualFold(c, "Roads & Infrastructure"):
		return "Roads & Infrastructure"
	case strings.EqualFold(c, "Furniture") || strings.EqualFold(c, "Furniture & Facilities"):
		return "Furniture & Facilities"
	case strings.EqualFold(c, "Cleanliness") || strings.EqualFold(c, "Cleanliness & Sanitation"):
		return "Cleanliness & Sanitation"
	case strings.EqualFold(c, "Food") || strings.EqualFold(c, "Food & Dining") || strings.EqualFold(c, "Dining"):
		return "Food & Dining"
	case strings.EqualFold(c, "Wifi") || strings.EqualFold(c, "Wi-Fi") || strings.EqualFold(c, "Wi-Fi & Internet") || strings.EqualFold(c, "Internet"):
		return "Wi-Fi & Internet"
	case strings.EqualFold(c, "Electrical"):
		return "Electrical"
	case strings.EqualFold(c, "Other"):
		return "Other"
	default:
		return cat
	}
}

func categoryAliases(cat string) []string {
	norm := normalizeCategory(cat)
	switch norm {
	case "Roads & Infrastructure":
		return []string{"Roads & Infrastructure", "Infrastructure"}
	case "Plumbing & Water":
		return []string{"Plumbing & Water", "Plumbing"}
	case "Furniture & Facilities":
		return []string{"Furniture & Facilities", "Furniture"}
	case "Cleanliness & Sanitation":
		return []string{"Cleanliness & Sanitation", "Cleanliness"}
	case "Food & Dining":
		return []string{"Food & Dining", "Food", "Dining"}
	case "Wi-Fi & Internet":
		return []string{"Wi-Fi & Internet", "Wi-Fi", "Wifi", "Internet"}
	default:
		return []string{norm}
	}
}

type Report struct {
	ID               int64  `json:"id"`
	Title            string `json:"title"`
	Description      string `json:"description"`
	Category         string `json:"category"`
	Building         string `json:"building"`
	Location         string `json:"location"`
	ImageURL         string `json:"image_url"`
	ResolvedImageURL string `json:"resolved_image_url"`
	Status           string `json:"status"`
	CreatedAt        string `json:"created_at"`
	UpdatedAt        string `json:"updated_at"`
	ReporterName     string `json:"reporter_name"`
	UpvoteCount      int    `json:"upvote_count"`
	HasUpvoted       bool   `json:"has_upvoted"`
	IsMine           bool   `json:"is_mine"`
	PriorityScore    int    `json:"priority_score"`
	Severity         string `json:"severity"`
	SLADueAt         string `json:"sla_due_at"`
	AssetTag         string `json:"asset_tag"`
}

// CalculatePriority computes a quantitative score based on community upvotes, category hazard index,
// keyword critical triggers, and time aging factor.
func CalculatePriority(cat string, upvotes int, createdAtStr, desc, title string) (int, string) {
	score := upvotes * 3 // verified student vote weight

	// Category Urgency Weight
	switch cat {
	case "Roads & Infrastructure", "Infrastructure":
		score += 45 // Road pothole, structural cracks
	case "Electrical":
		score += 40 // Fire hazard, fan/light outage
	case "Plumbing & Water", "Plumbing":
		score += 35 // Water leak, no sanitation water
	case "Food & Dining":
		score += 35 // Health & nutrition risk, food hygiene
	case "Wi-Fi & Internet":
		score += 30 // Academic & research connectivity
	case "Cleanliness & Sanitation", "Cleanliness":
		score += 25
	case "Furniture & Facilities", "Furniture":
		score += 20
	default:
		score += 15
	}

	// AI/Smart critical keyword detection
	text := strings.ToLower(title + " " + desc)
	severity := "MEDIUM"
	if strings.Contains(text, "fire") || strings.Contains(text, "spark") || strings.Contains(text, "shock") ||
		strings.Contains(text, "danger") || strings.Contains(text, "burn") || strings.Contains(text, "burst") ||
		strings.Contains(text, "emergency") || strings.Contains(text, "poison") || strings.Contains(text, "rotten") {
		score += 55
		severity = "CRITICAL"
	} else if strings.Contains(text, "leak") || strings.Contains(text, "pothole") || strings.Contains(text, "dark") ||
		strings.Contains(text, "flood") || strings.Contains(text, "muddy") || strings.Contains(text, "broken") ||
		strings.Contains(text, "undercooked") || strings.Contains(text, "cold food") || strings.Contains(text, "no internet") ||
		strings.Contains(text, "disconnected") {
		score += 20
		severity = "HIGH"
	}

	// Age Factor: +2 points per day old (max 30 points) so neglected tickets climb the queue
	if t, err := time.Parse(time.RFC3339, createdAtStr); err == nil {
		daysOld := int(time.Since(t).Hours() / 24)
		if daysOld > 0 {
			ageBonus := daysOld * 2
			if ageBonus > 30 {
				ageBonus = 30
			}
			score += ageBonus
		}
	}

	return score, severity
}

func makeAssetTag(building, category string, id int64) string {
	bCode := "CAMPUS"
	if strings.Contains(building, "IICT") {
		bCode = "IICT"
	} else if strings.Contains(building, "Academic Building A") {
		bCode = "ACAD-A"
	} else if strings.Contains(building, "Academic Building B") {
		bCode = "ACAD-B"
	} else if strings.Contains(building, "Library") {
		bCode = "LIB"
	} else if strings.Contains(building, "Hall") {
		bCode = "HALL"
	} else if strings.Contains(building, "Roads") {
		bCode = "ROAD"
	}
	cCode := "GEN"
	if len(category) >= 3 {
		cCode = strings.ToUpper(category[:3])
	}
	return fmt.Sprintf("SUST-%s-%s-%03d", bCode, cCode, id)
}

func calculateSLA(status, updatedAtStr string) string {
	if status != "in_progress" {
		return ""
	}
	if t, err := time.Parse(time.RFC3339, updatedAtStr); err == nil {
		return t.Add(48 * time.Hour).Format(time.RFC3339)
	}
	return ""
}

func contains(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}

// The "?" for the viewer id is the first placeholder, so it must be the first argument.
const reportSelect = `
SELECT r.id, r.title, r.description, r.category, r.building, r.location,
       r.image_url, r.resolved_image_url, r.status, r.created_at, r.updated_at,
       r.user_id, u.name,
       (SELECT COUNT(*) FROM upvotes v WHERE v.report_id = r.id),
       EXISTS(SELECT 1 FROM upvotes v WHERE v.report_id = r.id AND v.user_id = ?)
FROM reports r JOIN users u ON u.id = r.user_id `

func scanReport(row interface{ Scan(...any) error }, viewerID int64) (Report, error) {
	var rp Report
	var ownerID int64
	var voted int
	err := row.Scan(&rp.ID, &rp.Title, &rp.Description, &rp.Category, &rp.Building, &rp.Location,
		&rp.ImageURL, &rp.ResolvedImageURL, &rp.Status, &rp.CreatedAt, &rp.UpdatedAt,
		&ownerID, &rp.ReporterName, &rp.UpvoteCount, &voted)
	rp.Category = normalizeCategory(rp.Category)
	switch rp.Building {
	case "Student Hall":
		rp.Building = "Shah Paran Hall"
	case "Roads & Ground":
		rp.Building = "Roads & Campus Grounds"
	case "Cafeteria":
		rp.Building = "University Center & Cafeteria"
	}
	rp.HasUpvoted = voted == 1
	rp.IsMine = viewerID != 0 && ownerID == viewerID
	rp.PriorityScore, rp.Severity = CalculatePriority(rp.Category, rp.UpvoteCount, rp.CreatedAt, rp.Description, rp.Title)
	rp.AssetTag = makeAssetTag(rp.Building, rp.Category, rp.ID)
	rp.SLADueAt = calculateSLA(rp.Status, rp.UpdatedAt)
	return rp, err
}

func (a *App) getReport(id, viewerID int64) (Report, error) {
	return scanReport(a.db.QueryRow(reportSelect+"WHERE r.id = ?", viewerID, id), viewerID)
}

// optionalUser: the public feed works without login, but we still want to know
// which reports the visitor has already upvoted when they ARE logged in.
func (a *App) optionalUser(r *http.Request) *User {
	u, err := a.userFromRequest(r)
	if err != nil {
		return nil
	}
	return u
}

func pathID(r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	return id, err == nil && id > 0
}

// ---------- meta + stats ----------

func (a *App) handleMeta(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"buildings": buildings, "categories": categories, "statuses": statuses,
	})
}

// SeededDemoUserEmails explicitly lists seeded demonstration accounts.
// Accounts in this set are flagged as is_demo: true on the leaderboard and API
// so presentation sample data is transparently distinguished from real student activity.
var SeededDemoUserEmails = map[string]bool{
	"rafi@student.sust.edu":   true,
	"nusrat@student.sust.edu": true,
	"tanvir@student.sust.edu": true,
}

func isDemoStudent(email, studentID string) bool {
	if SeededDemoUserEmails[strings.ToLower(strings.TrimSpace(email))] {
		return true
	}
	if strings.HasPrefix(strings.ToUpper(strings.TrimSpace(studentID)), "DEMO") {
		return true
	}
	return false
}

func (a *App) handleStats(w http.ResponseWriter, r *http.Request) {
	byStatus := map[string]int{}
	byCategory := map[string]int{}
	for _, s := range statuses {
		byStatus[s] = 0
	}
	for _, c := range categories {
		byCategory[c] = 0
	}
	if err := a.countGroups("status", byStatus); err != nil {
		writeErr(w, http.StatusInternalServerError, "Could not load statistics")
		return
	}
	catRows, err := a.db.Query("SELECT category, COUNT(*) FROM reports GROUP BY category")
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "Could not load statistics")
		return
	}
	defer catRows.Close()
	for catRows.Next() {
		var c string
		var n int
		if err := catRows.Scan(&c, &n); err == nil {
			byCategory[normalizeCategory(c)] += n
		}
	}
	total := 0
	for _, n := range byStatus {
		total += n
	}

	byBuilding := map[string]int{}
	bldRows, err := a.db.Query("SELECT building, COUNT(*) FROM reports GROUP BY building")
	if err == nil {
		defer bldRows.Close()
		for bldRows.Next() {
			var bld string
			var n int
			if err := bldRows.Scan(&bld, &n); err == nil {
				switch bld {
				case "Student Hall":
					bld = "Shah Paran Hall"
				case "Roads & Ground":
					bld = "Roads & Campus Grounds"
				case "Cafeteria":
					bld = "University Center & Cafeteria"
				}
				byBuilding[bld] += n
			}
		}
	}

	type Hero struct {
		Name       string `json:"name"`
		Department string `json:"department"`
		Reports    int    `json:"reports"`
		Points     int    `json:"points"`
		Badge      string `json:"badge"`
		IsDemo     bool   `json:"is_demo"`
	}
	var heroes []Hero
	heroRows, err := a.db.Query(`
		SELECT u.name, u.department, u.email, u.student_id, COUNT(r.id) as cnt,
		       (COUNT(r.id)*25 + COALESCE(SUM(CASE WHEN r.status = 'resolved' THEN 50 ELSE 0 END), 0)) as pts
		FROM users u
		JOIN reports r ON r.user_id = u.id
		WHERE u.role = 'student'
		GROUP BY u.id
		ORDER BY pts DESC, cnt DESC
		LIMIT 5`)
	if err == nil {
		defer heroRows.Close()
		rank := 1
		for heroRows.Next() {
			var h Hero
			var email, studentID string
			if err := heroRows.Scan(&h.Name, &h.Department, &email, &studentID, &h.Reports, &h.Points); err == nil {
				h.IsDemo = isDemoStudent(email, studentID)
				switch rank {
				case 1:
					h.Badge = "Campus Champion 🥇"
				case 2:
					h.Badge = "Master Fixer 🥈"
				case 3:
					h.Badge = "Active Scout 🥉"
				default:
					h.Badge = "Civic Hero ⭐"
				}
				heroes = append(heroes, h)
				rank++
			}
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"total": total, "resolved": byStatus["resolved"], "open": total - byStatus["resolved"],
		"by_status": byStatus, "by_category": byCategory, "by_building": byBuilding,
		"top_reporters": heroes,
	})
}

// column is a fixed string from our own code (never user input).
func (a *App) countGroups(column string, into map[string]int) error {
	rows, err := a.db.Query("SELECT " + column + ", COUNT(*) FROM reports GROUP BY " + column)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var k string
		var n int
		if err := rows.Scan(&k, &n); err != nil {
			return err
		}
		into[k] = n
	}
	return rows.Err()
}

// ---------- feed ----------

func (a *App) handleListReports(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	viewer := a.optionalUser(r)
	var viewerID int64
	if viewer != nil {
		viewerID = viewer.ID
	}

	var where []string
	var args []any
	if s := q.Get("status"); s != "" {
		if !contains(statuses, s) {
			writeErr(w, http.StatusBadRequest, "Unknown status")
			return
		}
		where = append(where, "r.status = ?")
		args = append(args, s)
	}
	if q.Get("open") == "1" {
		where = append(where, "r.status != 'resolved'")
	}
	if v := q.Get("category"); v != "" {
		aliases := categoryAliases(v)
		placeholders := make([]string, len(aliases))
		for i, a := range aliases {
			placeholders[i] = "r.category = ?"
			args = append(args, a)
		}
		where = append(where, "("+strings.Join(placeholders, " OR ")+")")
	}
	if v := q.Get("building"); v != "" {
		switch v {
		case "Shah Paran Hall":
			where = append(where, "(r.building = ? OR r.building = 'Student Hall')")
			args = append(args, v)
		case "Roads & Campus Grounds":
			where = append(where, "(r.building = ? OR r.building = 'Roads & Ground')")
			args = append(args, v)
		case "University Center & Cafeteria":
			where = append(where, "(r.building = ? OR r.building = 'Cafeteria')")
			args = append(args, v)
		default:
			where = append(where, "r.building = ?")
			args = append(args, v)
		}
	}
	if q.Get("mine") == "1" {
		if viewer == nil {
			writeErr(w, http.StatusUnauthorized, "Please log in to see your reports")
			return
		}
		where = append(where, "r.user_id = ?")
		args = append(args, viewer.ID)
	}
	if s := strings.TrimSpace(q.Get("q")); s != "" {
		like := "%" + s + "%"
		where = append(where, "(r.title LIKE ? OR r.description LIKE ? OR r.location LIKE ?)")
		args = append(args, like, like, like)
	}
	whereSQL := ""
	if len(where) > 0 {
		whereSQL = "WHERE " + strings.Join(where, " AND ") + " "
	}

	order := map[string]string{
		"new":      "ORDER BY r.id DESC",
		"old":      "ORDER BY r.id ASC",
		"upvotes":  "ORDER BY (SELECT COUNT(*) FROM upvotes v WHERE v.report_id = r.id) DESC, r.id DESC",
		"priority": "ORDER BY ((SELECT COUNT(*) FROM upvotes v WHERE v.report_id = r.id)*3 + CASE r.category WHEN 'Roads & Infrastructure' THEN 45 WHEN 'Infrastructure' THEN 45 WHEN 'Electrical' THEN 40 WHEN 'Plumbing & Water' THEN 35 WHEN 'Plumbing' THEN 35 WHEN 'Food & Dining' THEN 35 WHEN 'Wi-Fi & Internet' THEN 30 WHEN 'Cleanliness & Sanitation' THEN 25 WHEN 'Cleanliness' THEN 25 ELSE 20 END) DESC, r.id DESC",
	}[q.Get("sort")]
	if order == "" {
		order = "ORDER BY ((SELECT COUNT(*) FROM upvotes v WHERE v.report_id = r.id)*3 + CASE r.category WHEN 'Roads & Infrastructure' THEN 45 WHEN 'Infrastructure' THEN 45 WHEN 'Electrical' THEN 40 WHEN 'Plumbing & Water' THEN 35 WHEN 'Plumbing' THEN 35 WHEN 'Food & Dining' THEN 35 WHEN 'Wi-Fi & Internet' THEN 30 WHEN 'Cleanliness & Sanitation' THEN 25 WHEN 'Cleanliness' THEN 25 ELSE 20 END) DESC, r.id DESC"
	}

	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit < 1 || limit > 50 {
		limit = 20
	}
	offset, _ := strconv.Atoi(q.Get("offset"))
	if offset < 0 {
		offset = 0
	}

	var total int
	if err := a.db.QueryRow("SELECT COUNT(*) FROM reports r "+whereSQL, args...).Scan(&total); err != nil {
		log.Println("count reports:", err)
		writeErr(w, http.StatusInternalServerError, "Could not load reports")
		return
	}

	listArgs := append([]any{viewerID}, args...)
	listArgs = append(listArgs, limit, offset)
	rows, err := a.db.Query(reportSelect+whereSQL+order+" LIMIT ? OFFSET ?", listArgs...)
	if err != nil {
		log.Println("list reports:", err)
		writeErr(w, http.StatusInternalServerError, "Could not load reports")
		return
	}
	defer rows.Close()
	reports := []Report{}
	for rows.Next() {
		rp, err := scanReport(rows, viewerID)
		if err != nil {
			log.Println("scan report:", err)
			writeErr(w, http.StatusInternalServerError, "Could not load reports")
			return
		}
		reports = append(reports, rp)
	}
	if err := rows.Err(); err != nil {
		writeErr(w, http.StatusInternalServerError, "Could not load reports")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"reports": reports, "total": total})
}

func (a *App) handleGetReport(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeErr(w, http.StatusBadRequest, "Invalid report id")
		return
	}
	var viewerID int64
	if u := a.optionalUser(r); u != nil {
		viewerID = u.ID
	}
	rp, err := a.getReport(id, viewerID)
	if errors.Is(err, sql.ErrNoRows) {
		writeErr(w, http.StatusNotFound, "Report not found")
		return
	} else if err != nil {
		writeErr(w, http.StatusInternalServerError, "Could not load the report")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"report": rp})
}

// ---------- duplicate suggestion (no AI: plain word + place matching) ----------

var stopWords = map[string]bool{
	"the": true, "and": true, "are": true, "not": true, "for": true, "with": true, "from": true,
	"this": true, "that": true, "has": true, "have": true, "was": true, "near": true, "floor": true,
}

func tokens(s string) map[string]bool {
	out := map[string]bool{}
	for _, w := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	}) {
		if utf8.RuneCountInString(w) >= 3 && !stopWords[w] {
			out[w] = true
		}
	}
	return out
}

func (a *App) handleSimilar(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	building, category := q.Get("building"), q.Get("category")
	mine := tokens(q.Get("title") + " " + q.Get("location"))
	if building == "" || len(mine) == 0 {
		writeJSON(w, http.StatusOK, map[string]any{"reports": []Report{}})
		return
	}
	var viewerID int64
	if u := a.optionalUser(r); u != nil {
		viewerID = u.ID
	}

	// Only look at unresolved problems in the same building.
	rows, err := a.db.Query(reportSelect+"WHERE r.building = ? AND r.status != 'resolved' ORDER BY r.id DESC LIMIT 200", viewerID, building)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "Could not check for similar reports")
		return
	}
	defer rows.Close()
	type scored struct {
		rp    Report
		score int
	}
	var found []scored
	for rows.Next() {
		rp, err := scanReport(rows, viewerID)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "Could not check for similar reports")
			return
		}
		score := 0
		if category != "" && rp.Category == category {
			score += 3
		}
		for t := range tokens(rp.Title + " " + rp.Location) {
			if mine[t] {
				score += 2
			}
		}
		if score >= 5 {
			found = append(found, scored{rp, score})
		}
	}
	if err := rows.Err(); err != nil {
		writeErr(w, http.StatusInternalServerError, "Could not check for similar reports")
		return
	}
	sort.Slice(found, func(i, j int) bool {
		if found[i].score != found[j].score {
			return found[i].score > found[j].score
		}
		return found[i].rp.UpvoteCount > found[j].rp.UpvoteCount
	})
	out := []Report{}
	for i := 0; i < len(found) && i < 3; i++ {
		out = append(out, found[i].rp)
	}
	writeJSON(w, http.StatusOK, map[string]any{"reports": out})
}

// ---------- create / delete ----------

func (a *App) handleCreateReport(w http.ResponseWriter, r *http.Request, u *User) {
	r.Body = http.MaxBytesReader(w, r.Body, maxUpload+(1<<20))
	if err := r.ParseMultipartForm(maxUpload); err != nil {
		writeErr(w, http.StatusBadRequest, "The photo is too large (max 5 MB) or the form is invalid")
		return
	}
	defer r.MultipartForm.RemoveAll()

	title := strings.TrimSpace(r.FormValue("title"))
	desc := strings.TrimSpace(r.FormValue("description"))
	category := normalizeCategory(r.FormValue("category"))
	building := strings.TrimSpace(r.FormValue("building"))
	location := strings.TrimSpace(r.FormValue("location"))

	titleLen := utf8.RuneCountInString(title)
	switch {
	case titleLen < 5 || titleLen > 120:
		writeErr(w, http.StatusBadRequest, "Title must be 5-120 characters")
	case utf8.RuneCountInString(desc) < 10 || utf8.RuneCountInString(desc) > 1500:
		writeErr(w, http.StatusBadRequest, "Description must be 10-1500 characters")
	case !contains(categories, category):
		writeErr(w, http.StatusBadRequest, "Choose a category")
	case !contains(buildings, building):
		writeErr(w, http.StatusBadRequest, "Choose a building")
	case utf8.RuneCountInString(location) < 2 || utf8.RuneCountInString(location) > 120:
		writeErr(w, http.StatusBadRequest, "Enter the exact location (for example: Lab 301, 3rd floor)")
	default:
		// The photo is optional: patchy campus wifi, a dead phone camera, or
		// any other reason shouldn't stop a report from being filed. Students
		// can add the photo later; the feed shows a category placeholder
		// until then (see frontend js/feed-card.js: thumbHtml).
		var imgURL string
		if file, hdr, ferr := r.FormFile("image"); ferr == nil {
			defer file.Close()
			saved, serr := a.saveImage(file, hdr.Size)
			if serr != nil {
				writeErr(w, http.StatusBadRequest, serr.Error())
				return
			}
			imgURL = saved
		}

		// Server-side duplicate debounce protection:
		// If the same user submits an identical report (same title, building, location) within the last 15 seconds,
		// return the existing report idempotently to prevent accidental duplicate insertions.
		var dupID int64
		errDup := a.db.QueryRow(`
			SELECT id FROM reports
			WHERE user_id = ? AND title = ? AND building = ? AND location = ?
			  AND datetime(created_at) >= datetime('now', '-15 seconds')
			ORDER BY id DESC LIMIT 1`, u.ID, title, building, location).Scan(&dupID)
		if errDup == nil && dupID > 0 {
			a.removeUpload(imgURL)
			rp, err := a.getReport(dupID, u.ID)
			if err == nil {
				writeJSON(w, http.StatusOK, map[string]any{"report": rp})
				return
			}
		}

		now := nowStr()
		res, err := a.db.Exec(
			`INSERT INTO reports (user_id, title, description, category, building, location, image_url, status, created_at, updated_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, 'reported', ?, ?)`,
			u.ID, title, desc, category, building, location, imgURL, now, now)
		if err != nil {
			log.Println("insert report:", err)
			a.removeUpload(imgURL)
			writeErr(w, http.StatusInternalServerError, "Could not save the report")
			return
		}
		id, _ := res.LastInsertId()
		rp, err := a.getReport(id, u.ID)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "Report saved, but could not be loaded")
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"report": rp})
	}
}

func (a *App) handleDeleteReport(w http.ResponseWriter, r *http.Request, u *User) {
	id, ok := pathID(r)
	if !ok {
		writeErr(w, http.StatusBadRequest, "Invalid report id")
		return
	}
	var ownerID int64
	var status, img, img2 string
	err := a.db.QueryRow("SELECT user_id, status, image_url, resolved_image_url FROM reports WHERE id = ?", id).
		Scan(&ownerID, &status, &img, &img2)
	if errors.Is(err, sql.ErrNoRows) {
		writeErr(w, http.StatusNotFound, "Report not found")
		return
	} else if err != nil {
		writeErr(w, http.StatusInternalServerError, "Could not delete the report")
		return
	}
	isAdmin := u.Role == "admin"
	if !isAdmin && ownerID != u.ID {
		writeErr(w, http.StatusForbidden, "You can only delete your own reports")
		return
	}
	if !isAdmin && status != "reported" {
		writeErr(w, http.StatusForbidden, "This report is already being handled and can no longer be deleted")
		return
	}
	if _, err := a.db.Exec("DELETE FROM reports WHERE id = ?", id); err != nil {
		writeErr(w, http.StatusInternalServerError, "Could not delete the report")
		return
	}
	a.removeUpload(img)
	a.removeUpload(img2)
	writeJSON(w, http.StatusOK, map[string]any{"deleted": id})
}

// ---------- upvote (toggle) ----------

func (a *App) handleToggleUpvote(w http.ResponseWriter, r *http.Request, u *User) {
	id, ok := pathID(r)
	if !ok {
		writeErr(w, http.StatusBadRequest, "Invalid report id")
		return
	}
	var exists int
	if err := a.db.QueryRow("SELECT COUNT(*) FROM reports WHERE id = ?", id).Scan(&exists); err != nil || exists == 0 {
		writeErr(w, http.StatusNotFound, "Report not found")
		return
	}
	// Check if action is specified as "vote" (to ensure upvoted without toggling off)
	action := r.URL.Query().Get("action")
	if action == "vote" || r.URL.Query().Get("ensure") == "true" {
		var already int
		_ = a.db.QueryRow("SELECT COUNT(*) FROM upvotes WHERE user_id = ? AND report_id = ?", u.ID, id).Scan(&already)
		if already > 0 {
			var count int
			_ = a.db.QueryRow("SELECT COUNT(*) FROM upvotes WHERE report_id = ?", id).Scan(&count)
			writeJSON(w, http.StatusOK, map[string]any{"upvoted": true, "already_upvoted": true, "upvote_count": count})
			return
		}
		if _, err := a.db.Exec("INSERT OR IGNORE INTO upvotes (user_id, report_id, created_at) VALUES (?, ?, ?)", u.ID, id, nowStr()); err != nil {
			writeErr(w, http.StatusInternalServerError, "Could not save your vote")
			return
		}
		var count int
		_ = a.db.QueryRow("SELECT COUNT(*) FROM upvotes WHERE report_id = ?", id).Scan(&count)
		writeJSON(w, http.StatusOK, map[string]any{"upvoted": true, "upvote_count": count})
		return
	}

	// DELETE first: if a row was removed, the student had voted and we are un-voting.
	// Otherwise insert. The PRIMARY KEY (user_id, report_id) makes double votes impossible.
	res, err := a.db.Exec("DELETE FROM upvotes WHERE user_id = ? AND report_id = ?", u.ID, id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "Could not save your vote")
		return
	}
	removed, _ := res.RowsAffected()
	upvoted := false
	if removed == 0 {
		if _, err := a.db.Exec("INSERT OR IGNORE INTO upvotes (user_id, report_id, created_at) VALUES (?, ?, ?)", u.ID, id, nowStr()); err != nil {
			writeErr(w, http.StatusInternalServerError, "Could not save your vote")
			return
		}
		upvoted = true
	}
	var count int
	_ = a.db.QueryRow("SELECT COUNT(*) FROM upvotes WHERE report_id = ?", id).Scan(&count)
	writeJSON(w, http.StatusOK, map[string]any{"upvoted": upvoted, "upvote_count": count})
}

// ---------- admin: change status (+ "after" photo) ----------

func (a *App) handleUpdateStatus(w http.ResponseWriter, r *http.Request, u *User) {
	id, ok := pathID(r)
	if !ok {
		writeErr(w, http.StatusBadRequest, "Invalid report id")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxUpload+(1<<20))
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
		if err := r.ParseMultipartForm(maxUpload); err != nil {
			writeErr(w, http.StatusBadRequest, "The photo is too large (max 5 MB) or the form is invalid")
			return
		}
		defer r.MultipartForm.RemoveAll()
	} else if err := r.ParseForm(); err != nil {
		writeErr(w, http.StatusBadRequest, "Invalid form")
		return
	}

	status := r.FormValue("status")
	if !contains(statuses, status) {
		writeErr(w, http.StatusBadRequest, "Unknown status")
		return
	}
	var current, afterURL string
	err := a.db.QueryRow("SELECT status, resolved_image_url FROM reports WHERE id = ?", id).Scan(&current, &afterURL)
	if errors.Is(err, sql.ErrNoRows) {
		writeErr(w, http.StatusNotFound, "Report not found")
		return
	} else if err != nil {
		writeErr(w, http.StatusInternalServerError, "Could not update the report")
		return
	}

	// The after-photo is optional too, for the same reason as the report
	// photo: an admin can mark something resolved right away and add the
	// proof photo later if one isn't on hand yet.
	newAfter := afterURL
	if status == "resolved" {
		if file, hdr, ferr := r.FormFile("resolved_image"); ferr == nil {
			defer file.Close()
			saved, err := a.saveImage(file, hdr.Size)
			if err != nil {
				writeErr(w, http.StatusBadRequest, err.Error())
				return
			}
			newAfter = saved
		}
	}

	if _, err := a.db.Exec("UPDATE reports SET status = ?, resolved_image_url = ?, updated_at = ? WHERE id = ?",
		status, newAfter, nowStr(), id); err != nil {
		if newAfter != afterURL {
			a.removeUpload(newAfter)
		}
		writeErr(w, http.StatusInternalServerError, "Could not update the report")
		return
	}
	if newAfter != afterURL {
		a.removeUpload(afterURL) // replaced by a newer photo
	}
	rp, err := a.getReport(id, u.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "Updated, but could not reload the report")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"report": rp})
}

// ---------- photo storage ----------

// saveImage checks the real file content (not the file name) and stores it under a random name.
func (a *App) saveImage(f multipart.File, size int64) (string, error) {
	if size > maxUpload {
		return "", errors.New("The photo is too large (max 5 MB)")
	}
	head := make([]byte, 512)
	n, _ := io.ReadFull(f, head)
	ext, ok := imageTypes[http.DetectContentType(head[:n])]
	if !ok {
		return "", errors.New("Only JPG, PNG, GIF or WebP photos are allowed")
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return "", errors.New("Could not read the photo")
	}
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", errors.New("Could not store the photo")
	}
	name := hex.EncodeToString(b) + ext
	out, err := os.OpenFile(filepath.Join(a.uploadDir, name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return "", errors.New("Could not store the photo")
	}
	defer out.Close()
	if _, err := io.Copy(out, io.LimitReader(f, maxUpload)); err != nil {
		return "", errors.New("Could not store the photo")
	}
	return "/uploads/" + name, nil
}

// removeUpload deletes a stored photo. filepath.Base stops "../" tricks.
func (a *App) removeUpload(url string) {
	if !strings.HasPrefix(url, "/uploads/") {
		return
	}
	name := filepath.Base(url)
	if strings.HasPrefix(name, "demo-") || strings.HasPrefix(name, "seed-") {
		return // keep the seeded demo pictures (generated or real)
	}
	_ = os.Remove(filepath.Join(a.uploadDir, name))
}
