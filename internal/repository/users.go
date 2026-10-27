package repository

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"forum-authentication/internal/models"
)

// Пакетные ошибки для проверки типовых ситуаций.
var (
	ErrUserExists = errors.New("user already exists")
	ErrNotFound   = errors.New("record not found")
)

// NewUser — данные нового пользователя.
type NewUser struct {
	Username     string
	Email        string
	PasswordHash string
	FirstName    string
	LastName     string
	Age          int
	Gender       string
}

// CreateUser создаёт пользователя и возвращает его id.
func (r *Repository) CreateUser(u NewUser) (int, error) {
	res, err := r.db.Exec(
		`INSERT INTO users (email, username, password, first_name, last_name, age, gender)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		u.Email, u.Username, u.PasswordHash, u.FirstName, u.LastName, u.Age, u.Gender,
	)
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("repository: last insert id: %w", err)
	}
	return int(id), nil
}

// EmailExists возвращает true, если email уже занят.
func (r *Repository) EmailExists(email string) (bool, error) {
	var exists int
	err := r.db.QueryRow("SELECT COUNT(*) FROM users WHERE email = ? AND password <> '!oauth-account'", email).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("repository: check email: %w", err)
	}
	return exists > 0, nil
}

// UsernameExists возвращает true, если имя пользователя уже занято.
func (r *Repository) UsernameExists(username string) (bool, error) {
	var exists int
	err := r.db.QueryRow("SELECT COUNT(*) FROM users WHERE username = ?", username).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("repository: check username: %w", err)
	}
	return exists > 0, nil
}

// UserCredentialsByLogin возвращает id и хэш пароля пользователя,
// найденного по email ИЛИ по никнейму.
func (r *Repository) UserCredentialsByLogin(login string) (id int, passwordHash string, err error) {
	err = r.db.QueryRow(
		"SELECT id, password FROM users WHERE (email = ? OR username = ?) AND password <> '!oauth-account'", login, login,
	).Scan(&id, &passwordHash)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, "", ErrNotFound
	}
	if err != nil {
		return 0, "", fmt.Errorf("repository: select user by login: %w", err)
	}
	return id, passwordHash, nil
}

// UserByID возвращает пользователя по его id.
func (r *Repository) UserByID(id int) (*models.User, error) {
	var u models.User
	err := r.db.QueryRow(
		"SELECT id, username, email, first_name, last_name, age, gender FROM users WHERE id = ?", id,
	).Scan(&u.ID, &u.Username, &u.Email, &u.FirstName, &u.LastName, &u.Age, &u.Gender)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("repository: select user by id: %w", err)
	}
	return &u, nil
}

// UserBySession возвращает пользователя, которому принадлежит сессия,
// если сессия существует и ещё не истекла.
func (r *Repository) UserBySession(token string, now time.Time) (*models.User, error) {
	var u models.User
	err := r.db.QueryRow(`
		SELECT u.id, u.username, u.email, u.first_name, u.last_name, u.age, u.gender
		FROM sessions s
		JOIN users u ON u.id = s.user_id
		WHERE s.token = ? AND s.expiry > ?`, token, now,
	).Scan(&u.ID, &u.Username, &u.Email, &u.FirstName, &u.LastName, &u.Age, &u.Gender)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("repository: select user by session: %w", err)
	}
	return &u, nil
}

// CreateSession сохраняет новую сессию пользователя.
func (r *Repository) CreateSession(userID int, token string, expiry time.Time) error {
	_, err := r.db.Exec(
		"INSERT INTO sessions (user_id, token, expiry) VALUES (?, ?, ?)",
		userID, token, expiry,
	)
	if err != nil {
		return fmt.Errorf("repository: insert session: %w", err)
	}
	return nil
}

// DeleteSessionsByUser удаляет все сессии пользователя (например, перед созданием новой).
func (r *Repository) DeleteSessionsByUser(userID int) error {
	_, err := r.db.Exec("DELETE FROM sessions WHERE user_id = ?", userID)
	if err != nil {
		return fmt.Errorf("repository: delete sessions: %w", err)
	}
	return nil
}

// DeleteSession удаляет одну сессию по токену.
func (r *Repository) DeleteSession(token string) error {
	_, err := r.db.Exec("DELETE FROM sessions WHERE token = ?", token)
	if err != nil {
		return fmt.Errorf("repository: delete session: %w", err)
	}
	return nil
}

// TouchLastSeen обновляет поле last_seen пользователя, найденного по токену сессии.
func (r *Repository) TouchLastSeen(token string, t time.Time) error {
	_, err := r.db.Exec(`
		UPDATE users SET last_seen = ?
		WHERE id = (SELECT user_id FROM sessions WHERE token = ? AND expiry > ?)`,
		t, token, t)
	if err != nil {
		return fmt.Errorf("repository: update last_seen: %w", err)
	}
	return nil
}
