package main

import (
	"embed"
	"fmt"
	"html"
	"log"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// Real campus photos, bundled into the binary so seeding works regardless of
// the working directory the server is started from. See seed_assets/.
//
//go:embed seed_assets/*.jpg
var seedPhotos embed.FS

// seedDemoData fills an empty database with example students and problems,
// so the presentation does not start with a blank page.
// Run with:  go run . -seed
func (a *App) seedDemoData() error {
	var n int
	if err := a.db.QueryRow("SELECT COUNT(*) FROM reports").Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		log.Println("seed: database already has reports, skipping")
		return nil
	}

	hash, err := bcrypt.GenerateFromPassword([]byte("student123"), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	ts := func(daysAgo int) string { return now.AddDate(0, 0, -daysAgo).Format(time.RFC3339) }

	addUser := func(sid, name, email, dept string) (int64, error) {
		res, err := a.db.Exec(
			`INSERT INTO users (student_id, name, email, password_hash, department, role, created_at)
			 VALUES (?, ?, ?, ?, ?, 'student', ?)`, sid, name, email, string(hash), dept, ts(30))
		if err != nil {
			return 0, err
		}
		return res.LastInsertId()
	}

	rafi, err := addUser("2021331001", "Rafi Ahmed", "rafi@student.sust.edu", "CSE")
	if err != nil {
		return err
	}
	nusrat, err := addUser("2021331002", "Nusrat Jahan", "nusrat@student.sust.edu", "SWE")
	if err != nil {
		return err
	}
	tanvir, err := addUser("2021331003", "Tanvir Hasan", "tanvir@student.sust.edu", "EEE")
	if err != nil {
		return err
	}

	userHash, err := bcrypt.GenerateFromPassword([]byte("12345678"), bcrypt.DefaultCost)
	if err == nil {
		_, _ = a.db.Exec(
			`INSERT INTO users (student_id, name, email, password_hash, department, role, created_at)
			 VALUES ('2023331060', 'SUST Student', '2023331060@student.sust.edu', ?, 'CSE', 'student', ?)`, string(userHash), ts(30))
		_, _ = a.db.Exec(
			`INSERT INTO users (student_id, name, email, password_hash, department, role, created_at)
			 VALUES ('2023331060B', 'SUST Student', '2023331060@sttudent.sust.edu', ?, 'CSE', 'student', ?)`, string(userHash), ts(30))
	}

	owners := []int64{rafi, nusrat, tanvir}

	// 25 extra students who will upvote.
	var voters []int64
	for i := 1; i <= 25; i++ {
		id, err := addUser(fmt.Sprintf("DEMO%03d", i), fmt.Sprintf("Demo Student %d", i),
			fmt.Sprintf("demo%03d@student.sust.edu", i), "CSE")
		if err != nil {
			return err
		}
		voters = append(voters, id)
	}

	// photo: an embedded seed_assets/<file> copied into uploads/ once.
	// noPhoto: this report is seeded with no photo at all, the normal case
	// for "photo is optional" — the feed shows the category placeholder.
	type item struct {
		title, desc, cat, building, loc, status string
		votes, daysAgo                          int
		photo                                   string // seed_assets filename, or "" for no photo
		afterSVG                                string // label for a generated "after" photo, or ""
	}
	items := []item{
		// Real campus photos (the fan issue is reported separately for three
		// rooms — same category and building but different exact location,
		// which is exactly what the duplicate-suggestion check is meant to
		// catch: students report Gallery 1 and Gallery 2 separately because
		// the location genuinely differs, while a second "Gallery 1" report
		// would get flagged as a likely duplicate).
		{"Projector not displaying in Gallery 1", "The ceiling-mounted projector turns on but shows no image on the screen. Classes are being held without slides.",
			"Electrical", "IICT Building", "Gallery 1 (Seminar Room)", "in_progress", 19, 5, "projector-gallery1.jpg", ""},
		{"Ceiling fan not working in Gallery 1", "The ceiling fan does not turn on at all. The room gets uncomfortably hot during afternoon sessions.",
			"Electrical", "IICT Building", "Gallery 1 (Seminar Room)", "reported", 9, 3, "ceiling-fan.jpg", ""},
		{"Ceiling fan not working in Gallery 2", "Same issue as Gallery 1 — the fan doesn't start even after multiple switch toggles.",
			"Electrical", "IICT Building", "Gallery 2 (Seminar Room)", "reported", 6, 2, "ceiling-fan.jpg", ""},
		{"Ceiling fan not working in the common room", "The fan in the shared common room has stopped completely, affecting everyone who uses the space between classes.",
			"Electrical", "Shah Paran Hall", "Common room", "verified", 14, 7, "common-room.jpg", ""},
		{"Large waterlogged pothole on the road in front of IICT", "A deep pothole fills with muddy water whenever it rains, making the road in front of IICT hard to cross on foot, bike, or rickshaw.",
			"Roads & Infrastructure", "Roads & Campus Grounds", "Main road in front of IICT Building", "reported", 31, 6, "road-pothole.jpg", ""},
		{"Hand-wash basin broken, no water on 2nd floor", "The basin tap is damaged and there's no running water at all on this floor, so students can't wash their hands between labs.",
			"Plumbing & Water", "IICT Building", "2nd floor washroom", "verified", 11, 4, "basin-no-water.jpg", ""},

		// A few reports filed with no photo — food quality, wifi outage,
		// or broken desks where students choose not to take a photo.
		// The feed shows a category placeholder for these instead of a broken image.
		{"Wifi not working in the reading room", "No wifi signal at all since this morning in the reading room [SSID: SUST-STUDENT | Problem: No connection] — couldn't take a photo on my phone either since it also needs wifi to upload right now.",
			"Wi-Fi & Internet", "Central Library", "2nd floor reading room", "in_progress", 23, 1, "", ""},
		{"Undercooked rice and spoiled lentils in Shah Paran Dining", "Lunch served today had stale smelling lentils and hard undercooked rice [Dining: Shah Paran Hall Mess 2 | Meal: Lunch | Issue: Quality & Hygiene]. Multiple students had to skip their meal.",
			"Food & Dining", "Shah Paran Hall", "Mess 2 (Ground Floor)", "verified", 27, 2, "", ""},
		{"Broken chairs and desks in Room 204", "Four chairs in the back rows have broken arm rests and one has a loose leg.",
			"Furniture & Facilities", "Academic Building B", "Room 204, 2nd floor", "reported", 7, 2, "", ""},
		{"Overflowing dustbin near the cafeteria", "The dustbin is full by noon every day and waste spreads to the entrance.",
			"Cleanliness & Sanitation", "University Center & Cafeteria", "Front entrance", "reported", 12, 1, "", ""},

		// One fully resolved example with a generated before/after pair, to
		// demo the resolution-photo flow end to end.
		{"Corridor lights are off after dark", "Three of the corridor tube lights do not work, so the stairs are unsafe in the evening.",
			"Electrical", "Academic Building A", "2nd floor corridor", "resolved", 15, 12, "", "Corridor lights working (after)"},
	}

	svg := func(name, color, label string) (string, error) {
		body := fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 640 420">
<rect width="640" height="420" fill="%s"/>
<rect x="24" y="24" width="592" height="372" fill="none" stroke="#ffffff" stroke-opacity=".55" stroke-width="3" stroke-dasharray="14 10"/>
<text x="320" y="200" text-anchor="middle" font-family="Arial, sans-serif" font-size="34" font-weight="700" fill="#ffffff">%s</text>
<text x="320" y="245" text-anchor="middle" font-family="Arial, sans-serif" font-size="18" fill="#ffffff" fill-opacity=".8">demo photo</text>
</svg>`, color, html.EscapeString(label))
		if err := os.WriteFile(filepath.Join(a.uploadDir, name), []byte(body), 0o644); err != nil {
			return "", err
		}
		return "/uploads/" + name, nil
	}

	// copies an embedded real photo into uploads/ once, named seed-<file>.
	copyRealPhoto := func(assetName string) (string, error) {
		outName := "seed-" + assetName
		outPath := filepath.Join(a.uploadDir, outName)
		if _, err := os.Stat(outPath); err == nil {
			return "/uploads/" + outName, nil // already copied (re-run safety)
		}
		data, err := seedPhotos.ReadFile("seed_assets/" + assetName)
		if err != nil {
			return "", fmt.Errorf("read embedded %s: %w", assetName, err)
		}
		if err := os.WriteFile(outPath, data, 0o644); err != nil {
			return "", fmt.Errorf("write %s: %w", outPath, err)
		}
		return "/uploads/" + outName, nil
	}

	for i, it := range items {
		var img string
		var err error
		switch {
		case it.photo != "":
			img, err = copyRealPhoto(it.photo)
		default:
			// no photo seeded — leave img empty so the frontend shows the
			// category placeholder, exactly like a real "skipped the photo" report.
		}
		if err != nil {
			return err
		}

		after := ""
		if it.afterSVG != "" {
			if after, err = svg(fmt.Sprintf("demo-%d-after.svg", i+1), "#1f9d6e", it.afterSVG); err != nil {
				return err
			}
		}

		var exists int
		_ = a.db.QueryRow("SELECT COUNT(*) FROM reports WHERE title = ? AND building = ? AND location = ?", it.title, it.building, it.loc).Scan(&exists)
		if exists > 0 {
			continue
		}

		res, err := a.db.Exec(
			`INSERT INTO reports (user_id, title, description, category, building, location, image_url, resolved_image_url, status, created_at, updated_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			owners[i%len(owners)], it.title, it.desc, it.cat, it.building, it.loc, img, after, it.status, ts(it.daysAgo), ts(it.daysAgo/2))
		if err != nil {
			return err
		}
		rid, _ := res.LastInsertId()
		for v := 0; v < it.votes && v < len(voters); v++ {
			if _, err := a.db.Exec("INSERT INTO upvotes (user_id, report_id, created_at) VALUES (?, ?, ?)", voters[v], rid, ts(it.daysAgo)); err != nil {
				return err
			}
		}
		// give the top item a few more votes from the three named students too
		if it.votes >= 25 {
			for _, uid := range owners {
				_, _ = a.db.Exec("INSERT OR IGNORE INTO upvotes (user_id, report_id, created_at) VALUES (?, ?, ?)", uid, rid, ts(it.daysAgo))
			}
		}
	}
	log.Println("seed: demo data loaded (students: rafi@student.sust.edu / student123)")
	return nil
}
