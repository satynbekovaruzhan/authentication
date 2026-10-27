package repository

import (
	"database/sql"
	"fmt"

	_ "github.com/mattn/go-sqlite3"
)

// Repository — обёртка над соединением с SQLite,
// содержащая все запросы к базе данных.
type Repository struct {
	db *sql.DB
}

// Open открывает (и при необходимости создаёт) базу данных по указанному пути,
// включает поддержку внешних ключей и создаёт структуру таблиц.
func Open(path string) (*Repository, error) {
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		return nil, fmt.Errorf("repository: open sqlite (%s): %w", path, err)
	}

	// Для in-memory баз каждое соединение хранит свою копию БД,
	// поэтому ограничиваем пул одним соединением.
	if path == ":memory:" {
		db.SetMaxOpenConns(1)
	}

	if _, err := db.Exec("PRAGMA foreign_keys = ON;"); err != nil {
		db.Close()
		return nil, fmt.Errorf("repository: enable foreign keys: %w", err)
	}

	if err := createSchema(db); err != nil {
		db.Close()
		return nil, err
	}

	if err := migrateSeparateOAuthAccounts(db); err != nil {
		db.Close()
		return nil, err
	}

	if err := seedCategories(db); err != nil {
		db.Close()
		return nil, err
	}

	return &Repository{db: db}, nil
}

// Close закрывает соединение с базой данных.
func (r *Repository) Close() error {
	return r.db.Close()
}

func createSchema(db *sql.DB) error {
	const schema = `
CREATE TABLE IF NOT EXISTS users (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	email TEXT NOT NULL,
	username TEXT UNIQUE NOT NULL,
	password TEXT NOT NULL,
	first_name TEXT NOT NULL DEFAULT '',
	last_name TEXT NOT NULL DEFAULT '',
	age INTEGER NOT NULL DEFAULT 0,
	gender TEXT NOT NULL DEFAULT '',
	last_seen DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS sessions (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	user_id INTEGER NOT NULL,
	token TEXT UNIQUE NOT NULL,
	expiry DATETIME NOT NULL,
	FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS oauth_accounts (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	user_id INTEGER NOT NULL,
	provider TEXT NOT NULL,
	provider_id TEXT NOT NULL,
	UNIQUE(provider, provider_id),
	FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS posts (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	user_id INTEGER NOT NULL,
	title TEXT NOT NULL,
	content TEXT NOT NULL,
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS categories (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	name TEXT UNIQUE NOT NULL
);

CREATE TABLE IF NOT EXISTS post_categories (
	post_id INTEGER NOT NULL,
	category_id INTEGER NOT NULL,
	PRIMARY KEY (post_id, category_id),
	FOREIGN KEY(post_id) REFERENCES posts(id) ON DELETE CASCADE,
	FOREIGN KEY(category_id) REFERENCES categories(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS comments (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	post_id INTEGER NOT NULL,
	user_id INTEGER NOT NULL,
	content TEXT NOT NULL,
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	FOREIGN KEY(post_id) REFERENCES posts(id) ON DELETE CASCADE,
	FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS reactions (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	user_id INTEGER NOT NULL,
	post_id INTEGER,
	comment_id INTEGER,
	vote INTEGER NOT NULL CHECK (vote IN (1, -1)),
	FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE,
	FOREIGN KEY(post_id) REFERENCES posts(id) ON DELETE CASCADE,
	FOREIGN KEY(comment_id) REFERENCES comments(id) ON DELETE CASCADE,
	CHECK (post_id IS NOT NULL OR comment_id IS NOT NULL),
	UNIQUE(user_id, post_id),
	UNIQUE(user_id, comment_id)
);

CREATE TABLE IF NOT EXISTS messages (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	sender_id INTEGER NOT NULL,
	receiver_id INTEGER NOT NULL,
	content TEXT NOT NULL,
	created_at DATETIME NOT NULL,
	FOREIGN KEY(sender_id) REFERENCES users(id) ON DELETE CASCADE,
	FOREIGN KEY(receiver_id) REFERENCES users(id) ON DELETE CASCADE
);
`

	if _, err := db.Exec(schema); err != nil {
		return fmt.Errorf("repository: create schema: %w", err)
	}

	// Миграция существующей базы: добавляем новые поля пользователя,
	// если таблица была создана до их появления.
	migrations := []struct{ name, decl string }{
		{"first_name", "TEXT NOT NULL DEFAULT ''"},
		{"last_name", "TEXT NOT NULL DEFAULT ''"},
		{"age", "INTEGER NOT NULL DEFAULT 0"},
		{"gender", "TEXT NOT NULL DEFAULT ''"},
		{"last_seen", "DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP"},
	}
	for _, m := range migrations {
		if err := ensureColumn(db, "users", m.name, m.decl); err != nil {
			return err
		}
	}
	return nil
}

// ensureColumn добавляет колонку в таблицу, если её там ещё нет.
func ensureColumn(db *sql.DB, table, name, decl string) error {
	rows, err := db.Query(fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return fmt.Errorf("repository: read table info %s: %w", table, err)
	}
	defer rows.Close()

	for rows.Next() {
		var cid, notnull, pk int
		var colName, colType string
		var dflt sql.NullString
		if err := rows.Scan(&cid, &colName, &colType, &notnull, &dflt, &pk); err != nil {
			return fmt.Errorf("repository: scan table info %s: %w", table, err)
		}
		if colName == name {
			return rows.Err()
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("repository: iterate table info %s: %w", table, err)
	}

	if _, err := db.Exec(fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", table, name, decl)); err != nil {
		return fmt.Errorf("repository: add column %s.%s: %w", table, name, err)
	}
	return nil
}

func seedCategories(db *sql.DB) error {
	categories := []string{"golang", "docker", "projects", "01edu"}
	for _, name := range categories {
		_, err := db.Exec("INSERT OR IGNORE INTO categories (name) VALUES (?)", name)
		if err != nil {
			return fmt.Errorf("repository: seed category %q: %w", name, err)
		}
	}
	return nil
}
