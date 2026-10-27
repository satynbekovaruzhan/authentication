package repository

import (
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"forum-authentication/internal/models"
)

// OnlineWindow — период, в течение которого пользователь считается «в сети».
const OnlineWindow = 5 * time.Minute

// CreateMessage сохраняет личное сообщение от senderID к receiverID
// и возвращает его id.
func (r *Repository) CreateMessage(senderID, receiverID int, content string) (int64, error) {
	res, err := r.db.Exec(
		"INSERT INTO messages (sender_id, receiver_id, content, created_at) VALUES (?, ?, ?, ?)",
		senderID, receiverID, content, time.Now(),
	)
	if err != nil {
		return 0, fmt.Errorf("repository: insert message: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("repository: message id: %w", err)
	}
	return id, nil
}

// MessageByID возвращает сообщение по его id вместе с именем отправителя.
func (r *Repository) MessageByID(id int64) (models.Message, error) {
	var m models.Message
	err := r.db.QueryRow(`
		SELECT m.id, m.sender_id, u.username, m.content, m.created_at
		FROM messages m
		JOIN users u ON u.id = m.sender_id
		WHERE m.id = ?`, id).
		Scan(&m.ID, &m.SenderID, &m.Sender, &m.Content, &m.CreatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return m, ErrNotFound
		}
		return m, fmt.Errorf("repository: select message by id: %w", err)
	}
	return m, nil
}

// Conversations возвращает список диалогов пользователя
// (собеседник + последнее сообщение), новых сверху.
func (r *Repository) Conversations(userID int) ([]models.Conversation, error) {
	rows, err := r.db.Query(`
		SELECT m.sender_id, s.username, m.receiver_id, rcv.username, m.content, m.created_at
		FROM messages m
		JOIN users s ON s.id = m.sender_id
		JOIN users rcv ON rcv.id = m.receiver_id
		WHERE m.sender_id = ? OR m.receiver_id = ?
		ORDER BY m.created_at DESC, m.id DESC`, userID, userID)
	if err != nil {
		return nil, fmt.Errorf("repository: select conversations: %w", err)
	}
	defer rows.Close()

	var conversations []models.Conversation
	seen := map[int]bool{}
	for rows.Next() {
		var senderID, receiverID int
		var senderName, receiverName, content string
		var createdAt time.Time
		if err := rows.Scan(&senderID, &senderName, &receiverID, &receiverName, &content, &createdAt); err != nil {
			return nil, fmt.Errorf("repository: scan conversation: %w", err)
		}

		otherID, otherName := senderID, senderName
		if senderID == userID {
			otherID, otherName = receiverID, receiverName
		}
		if seen[otherID] {
			continue
		}
		seen[otherID] = true

		conversations = append(conversations, models.Conversation{
			OtherUser: models.User{ID: otherID, Username: otherName},
			LastMsg:   content,
			LastTime:  createdAt,
		})
	}
	return conversations, rows.Err()
}

// MessagesBetween возвращает переписку двух пользователей,
// отсортированную от старых к новым.
func (r *Repository) MessagesBetween(userID, otherID int) ([]models.Message, error) {
	return r.MessagePage(userID, otherID, 0, 1<<31-1)
}

// MessagePage возвращает страницу переписки двух пользователей
// от старых к новым: не более limit сообщений, созданных до beforeID
// (имействует последние limit, если beforeID = 0).
func (r *Repository) MessagePage(userID, otherID, beforeID, limit int) ([]models.Message, error) {
	query := `
		SELECT m.id, m.sender_id, u.username, m.content, m.created_at
		FROM messages m
		JOIN users u ON u.id = m.sender_id
		WHERE (m.sender_id = ? AND m.receiver_id = ?)
		   OR (m.sender_id = ? AND m.receiver_id = ?)`
	args := []any{userID, otherID, otherID, userID}
	if beforeID > 0 {
		query += " AND m.id < ?"
		args = append(args, beforeID)
	}
	query += " ORDER BY m.id DESC LIMIT ?"
	args = append(args, limit)

	rows, err := r.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("repository: select message page: %w", err)
	}
	defer rows.Close()

	var descending []models.Message
	for rows.Next() {
		var m models.Message
		if err := rows.Scan(&m.ID, &m.SenderID, &m.Sender, &m.Content, &m.CreatedAt); err != nil {
			return nil, fmt.Errorf("repository: scan message page: %w", err)
		}
		descending = append(descending, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("repository: iterate message page: %w", err)
	}

	// Разворачиваем, чтобы вернуть от старых к новым.
	messages := make([]models.Message, 0, len(descending))
	for i := len(descending) - 1; i >= 0; i-- {
		messages = append(messages, descending[i])
	}
	return messages, nil
}

