package main

// The only file that knows which SQLite driver is used.
// modernc.org/sqlite is pure Go, so no C compiler is needed (works easily on Windows).
import _ "modernc.org/sqlite"

const driverName = "sqlite"
