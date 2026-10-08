package database

import (
	"database/sql"
	"log"
	"os"
	"path/filepath"

	// Чисто-Go драйвер SQLite: не требует CGO, поэтому backend собирается
	// на Windows без WSL/MinGW обычной командой `go build`.
	_ "modernc.org/sqlite"
)

var DB *sql.DB

func Init() {
	dbPath := filepath.Join(".", "data", "skud.db")
	os.MkdirAll(filepath.Dir(dbPath), 0755)

	var err error
	DB, err = sql.Open("sqlite", dbPath)
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
		code_word TEXT NOT NULL DEFAULT '',
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

	// Миграция для баз, созданных до появления «кодового слова»
	ensureColumn("users", "code_word", "TEXT NOT NULL DEFAULT ''")

	// Seed default admin user if not exists
	var count int
	DB.QueryRow("SELECT COUNT(*) FROM users").Scan(&count)
	if count == 0 {
		seedDefaultUsers()
	}
}

// ensureColumn добавляет колонку в существующую таблицу, если её ещё нет.
// Нужно, чтобы новые поля (например, «кодовое слово») появлялись и в старой базе,
// без пересоздания таблицы и потери данных.
func ensureColumn(table, column, ddl string) {
	rows, err := DB.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		log.Printf("Warning: cannot read schema of %s: %v", table, err)
		return
	}
	defer rows.Close()

	for rows.Next() {
		var cid, notNull, pk int
		var name, colType string
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &colType, &notNull, &dflt, &pk); err != nil {
			continue
		}
		if name == column {
			return // колонка уже есть
		}
	}

	if _, err := DB.Exec("ALTER TABLE " + table + " ADD COLUMN " + column + " " + ddl); err != nil {
		log.Printf("Warning: cannot add column %s.%s: %v", table, column, err)
		return
	}
	log.Printf("Migration: added column %s.%s", table, column)
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
		codeWord     string
	}{
		{"admin", "$2a$10$4QlSmkiL2zBUM0HX0vAuluozOKbYdVSXCGK/EuPwFqpiq5QR0bqIi", "Администратор", "admin", "АЛЬФА"},
		{"user1", "$2a$10$2697g5WnJI7lGNUzeirPd.WS9TA8EMsURM3JSXSZHKwThFxKHA1VC", "Иванов Иван", "user", "РОМАШКА"},
		{"user2", "$2a$10$2697g5WnJI7lGNUzeirPd.WS9TA8EMsURM3JSXSZHKwThFxKHA1VC", "Петров Петр", "user", "ТОПОЛЬ"},
	}

	for _, u := range users {
		_, err := DB.Exec(
			"INSERT INTO users (username, password_hash, full_name, role, code_word) VALUES (?, ?, ?, ?, ?)",
			u.username, u.passwordHash, u.fullName, u.role, u.codeWord,
		)
		if err != nil {
			log.Printf("Warning: failed to seed user %s: %v", u.username, err)
		}
	}
	log.Println("Default users seeded")
}
