package models

import "time"

type User struct {
	ID           int       `json:"id"`
	Username     string    `json:"username"`
	PasswordHash string    `json:"-"`
	FullName     string    `json:"full_name"`
	Role         string    `json:"role"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type QRCode struct {
	ID        int       `json:"id"`
	UserID    int       `json:"user_id"`
	Token     string    `json:"token"`
	UserData  string    `json:"user_data"`
	IssuedAt  time.Time `json:"issued_at"`
	ExpiresAt time.Time `json:"expires_at"`
	IsUsed    bool      `json:"is_used"`
	CreatedAt time.Time `json:"created_at"`
}

type AccessLog struct {
	ID        int       `json:"id"`
	QrToken   string    `json:"qr_token"`
	UserID    *int      `json:"user_id"`
	Action    string    `json:"action"`
	Status    string    `json:"status"`
	Message   string    `json:"message"`
	ScannedAt time.Time `json:"scanned_at"`
}

// Login request/response
type LoginRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

type LoginResponse struct {
	Token string `json:"token"`
	User  User   `json:"user"`
}

// QR Code generation request/response
type QRCodeResponse struct {
	QRCode      string `json:"qr_code"`
	Token       string `json:"token"`
	UserData    string `json:"user_data"`
	ExpiresIn   int    `json:"expires_in"` // seconds
	GeneratedAt string `json:"generated_at"`
}

// QR Code validation request (from SKUD scanner)
type QRValidationRequest struct {
	Token string `json:"token" binding:"required"`
}

type QRValidationResponse struct {
	Valid    bool   `json:"valid"`
	UserID   int    `json:"user_id,omitempty"`
	UserName string `json:"user_name,omitempty"`
	Message  string `json:"message"`
}
