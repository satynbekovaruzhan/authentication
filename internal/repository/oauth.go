package repository

import (
	"database/sql"
	"errors"
	"fmt"

	"forum-authentication/internal/models"
)

type OAuthAccount struct {
	Provider   string
	ProviderID string
	Email      string
	Username   string
}

func (r *Repository) UserByOAuth(provider, providerID string) (*models.User, error) {
	var u models.User
	err := r.db.QueryRow(`SELECT u.id, u.username, u.email, u.first_name, u.last_name, u.age, u.gender
		FROM oauth_accounts oa JOIN users u ON u.id = oa.user_id
		WHERE oa.provider = ? AND oa.provider_id = ?`, provider, providerID).
		Scan(&u.ID, &u.Username, &u.Email, &u.FirstName, &u.LastName, &u.Age, &u.Gender)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("repository: oauth account: %w", err)
	}
	return &u, nil
}

func (r *Repository) LinkOAuthAccount(userID int, provider, providerID string) error {
	_, err := r.db.Exec("INSERT INTO oauth_accounts (user_id, provider, provider_id) VALUES (?, ?, ?)", userID, provider, providerID)
	if err != nil {
		return fmt.Errorf("repository: link oauth account: %w", err)
	}
	return nil
}

func (r *Repository) UserByEmail(email string) (*models.User, error) {
	var u models.User
	err := r.db.QueryRow("SELECT id, username, email, first_name, last_name, age, gender FROM users WHERE email = ?", email).
		Scan(&u.ID, &u.Username, &u.Email, &u.FirstName, &u.LastName, &u.Age, &u.Gender)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("repository: user by email: %w", err)
	}
	return &u, nil
}

func (r *Repository) CreateOAuthUser(account OAuthAccount) (int, error) {
	tx, err := r.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	res, err := tx.Exec(`INSERT INTO users (email, username, password, first_name, last_name)
        VALUES (?, ?, ?, ?, ?)`, account.Email, account.Username, "!oauth-account", account.Username, "")
	if err != nil {
		return 0, fmt.Errorf("repository: create oauth user: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	if _, err := tx.Exec("INSERT INTO oauth_accounts (user_id, provider, provider_id) VALUES (?, ?, ?)", id, account.Provider, account.ProviderID); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return int(id), nil
}
