package repository

import (
	"context"
	"database/sql"
	"fmt"
)

// Rebuild the old unique-email table without deleting forum data. OAuth
// identities own separate users; only password accounts require unique emails.
const separateOAuthSchema = `
CREATE TABLE users_separate (
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
INSERT INTO users_separate SELECT id, email, username, password, first_name,
 last_name, age, gender, last_seen FROM users;
DROP TABLE users;
ALTER TABLE users_separate RENAME TO users;
CREATE UNIQUE INDEX users_password_email ON users(email)
 WHERE password <> '!oauth-account';
`

func migrateSeparateOAuthAccounts(db *sql.DB) error {
	ctx := context.Background()
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (name TEXT PRIMARY KEY)`); err != nil {
		return err
	}
	var done int
	if err := conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE name = 'separate_oauth_v1'`).Scan(&done); err != nil {
		return err
	}
	if done != 0 {
		return nil
	}
	// PRAGMA must run outside the transaction, on the same connection.
	if _, err := conn.ExecContext(ctx, "PRAGMA foreign_keys = OFF"); err != nil {
		return err
	}
	defer conn.ExecContext(ctx, "PRAGMA foreign_keys = ON")
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(separateOAuthSchema); err != nil {
		return fmt.Errorf("separate OAuth schema: %w", err)
	}

	type link struct {
		id, userID         int
		provider, password string
	}
	rows, err := tx.Query(`SELECT oa.id, oa.user_id, oa.provider, u.password
        FROM oauth_accounts oa JOIN users u ON u.id = oa.user_id
        ORDER BY oa.user_id, CASE WHEN oa.provider = 'google' THEN 0 ELSE 1 END, oa.id`)
	if err != nil {
		return err
	}
	var links []link
	for rows.Next() {
		var l link
		if err := rows.Scan(&l.id, &l.userID, &l.provider, &l.password); err != nil {
			rows.Close()
			return err
		}
		links = append(links, l)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	kept := make(map[int]bool)
	for _, l := range links {
		// Keep Google on the original OAuth-only account when available.
		// Password accounts retain their original user and all existing data.
		if l.password == "!oauth-account" && !kept[l.userID] {
			kept[l.userID] = true
			continue
		}
		base := fmt.Sprintf("%s_%d", l.provider, l.id)
		username := base
		for suffix := 1; ; suffix++ {
			var count int
			if err := tx.QueryRow("SELECT COUNT(*) FROM users WHERE username = ?", username).Scan(&count); err != nil {
				return err
			}
			if count == 0 {
				break
			}
			username = fmt.Sprintf("%s_%d", base, suffix)
		}
		result, err := tx.Exec(`INSERT INTO users (email, username, password, first_name, last_name, age, gender)
            SELECT email, ?, '!oauth-account', first_name, last_name, age, gender FROM users WHERE id = ?`, username, l.userID)
		if err != nil {
			return err
		}
		newID, err := result.LastInsertId()
		if err != nil {
			return err
		}
		if _, err := tx.Exec("UPDATE oauth_accounts SET user_id = ? WHERE id = ?", newID, l.id); err != nil {
			return err
		}
		if _, err := tx.Exec("DELETE FROM sessions WHERE user_id = ?", l.userID); err != nil {
			return err
		}
	}
	checks, err := tx.Query("PRAGMA foreign_key_check")
	if err != nil {
		return err
	}
	invalid := checks.Next()
	err = checks.Err()
	checks.Close()
	if err != nil {
		return err
	}
	if invalid {
		return fmt.Errorf("separate OAuth migration: foreign-key check failed; changes rolled back")
	}
	if _, err := tx.Exec("INSERT INTO schema_migrations (name) VALUES ('separate_oauth_v1')"); err != nil {
		return err
	}
	return tx.Commit()
}
