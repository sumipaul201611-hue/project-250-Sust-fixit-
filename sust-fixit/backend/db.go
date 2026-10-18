package main

import (
	"database/sql"
	"fmt"
)

const schema = `
CREATE TABLE IF NOT EXISTS users (
	id            INTEGER PRIMARY KEY AUTOINCREMENT,
	student_id    TEXT NOT NULL UNIQUE,
	name          TEXT NOT NULL,
	email         TEXT NOT NULL UNIQUE,
	password_hash TEXT NOT NULL,
	department    TEXT NOT NULL DEFAULT '',
	role          TEXT NOT NULL DEFAULT 'student' CHECK (role IN ('student','admin')),
	created_at    TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS reports (
	id                 INTEGER PRIMARY KEY AUTOINCREMENT,
	user_id            INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	title              TEXT NOT NULL,
	description        TEXT NOT NULL,
	category           TEXT NOT NULL,
	building           TEXT NOT NULL,
	location           TEXT NOT NULL,
	image_url          TEXT NOT NULL,
	resolved_image_url TEXT NOT NULL DEFAULT '',
	status             TEXT NOT NULL DEFAULT 'reported'
	                   CHECK (status IN ('reported','verified','in_progress','resolved')),
	created_at         TEXT NOT NULL,
	updated_at         TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS upvotes (
	user_id    INTEGER NOT NULL REFERENCES users(id)   ON DELETE CASCADE,
	report_id  INTEGER NOT NULL REFERENCES reports(id) ON DELETE CASCADE,
	created_at TEXT NOT NULL,
	PRIMARY KEY (user_id, report_id) -- one student = one vote per report
);

CREATE INDEX IF NOT EXISTS idx_reports_status   ON reports(status);
CREATE INDEX IF NOT EXISTS idx_reports_building ON reports(building);
CREATE INDEX IF NOT EXISTS idx_upvotes_report   ON upvotes(report_id);
`

func openDB(path string) (*sql.DB, error) {
	db, err := sql.Open(driverName, path)
	if err != nil {
		return nil, err
	}
	// One connection keeps SQLite simple and makes the PRAGMAs below apply everywhere.
	// Rule for this project: never run a second query while looping over rows.
	db.SetMaxOpenConns(1)
	for _, p := range []string{
		"PRAGMA journal_mode = WAL",
		"PRAGMA foreign_keys = ON",
		"PRAGMA busy_timeout = 5000",
	} {
		if _, err := db.Exec(p); err != nil {
			return nil, fmt.Errorf("%s: %w", p, err)
		}
	}
	if _, err := db.Exec(schema); err != nil {
		return nil, fmt.Errorf("create schema: %w", err)
	}
	return db, nil
}
