package repository

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"forum-authentication/internal/models"
)

// CreateComment сохраняет комментарий к посту.
func (r *Repository) CreateComment(postID, userID int, content string) error {
	_, err := r.db.Exec(
		"INSERT INTO comments (post_id, user_id, content, created_at) VALUES (?, ?, ?, ?)",
		postID, userID, content, time.Now(),
	)
	if err != nil {
		return fmt.Errorf("repository: insert comment: %w", err)
	}
	return nil
}

// CommentsByPost возвращает комментарии к посту (новые сверху) со счётчиками реакций.
func (r *Repository) CommentsByPost(postID int) ([]models.Comment, error) {
	rows, err := r.db.Query(`
		SELECT c.id, c.content, u.username, c.created_at,
			(SELECT COUNT(*) FROM reactions WHERE comment_id = c.id AND vote = 1) AS likes,
			(SELECT COUNT(*) FROM reactions WHERE comment_id = c.id AND vote = -1) AS dislikes
		FROM comments c
		JOIN users u ON u.id = c.user_id
		WHERE c.post_id = ?
		ORDER BY c.created_at DESC`, postID,
	)
	if err != nil {
		return nil, fmt.Errorf("repository: select comments: %w", err)
	}
	defer rows.Close()

	var comments []models.Comment
	for rows.Next() {
		var c models.Comment
		if err := rows.Scan(&c.ID, &c.Content, &c.Username, &c.CreatedAt, &c.Likes, &c.Dislikes); err != nil {
			return nil, fmt.Errorf("repository: scan comment: %w", err)
		}
		comments = append(comments, c)
	}
	return comments, rows.Err()
}

// SetCommentReaction устанавливает реакцию пользователя на комментарий —
// повторный клик той же реакцией отменяет её, другой — заменяет.
// Возвращает актуальные счётчики реакций комментария, посчитанные в той же транзакции.
func (r *Repository) SetCommentReaction(userID, commentID, vote int) (models.ReactionCounts, error) {
	counts := models.ReactionCounts{Target: models.ReactionTargetComment, ID: commentID}

	tx, err := r.db.Begin()
	if err != nil {
		return counts, fmt.Errorf("repository: begin tx: %w", err)
	}
	defer tx.Rollback()

	var existing int
	err = tx.QueryRow(
		"SELECT vote FROM reactions WHERE user_id = ? AND comment_id = ?", userID, commentID,
	).Scan(&existing)
	found := err == nil
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return counts, fmt.Errorf("repository: select comment reaction: %w", err)
	}

	if _, err := tx.Exec(
		"DELETE FROM reactions WHERE user_id = ? AND comment_id = ?", userID, commentID,
	); err != nil {
		return counts, fmt.Errorf("repository: delete comment reaction: %w", err)
	}

	if !found || existing != vote {
		if _, err := tx.Exec(
			"INSERT INTO reactions (user_id, comment_id, vote) VALUES (?, ?, ?)",
			userID, commentID, vote,
		); err != nil {
			return counts, fmt.Errorf("repository: insert comment reaction: %w", err)
		}
	}

	if err := tx.QueryRow(
		"SELECT "+reactionCountsExpr+" FROM reactions WHERE comment_id = ?", commentID,
	).Scan(&counts.Likes, &counts.Dislikes); err != nil {
		return counts, fmt.Errorf("repository: count comment reactions: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return counts, fmt.Errorf("repository: commit comment reaction: %w", err)
	}
	return counts, nil
}
