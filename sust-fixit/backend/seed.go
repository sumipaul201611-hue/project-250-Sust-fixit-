package main

import (
	"fmt"
	"html"
	"log"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/crypto/bcrypt"
)

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

	type item struct {
		title, desc, cat, building, loc, status string
		votes, daysAgo                          int
		color, label                            string
		afterLabel                              string
	}
	items := []item{
		{"Ceiling fan not working in Software Lab 2", "The fan above the third row has stopped completely. The lab gets very hot in the afternoon classes.",
			"Electrical", "IICT Building", "Software Lab 2, 3rd floor", "in_progress", 24, 6, "#5b7fa6", "Broken ceiling fan", ""},
		{"Water leaking from the ceiling near the server room", "Water drips from the ceiling onto the floor beside the server room door. The floor is slippery.",
			"Plumbing", "IICT Building", "Ground floor, beside the server room", "verified", 18, 4, "#4a8a94", "Ceiling water leak", ""},
		{"Potholes on the road in front of IICT", "Two large potholes near the entrance fill with water when it rains. Rickshaws and bikes struggle to pass.",
			"Infrastructure", "Roads & Ground", "Road in front of IICT", "reported", 12, 3, "#8a7b5b", "Potholes on the road", ""},
		{"Broken chairs in Room 204", "Four chairs in the back rows have broken arm rests and one has a loose leg.",
			"Furniture", "Academic Building B", "Room 204, 2nd floor", "reported", 7, 2, "#8b6a5c", "Broken classroom chairs", ""},
		{"Corridor lights are off after dark", "Three of the corridor tube lights do not work, so the stairs are unsafe in the evening.",
			"Electrical", "Academic Building A", "2nd floor corridor", "resolved", 15, 12, "#6c6f8f", "Dark corridor (before)", "Corridor lights working (after)"},
		{"Overflowing dustbin near the cafeteria", "The dustbin is full by noon every day and waste spreads to the entrance.",
			"Cleanliness", "Cafeteria", "Front entrance", "reported", 4, 1, "#6f8a5b", "Overflowing dustbin", ""},
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

	for i, it := range items {
		img, err := svg(fmt.Sprintf("demo-%d.svg", i+1), it.color, it.label)
		if err != nil {
			return err
		}
		after := ""
		if it.afterLabel != "" {
			if after, err = svg(fmt.Sprintf("demo-%d-after.svg", i+1), "#3f8f66", it.afterLabel); err != nil {
				return err
			}
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
		// the first item has 24 voters; give it 8 more from the three real students for a bigger number
		if i == 0 {
			for _, uid := range owners {
				_, _ = a.db.Exec("INSERT OR IGNORE INTO upvotes (user_id, report_id, created_at) VALUES (?, ?, ?)", uid, rid, ts(it.daysAgo))
			}
		}
	}
	log.Println("seed: demo data loaded (students: rafi@student.sust.edu / student123)")
	return nil
}
