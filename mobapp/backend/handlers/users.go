package handlers

import (
	"database/sql"
	"net/http"
	"strconv"
	"time"

	"qr-code-backend/database"
	"qr-code-backend/models"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
)

type UserHandler struct{}

func NewUserHandler() *UserHandler {
	return &UserHandler{}
}

// AdminOnly middleware — проверяет роль admin
func AdminOnly() gin.HandlerFunc {
	return func(c *gin.Context) {
		role := c.GetString("role")
		if role != "admin" {
			c.JSON(http.StatusForbidden, gin.H{"error": "Доступ запрещён. Только для администраторов."})
			c.Abort()
			return
		}
		c.Next()
	}
}

// CreateUserRequest — запрос на создание пользователя
type CreateUserRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
	FullName string `json:"full_name" binding:"required"`
	Role     string `json:"role"`
}

// UpdateUserRequest — запрос на обновление пользователя
type UpdateUserRequest struct {
	Password string `json:"password,omitempty"`
	FullName string `json:"full_name,omitempty"`
	Role     string `json:"role,omitempty"`
}

// ListUsers — возвращает список всех пользователей
func (h *UserHandler) ListUsers(c *gin.Context) {
	rows, err := database.DB.Query(
		"SELECT id, username, full_name, role, created_at, updated_at FROM users ORDER BY id",
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Ошибка получения пользователей"})
		return
	}
	defer rows.Close()

	type UserListItem struct {
		ID        int       `json:"id"`
		Username  string    `json:"username"`
		FullName  string    `json:"full_name"`
		Role      string    `json:"role"`
		CreatedAt time.Time `json:"created_at"`
		UpdatedAt time.Time `json:"updated_at"`
	}

	var users []UserListItem
	for rows.Next() {
		var u UserListItem
		if err := rows.Scan(&u.ID, &u.Username, &u.FullName, &u.Role, &u.CreatedAt, &u.UpdatedAt); err != nil {
			continue
		}
		users = append(users, u)
	}

	c.JSON(http.StatusOK, users)
}

// GetUser — возвращает одного пользователя по ID
func (h *UserHandler) GetUser(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Некорректный ID"})
		return
	}

	var user models.User
	err = database.DB.QueryRow(
		"SELECT id, username, password_hash, full_name, role, created_at, updated_at FROM users WHERE id = ?",
		id,
	).Scan(&user.ID, &user.Username, &user.PasswordHash, &user.FullName, &user.Role, &user.CreatedAt, &user.UpdatedAt)

	if err == sql.ErrNoRows {
		c.JSON(http.StatusNotFound, gin.H{"error": "Пользователь не найден"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Ошибка базы данных"})
		return
	}

	c.JSON(http.StatusOK, user)
}

// CreateUser — создаёт нового пользователя
func (h *UserHandler) CreateUser(c *gin.Context) {
	var req CreateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Заполните все обязательные поля"})
		return
	}

	// Роль по умолчанию
	if req.Role == "" {
		req.Role = "user"
	}
	if req.Role != "user" && req.Role != "admin" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Роль должна быть 'user' или 'admin'"})
		return
	}

	// Проверка длины пароля
	if len(req.Password) < 4 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Пароль должен быть минимум 4 символа"})
		return
	}

	// Хэшируем пароль
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Ошибка хэширования пароля"})
		return
	}

	_, err = database.DB.Exec(
		"INSERT INTO users (username, password_hash, full_name, role) VALUES (?, ?, ?, ?)",
		req.Username, string(hash), req.FullName, req.Role,
	)
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "Пользователь с таким логином уже существует"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"message": "Пользователь создан"})
}

// UpdateUser — обновляет данные пользователя
func (h *UserHandler) UpdateUser(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Некорректный ID"})
		return
	}

	var req UpdateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Некорректные данные"})
		return
	}

	// Проверяем, что пользователь существует
	var exists bool
	database.DB.QueryRow("SELECT EXISTS(SELECT 1 FROM users WHERE id = ?)", id).Scan(&exists)
	if !exists {
		c.JSON(http.StatusNotFound, gin.H{"error": "Пользователь не найден"})
		return
	}

	// Обновляем поля
	if req.FullName != "" {
		database.DB.Exec("UPDATE users SET full_name = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?", req.FullName, id)
	}
	if req.Role != "" {
		if req.Role != "user" && req.Role != "admin" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Роль должна быть 'user' или 'admin'"})
			return
		}
		database.DB.Exec("UPDATE users SET role = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?", req.Role, id)
	}
	if req.Password != "" {
		if len(req.Password) < 4 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Пароль должен быть минимум 4 символа"})
			return
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Ошибка хэширования пароля"})
			return
		}
		database.DB.Exec("UPDATE users SET password_hash = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?", string(hash), id)
	}

	c.JSON(http.StatusOK, gin.H{"message": "Пользователь обновлён"})
}

// DeleteUser — удаляет пользователя
func (h *UserHandler) DeleteUser(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Некорректный ID"})
		return
	}

	// Нельзя удалить самого себя
	currentUserID := c.GetInt("user_id")
	if id == currentUserID {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Нельзя удалить самого себя"})
		return
	}

	result, err := database.DB.Exec("DELETE FROM users WHERE id = ?", id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Ошибка удаления"})
		return
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "Пользователь не найден"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Пользователь удалён"})
}
