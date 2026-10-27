package models

import "time"

// User — зарегистрированный пользователь форума.
type User struct {
	ID        int
	Username  string
	Email     string
	FirstName string
	LastName  string
	Age       int
	Gender    string
}

// Post — пост вместе с именем автора и счётчиками реакций.
type Post struct {
	ID         int
	Username   string
	Title      string
	Content    string
	CreatedAt  time.Time
	Categories []string
	Likes      int
	Dislikes   int
}

// Comment — комментарий вместе с именем автора и счётчиками реакций.
type Comment struct {
	ID        int
	Content   string
	Username  string
	CreatedAt time.Time
	Likes     int
	Dislikes  int
}

// Category — категория для постов.
type Category struct {
	ID   int
	Name string
}

// PostFilter описывает условия фильтрации списка постов.
// Нулевые значения полей означают "без фильтра".
type PostFilter struct {
	CategoryID int // фильтр по категории
	AuthorID   int // только посты конкретного автора
	LikedBy    int // только посты, которые лайкнул конкретный пользователь
}

// Message — личное сообщение между пользователями.
type Message struct {
	ID        int       `json:"id"`
	SenderID  int       `json:"sender_id"`
	Sender    string    `json:"sender"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"created_at"`
}

// Цели реакций — значения поля ReactionCounts.Target.
const (
	ReactionTargetPost    = "post"
	ReactionTargetComment = "comment"
)

// ReactionCounts — актуальные счётчики лайков/дизлайков поста или комментария.
// Рассылается по WebSocket после каждого изменения реакции.
type ReactionCounts struct {
	Target   string `json:"target"` // ReactionTargetPost или ReactionTargetComment
	ID       int    `json:"id"`
	Likes    int    `json:"likes"`
	Dislikes int    `json:"dislikes"`
}

// Conversation — диалог с другим пользователем и последнее сообщение в нём.
type Conversation struct {
	OtherUser User
	LastMsg   string
	LastTime  time.Time
}

// Contact — пользователь из списка контактов чата вместе с его статусом
// и последним сообщением в переписке с текущим пользователем.
type Contact struct {
	User    User
	Online  bool
	HasMsg  bool
	LastMsg string
	Last    time.Time
}

// TemplateData — данные, передаваемые в шаблоны.
type TemplateData struct {
	User           *User
	Contacts       []Contact
	Posts          []Post
	Categories     []Category
	Post           Post
	Comments       []Comment
	ActiveFilter   string
	ActiveCategory int
	ActiveWith     int
}
