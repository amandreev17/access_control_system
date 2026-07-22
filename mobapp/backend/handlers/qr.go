package handlers

import (
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"time"

	"qr-code-backend/database"
	"qr-code-backend/keys"
	"qr-code-backend/models"

	"github.com/gin-gonic/gin"
)

type QRHandler struct{}

func NewQRHandler() *QRHandler {
	return &QRHandler{}
}

// GenerateQRCode generates a new QR code for the authenticated user
// QR Code format: {user_id}:{full_name}:{role}:{timestamp}:{expiry}:{signature}
// Signature is RSA-SHA256 of the data part, ensuring authenticity
func (h *QRHandler) GenerateQRCode(c *gin.Context) {
	userID := c.GetInt("user_id")

	var user models.User
	err := database.DB.QueryRow(
		"SELECT id, username, full_name, role FROM users WHERE id = ?",
		userID,
	).Scan(&user.ID, &user.Username, &user.FullName, &user.Role)

	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "User not found"})
		return
	}

	// Generate QR data with TOTP-style time-based component
	now := time.Now().UTC()
	// Round to nearest 10-second interval (TOTP style)
	interval := now.Truncate(10 * time.Second)
	expiresAt := interval.Add(10 * time.Second)

	// Create the data payload
	// Format: user_id:full_name:role:interval_timestamp:expiry_timestamp
	dataPayload := fmt.Sprintf("%d:%s:%s:%d:%d",
		user.ID,
		user.FullName,
		user.Role,
		interval.Unix(),
		expiresAt.Unix(),
	)

	// Sign the data with RSA private key
	signature, err := keys.SignData(dataPayload)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to sign QR data"})
		return
	}

	// Full QR token: data + signature
	token := fmt.Sprintf("%s|%s", dataPayload, signature)

	// 1. Удаляем старые истекшие QR-коды этого пользователя (до вставки)
	database.DB.Exec("DELETE FROM qr_codes WHERE user_id = ? AND expires_at < ?", user.ID, now)

	// 2. Удаляем дубликаты для этого же интервала (если уже есть QR на этот же dataPayload)
	database.DB.Exec("DELETE FROM qr_codes WHERE user_id = ? AND user_data = ?", user.ID, dataPayload)

	// 3. Вставляем новый QR-код
	_, err = database.DB.Exec(
		"INSERT INTO qr_codes (user_id, token, user_data, issued_at, expires_at) VALUES (?, ?, ?, ?, ?)",
		user.ID, token, dataPayload, now, expiresAt,
	)
	if err != nil {
		log.Printf("ERROR inserting QR code for user %d: %v", user.ID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to store QR code: " + err.Error()})
		return
	}

	response := models.QRCodeResponse{
		QRCode:      token,
		Token:       token,
		UserData:    dataPayload,
		ExpiresIn:   10,
		GeneratedAt: now.Format(time.RFC3339),
	}

	c.JSON(http.StatusOK, response)
}

