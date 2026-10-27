package repository

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"forum-authentication/internal/models"
)

// Categories возвращает список всех категорий, отсортированный по имени.
func (r *Repository) Categories() ([]models.Category, error) {
	rows, err := r.db.Query("SELECT id, name FROM categories ORDER BY name")
	if err != nil {
		return nil, fmt.Errorf("repository: select categories: %w", err)
	}
	defer rows.Close()

	var categories []models.Category
	for rows.Next() {
		var c models.Category
		if err := rows.Scan(&c.ID, &c.Name); err != nil {
			return nil, fmt.Errorf("repository: scan category: %w", err)
		}
		categories = append(categories, c)
	}
	return categories, rows.Err()
}

// CreatePost сохраняет пост вместе с выбранными категориями в одной транзакции.
func (r *Repository) CreatePost(userID int, title, content string, categoryIDs []int) (int, error) {
	tx, err := r.db.Begin()
	if err != nil {
		return 0, fmt.Errorf("repository: begin tx: %w", err)
	}
	defer tx.Rollback()

	res, err := tx.Exec(
		"INSERT INTO posts (user_id, title, content, created_at) VALUES (?, ?, ?, ?)",
		userID, title, content, time.Now(),
	)
	if err != nil {
		return 0, fmt.Errorf("repository: insert post: %w", err)
	}

	postID, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("repository: last insert id: %w", err)
	}

	for _, catID := range categoryIDs {
		if _, err := tx.Exec(
			"INSERT INTO post_categories (post_id, category_id) VALUES (?, ?)",
			postID, catID,
		); err != nil {
			return 0, fmt.Errorf("repository: link post category: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("repository: commit post: %w", err)
	}
	return int(postID), nil
}

// Posts возвращает посты согласно фильтру, отсортированные по дате создания (новые сверху).
func (r *Repository) Posts(filter models.PostFilter) ([]models.Post, error) {
	query := `
		SELECT p.id, u.username, p.title, p.content, p.created_at,
			GROUP_CONCAT(c.name, ',') AS categories,
			(SELECT COUNT(*) FROM reactions WHERE post_id = p.id AND vote = 1) AS likes,
			(SELECT COUNT(*) FROM reactions WHERE post_id = p.id AND vote = -1) AS dislikes
		FROM posts p
		JOIN users u ON u.id = p.user_id
		LEFT JOIN post_categories pc ON pc.post_id = p.id
		LEFT JOIN categories c ON c.id = pc.category_id
		WHERE 1 = 1`

	var args []interface{}
	if filter.CategoryID != 0 {
		query += " AND p.id IN (SELECT post_id FROM post_categories WHERE category_id = ?)"
		args = append(args, filter.CategoryID)
	}
	if filter.AuthorID != 0 {
		query += " AND p.user_id = ?"
		args = append(args, filter.AuthorID)
	}
	if filter.LikedBy != 0 {
		query += " AND p.id IN (SELECT post_id FROM reactions WHERE user_id = ? AND vote = 1)"
		args = append(args, filter.LikedBy)
	}

	query += " GROUP BY p.id ORDER BY p.created_at DESC"

	rows, err := r.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("repository: select posts: %w", err)
	}
	defer rows.Close()

	var posts []models.Post
	for rows.Next() {
		var p models.Post
		var catsRaw sql.NullString
		if err := rows.Scan(
			&p.ID, &p.Username, &p.Title, &p.Content, &p.CreatedAt,
			&catsRaw, &p.Likes, &p.Dislikes,
		); err != nil {
			return nil, fmt.Errorf("repository: scan post: %w", err)
		}
		if catsRaw.Valid && catsRaw.String != "" {
			p.Categories = strings.Split(catsRaw.String, ",")
		}
		posts = append(posts, p)
	}
	return posts, rows.Err()
}

// PostByID возвращает пост по его id со счётчиками реакций.
func (r *Repository) PostByID(id int) (*models.Post, error) {
	var p models.Post
	err := r.db.QueryRow(`
		SELECT p.id, u.username, p.title, p.content, p.created_at,
			(SELECT COUNT(*) FROM reactions WHERE post_id = p.id AND vote = 1) AS likes,
			(SELECT COUNT(*) FROM reactions WHERE post_id = p.id AND vote = -1) AS dislikes
		FROM posts p
		JOIN users u ON u.id = p.user_id
		WHERE p.id = ?`, id,
	).Scan(&p.ID, &p.Username, &p.Title, &p.Content, &p.CreatedAt, &p.Likes, &p.Dislikes)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("repository: select post by id: %w", err)
	}
	return &p, nil
}

// SetPostReaction устанавливает реакцию пользователя на пост.
// Повторный клик той же реакцией отменяет её, другой — заменяет.
// Возвращает актуальные счётчики реакций поста, посчитанные в той же транзакции.
func (r *Repository) SetPostReaction(userID, postID, vote int) (models.ReactionCounts, error) {
	counts := models.ReactionCounts{Target: models.ReactionTargetPost, ID: postID}

	tx, err := r.db.Begin()
	if err != nil {
		return counts, fmt.Errorf("repository: begin tx: %w", err)
	}
	defer tx.Rollback()

	var existing int
	err = tx.QueryRow(
		"SELECT vote FROM reactions WHERE user_id = ? AND post_id = ?", userID, postID,
	).Scan(&existing)
	found := err == nil
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return counts, fmt.Errorf("repository: select post reaction: %w", err)
	}

	if _, err := tx.Exec(
		"DELETE FROM reactions WHERE user_id = ? AND post_id = ?", userID, postID,
	); err != nil {
		return counts, fmt.Errorf("repository: delete post reaction: %w", err)
	}

	if !found || existing != vote {
		if _, err := tx.Exec(
			"INSERT INTO reactions (user_id, post_id, vote) VALUES (?, ?, ?)",
			userID, postID, vote,
		); err != nil {
			return counts, fmt.Errorf("repository: insert post reaction: %w", err)
		}
	}

	if err := tx.QueryRow(
		"SELECT "+reactionCountsExpr+" FROM reactions WHERE post_id = ?", postID,
	).Scan(&counts.Likes, &counts.Dislikes); err != nil {
		return counts, fmt.Errorf("repository: count post reactions: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return counts, fmt.Errorf("repository: commit post reaction: %w", err)
	}
	return counts, nil
}

// reactionCountsExpr — выражение SELECT, считающее лайки и дизлайки
// по строкам таблицы reactions (используется с условием WHERE по цели).
const reactionCountsExpr = `
	COALESCE(SUM(CASE WHEN vote = 1 THEN 1 ELSE 0 END), 0),
	COALESCE(SUM(CASE WHEN vote = -1 THEN 1 ELSE 0 END), 0)`
