package database

import (
	"database/sql"
	"log"
	"os"
	"path/filepath"

	_ "github.com/mattn/go-sqlite3"
)

var DB *sql.DB

func Init() {
	dbPath := filepath.Join(".", "data", "skud.db")
	os.MkdirAll(filepath.Dir(dbPath), 0755)

	var err error
	DB, err = sql.Open("sqlite3", dbPath)
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}

	if err = DB.Ping(); err != nil {
		log.Fatalf("Failed to ping database: %v", err)
	}

	createTables()
	log.Println("Database initialized successfully")
}

func createTables() {
	usersTable := `
	CREATE TABLE IF NOT EXISTS users (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		username TEXT UNIQUE NOT NULL,
		password_hash TEXT NOT NULL,
		full_name TEXT NOT NULL,
		role TEXT NOT NULL DEFAULT 'user',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);`

	qrCodesTable := `
	CREATE TABLE IF NOT EXISTS qr_codes (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		user_id INTEGER NOT NULL,
		token TEXT NOT NULL UNIQUE,
		user_data TEXT NOT NULL,
		issued_at DATETIME NOT NULL,
		expires_at DATETIME NOT NULL,
		is_used BOOLEAN DEFAULT 0,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (user_id) REFERENCES users(id)
	);`

	accessLogsTable := `
	CREATE TABLE IF NOT EXISTS access_logs (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		qr_token TEXT NOT NULL,
		user_id INTEGER,
		action TEXT NOT NULL,
		status TEXT NOT NULL,
		message TEXT,
		scanned_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (user_id) REFERENCES users(id)
	);`

	if _, err := DB.Exec(usersTable); err != nil {
		log.Fatalf("Failed to create users table: %v", err)
	}
	if _, err := DB.Exec(qrCodesTable); err != nil {
		log.Fatalf("Failed to create qr_codes table: %v", err)
	}
	if _, err := DB.Exec(accessLogsTable); err != nil {
		log.Fatalf("Failed to create access_logs table: %v", err)
	}

	// Seed default admin user if not exists
	var count int
	DB.QueryRow("SELECT COUNT(*) FROM users").Scan(&count)
	if count == 0 {
		seedDefaultUsers()
	}
}

func seedDefaultUsers() {
	// admin / admin123
	// user1 / user123
	// user2 / user123
	users := []struct {
		username     string
		passwordHash string
		fullName     string
		role         string
	}{
		{"admin", "$2a$10$4QlSmkiL2zBUM0HX0vAuluozOKbYdVSXCGK/EuPwFqpiq5QR0bqIi", "Администратор", "admin"},
		{"user1", "$2a$10$2697g5WnJI7lGNUzeirPd.WS9TA8EMsURM3JSXSZHKwThFxKHA1VC", "Иванов Иван", "user"},
		{"user2", "$2a$10$2697g5WnJI7lGNUzeirPd.WS9TA8EMsURM3JSXSZHKwThFxKHA1VC", "Петров Петр", "user"},
	}

	for _, u := range users {
		_, err := DB.Exec(
			"INSERT INTO users (username, password_hash, full_name, role) VALUES (?, ?, ?, ?)",
			u.username, u.passwordHash, u.fullName, u.role,
		)
		if err != nil {
			log.Printf("Warning: failed to seed user %s: %v", u.username, err)
		}
	}
	log.Println("Default users seeded")
}