// ValidateQRCode validates a QR code (called by SKUD scanner)
func (h *QRHandler) ValidateQRCode(c *gin.Context) {
	var req models.QRValidationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request: " + err.Error()})
		return
	}

	token := req.Token
	now := time.Now().UTC()

	// Parse token: data|signature
	var dataPayload, signature string
	_, err := fmt.Sscanf(token, "%s|%s", &dataPayload, &signature)
	if err != nil {
		// Manual split
		for i := len(token) - 1; i >= 0; i-- {
			if token[i] == '|' {
				dataPayload = token[:i]
				signature = token[i+1:]
				break
			}
		}
		if dataPayload == "" || signature == "" {
			logAccess("", 0, "validate", "denied", "Invalid QR format")
			c.JSON(http.StatusBadRequest, models.QRValidationResponse{
				Valid:   false,
				Message: "Invalid QR code format",
			})
			return
		}
	}

	// Verify RSA signature
	if !keys.VerifySignature(dataPayload, signature) {
		logAccess(token, 0, "validate", "denied", "Invalid signature - QR code may be tampered")
		c.JSON(http.StatusOK, models.QRValidationResponse{
			Valid:   false,
			Message: "Invalid QR code signature - access denied",
		})
		return
	}

	// Parse data payload: user_id:full_name:role:interval:expiry
	var userID int
	var fullName, role string
	var intervalUnix, expiryUnix int64
	_, err = fmt.Sscanf(dataPayload, "%d:%s:%s:%d:%d", &userID, &fullName, &role, &intervalUnix, &expiryUnix)
	if err != nil {
		// Try parsing with potential spaces in full_name
		parts := splitDataPayload(dataPayload)
		if len(parts) != 5 {
			logAccess(token, 0, "validate", "denied", "Failed to parse QR data")
			c.JSON(http.StatusBadRequest, models.QRValidationResponse{
				Valid:   false,
				Message: "Failed to parse QR code data",
			})
			return
		}
		userID = parseInt(parts[0])
		fullName = parts[1]
		role = parts[2]
		intervalUnix = parseInt64(parts[3])
		expiryUnix = parseInt64(parts[4])
	}

	// Check expiry
	expiryTime := time.Unix(expiryUnix, 0).UTC()
	if now.After(expiryTime) {
		logAccess(token, userID, "validate", "denied", "QR code expired")
		c.JSON(http.StatusOK, models.QRValidationResponse{
			Valid:   false,
			Message: "QR code has expired - access denied",
		})
		return
	}

	// Check if already used (within this interval)
	var existingUse int
	database.DB.QueryRow(
		"SELECT COUNT(*) FROM access_logs WHERE qr_token = ? AND status = 'granted' AND scanned_at > ?",
		token, time.Now().Add(-10*time.Second),
	).Scan(&existingUse)

	if existingUse > 0 {
		logAccess(token, userID, "validate", "denied", "QR code already used")
		c.JSON(http.StatusOK, models.QRValidationResponse{
			Valid:   false,
			Message: "QR code already used - access denied",
		})
		return
	}

	// Check if user exists and is active
	var dbUser models.User
	err = database.DB.QueryRow(
		"SELECT id, username, full_name, role FROM users WHERE id = ?",
		userID,
	).Scan(&dbUser.ID, &dbUser.Username, &dbUser.FullName, &dbUser.Role)

	if err == sql.ErrNoRows {
		logAccess(token, userID, "validate", "denied", "User not found")
		c.JSON(http.StatusOK, models.QRValidationResponse{
			Valid:   false,
			Message: "User not found - access denied",
		})
		return
	}

	// Mark QR as used
	database.DB.Exec("UPDATE qr_codes SET is_used = 1 WHERE token = ?", token)

	// Log successful access
	logAccess(token, userID, "validate", "granted", "Access granted")

	response := models.QRValidationResponse{
		Valid:    true,
		UserID:   dbUser.ID,
		UserName: dbUser.FullName,
		Message:  "Access granted",
	}

	c.JSON(http.StatusOK, response)
}

// GetPublicKey returns the public key (for scanner to verify)
func (h *QRHandler) GetPublicKey(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"public_key": keys.GetPublicKeyPEM(),
	})
}

// GetAccessLogs returns recent access logs
func (h *QRHandler) GetAccessLogs(c *gin.Context) {
	rows, err := database.DB.Query(`
		SELECT al.id, al.qr_token, al.user_id, al.action, al.status, al.message, al.scanned_at,
			   COALESCE(u.full_name, 'Unknown') as user_name
		FROM access_logs al
		LEFT JOIN users u ON al.user_id = u.id
		ORDER BY al.scanned_at DESC
		LIMIT 100
	`)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch logs"})
		return
	}
	defer rows.Close()

	type LogEntry struct {
		ID        int       `json:"id"`
		QrToken   string    `json:"qr_token"`
		UserID    *int      `json:"user_id"`
		UserName  string    `json:"user_name"`
		Action    string    `json:"action"`
		Status    string    `json:"status"`
		Message   string    `json:"message"`
		ScannedAt time.Time `json:"scanned_at"`
	}

	var logs []LogEntry
	for rows.Next() {
		var entry LogEntry
		rows.Scan(&entry.ID, &entry.QrToken, &entry.UserID, &entry.Action, &entry.Status, &entry.Message, &entry.ScannedAt, &entry.UserName)
		logs = append(logs, entry)
	}

	c.JSON(http.StatusOK, logs)
}

// Helper functions
func logAccess(qrToken string, userID int, action, status, message string) {
	var uid *int
	if userID > 0 {
		uid = &userID
	}
	database.DB.Exec(
		"INSERT INTO access_logs (qr_token, user_id, action, status, message) VALUES (?, ?, ?, ?, ?)",
		qrToken, uid, action, status, message,
	)
}

func splitDataPayload(data string) []string {
	var result []string
	current := ""
	for i := 0; i < len(data); i++ {
		if data[i] == ':' {
			result = append(result, current)
			current = ""
		} else {
			current += string(data[i])
		}
	}
	result = append(result, current)
	return result
}

func parseInt(s string) int {
	var n int
	fmt.Sscanf(s, "%d", &n)
	return n
}

func parseInt64(s string) int64 {
	var n int64
	fmt.Sscanf(s, "%d", &n)
	return n
}