// UsersExcept возвращает всех пользователей, кроме указанного
// (для выбора получателя нового сообщения).
func (r *Repository) UsersExcept(userID int) ([]models.User, error) {
	rows, err := r.db.Query(`
		SELECT id, username, email, first_name, last_name, age, gender FROM users WHERE id != ?
		ORDER BY username`, userID)
	if err != nil {
		return nil, fmt.Errorf("repository: select users: %w", err)
	}
	defer rows.Close()

	var users []models.User
	for rows.Next() {
		var u models.User
		if err := rows.Scan(&u.ID, &u.Username, &u.Email, &u.FirstName, &u.LastName, &u.Age, &u.Gender); err != nil {
			return nil, fmt.Errorf("repository: scan user: %w", err)
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

// Contacts возвращает список контактов для чата пользователя:
// сначала пользователи, с которыми есть переписка, — по принципу
// последнего отправленного сообщения (новые сверху), затем остальные —
// в алфавитном порядке. У каждого контакта — статус «online».
func (r *Repository) Contacts(userID int, now time.Time) ([]models.Contact, error) {
	type row struct {
		u        models.User
		lastSeen sql.NullTime
	}
	rows, err := r.db.Query(`
		SELECT id, username, email, first_name, last_name, age, gender, last_seen
		FROM users WHERE id != ?`, userID)
	if err != nil {
		return nil, fmt.Errorf("repository: select contacts: %w", err)
	}
	var userRows []row
	for rows.Next() {
		var rw row
		if err := rows.Scan(&rw.u.ID, &rw.u.Username, &rw.u.Email, &rw.u.FirstName,
			&rw.u.LastName, &rw.u.Age, &rw.u.Gender, &rw.lastSeen); err != nil {
			rows.Close()
			return nil, fmt.Errorf("repository: scan contact: %w", err)
		}
		userRows = append(userRows, rw)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("repository: close contacts rows: %w", err)
	}

	lastMsg := map[int]struct {
		content string
		at      time.Time
	}{}
	rows, err = r.db.Query(`
		SELECT sender_id, receiver_id, content, created_at
		FROM messages
		WHERE sender_id = ? OR receiver_id = ?
		ORDER BY created_at DESC, id DESC`, userID, userID)
	if err != nil {
		return nil, fmt.Errorf("repository: select contact last messages: %w", err)
	}
	for rows.Next() {
		var senderID, receiverID int
		var content string
		var at time.Time
		if err := rows.Scan(&senderID, &receiverID, &content, &at); err != nil {
			rows.Close()
			return nil, fmt.Errorf("repository: scan last message: %w", err)
		}
		otherID := senderID
		if senderID == userID {
			otherID = receiverID
		}
		if _, ok := lastMsg[otherID]; ok {
			continue
		}
		lastMsg[otherID] = struct {
			content string
			at      time.Time
		}{content, at}
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("repository: close last messages rows: %w", err)
	}

	contacts := make([]models.Contact, 0, len(userRows))
	for _, rw := range userRows {
		c := models.Contact{User: rw.u}
		if !rw.lastSeen.Valid || now.Sub(rw.lastSeen.Time) <= OnlineWindow {
			c.Online = true
		}
		if lm, ok := lastMsg[rw.u.ID]; ok {
			c.HasMsg = true
			c.LastMsg = lm.content
			c.Last = lm.at
		}
		contacts = append(contacts, c)
	}

	sort.SliceStable(contacts, func(i, j int) bool {
		if contacts[i].HasMsg != contacts[j].HasMsg {
			return contacts[i].HasMsg
		}
		if contacts[i].HasMsg {
			return contacts[i].Last.After(contacts[j].Last)
		}
		return strings.ToLower(contacts[i].User.Username) < strings.ToLower(contacts[j].User.Username)
	})
	return contacts, nil
}
