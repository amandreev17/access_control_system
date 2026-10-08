package handlers

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"qr-code-backend/database"
	"qr-code-backend/models"

	"github.com/gin-gonic/gin"
)

// =====================================================================
//
//	НАСТРОЙКИ API К OPENROUTER — здесь прописываем параметры LLM,
//	которая распознаёт снимок кодового слова.
//
//	1) ТОКЕН:  впишите ключ OpenRouter (https://openrouter.ai/keys)
//	           в openRouterAPIKeyDefault НИЖЕ, либо задайте переменную
//	           окружения OPENROUTER_API_KEY (окружение имеет приоритет).
//	2) МОДЕЛЬ: впишите идентификатор vision-модели в openRouterModelDefault
//	           НИЖЕ, либо задайте переменную окружения OPENROUTER_MODEL.
//	3) АДРЕС:  по умолчанию https://openrouter.ai/api/v1/chat/completions,
//	           переопределяется переменной OPENROUTER_API_URL
//	           (удобно для тестов на мок-сервере).
//
// =====================================================================
const (
	// >>> ТОКЕН OPENROUTER <<<
	openRouterAPIKeyDefault = "" // например: "sk-or-v1-xxxxxxxxxxxxxxxxxxxxxxxx"
	// >>> МОДЕЛЬ OPENROUTER (должна поддерживать изображения/vision) <<<
	openRouterModelDefault = "google/gemini-2.5-flash"
	// примеры других моделей: "openai/gpt-4o-mini", "anthropic/claude-3.5-sonnet",
	//                        "qwen/qwen2.5-vl-72b-instruct"
	// >>> АДРЕС API <<<
	openRouterURLDefault = "https://openrouter.ai/api/v1/chat/completions"
)

const (
	phraseLLMTimeout   = 60 * time.Second // таймаут запроса к LLM
	maxPhraseImageSize = 16 << 20         // максимальный размер снимка в base64 (16 МБ)
)

type PhraseHandler struct{}

func NewPhraseHandler() *PhraseHandler {
	return &PhraseHandler{}
}

// openRouterConfig собирает настройки OpenRouter:
// переменные окружения имеют приоритет над константами выше.
func openRouterConfig() (apiKey, model, apiURL string) {
	apiKey = strings.TrimSpace(os.Getenv("OPENROUTER_API_KEY"))
	if apiKey == "" {
		apiKey = strings.TrimSpace(openRouterAPIKeyDefault)
	}
	model = strings.TrimSpace(os.Getenv("OPENROUTER_MODEL"))
	if model == "" {
		model = openRouterModelDefault
	}
	apiURL = strings.TrimSpace(os.Getenv("OPENROUTER_API_URL"))
	if apiURL == "" {
		apiURL = openRouterURLDefault
	}
	return apiKey, model, apiURL
}

// ValidatePhrase — POST /api/phrase_validate
// Принимает от стороннего считывателя снимок кодового слова в base64
// ({"image": "/9j/4AAQ..."}), распознаёт его через LLM (OpenRouter),
// сравнивает с кодовыми словами из БД и при совпадении возвращает
// имя пользователя: {"valid":true,"user_name":"Иванов Иван","message":"Access granted"}
func (h *PhraseHandler) ValidatePhrase(c *gin.Context) {
	var req models.PhraseValidateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request: " + err.Error()})
		return
	}

	apiKey, model, apiURL := openRouterConfig()
	if apiKey == "" {
		log.Printf("phrase_validate: OPENROUTER_API_KEY is not set (see config block in handlers/phrase.go)")
		c.JSON(http.StatusOK, models.PhraseValidateResponse{
			Valid:   false,
			Message: "Recognition service is not configured",
		})
		return
	}

	imageBase64, mediaType, err := normalizeImage(req.Image)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid image: " + err.Error()})
		return
	}

	recognized, err := recognizePhrase(c.Request.Context(), apiKey, model, apiURL, imageBase64, mediaType)
	if err != nil {
		log.Printf("phrase_validate: recognition failed: %v", err)
		c.JSON(http.StatusOK, models.PhraseValidateResponse{
			Valid:   false,
			Message: "Recognition service error",
		})
		return
	}

	recognized = firstPhraseLine(recognized)
	if recognized == "" || strings.EqualFold(recognized, "НЕТ") {
		logAccess("phrase", 0, "phrase_validate", "denied", "No readable code word on the image")
		c.JSON(http.StatusOK, models.PhraseValidateResponse{
			Valid:   false,
			Message: "Access denied",
		})
		return
	}

	userID, fullName, found, err := findUserByCodeWord(recognized)
	if err != nil {
		log.Printf("phrase_validate: database error: %v", err)
		c.JSON(http.StatusOK, models.PhraseValidateResponse{
			Valid:   false,
			Message: "Database error",
		})
		return
	}

	if !found {
		logAccess("phrase:"+recognized, 0, "phrase_validate", "denied", "Code word does not match")
		c.JSON(http.StatusOK, models.PhraseValidateResponse{
			Valid:   false,
			Message: "Access denied",
		})
		return
	}

	logAccess("phrase:"+recognized, userID, "phrase_validate", "granted", "Access granted")
	c.JSON(http.StatusOK, models.PhraseValidateResponse{
		Valid:    true,
		UserName: fullName,
		Message:  "Access granted",
	})
}

// findUserByCodeWord ищет пользователя с кодовым словом, совпадающим
// с распознанным (без учёта регистра и лишних пробелов).
func findUserByCodeWord(recognized string) (int, string, bool, error) {
	rows, err := database.DB.Query(
		"SELECT id, full_name, code_word FROM users WHERE code_word IS NOT NULL AND code_word <> ''",
	)
	if err != nil {
		return 0, "", false, err
	}
	defer rows.Close()

	for rows.Next() {
		var id int
		var fullName, codeWord string
		if err := rows.Scan(&id, &fullName, &codeWord); err != nil {
			continue
		}
		if strings.EqualFold(normalizePhrase(codeWord), recognized) {
			return id, fullName, true, nil
		}
	}
	return 0, "", false, rows.Err()
}

// recognizePhrase отправляет снимок в LLM (OpenRouter, API-совместимый с
// OpenAI chat/completions) и возвращает распознанное кодовое слово.
func recognizePhrase(ctx context.Context, apiKey, model, apiURL, imageBase64, mediaType string) (string, error) {
	payload := map[string]any{
		"model": model,
		"messages": []map[string]any{
			{
				"role": "system",
				"content": "Ты — OCR-система распознавания кодовых слов со снимков. " +
					"Ответь ТОЛЬКО кодовым словом, которое изображено на снимке: " +
					"без кавычек, номеров, пояснений и лишнего текста. " +
					"Сохрани язык и регистр слова. " +
					"Если кодового слова нет или оно не читается, ответь ровно: НЕТ",
			},
			{
				"role": "user",
				"content": []map[string]any{
					{"type": "text", "text": "Прочитай кодовое слово на этом снимке."},
					{
						"type":      "image_url",
						"image_url": map[string]string{"url": "data:" + mediaType + ";base64," + imageBase64},
					},
				},
			},
		},
		"max_tokens":  100,
		"temperature": 0,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("X-Title", "dynamic-QR-code") // название приложения в OpenRouter

	client := &http.Client{Timeout: phraseLLMTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}

	var parsed struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return "", fmt.Errorf("invalid LLM response (HTTP %d)", resp.StatusCode)
	}

	if resp.StatusCode != http.StatusOK {
		msg := fmt.Sprintf("HTTP %d", resp.StatusCode)
		if parsed.Error != nil && parsed.Error.Message != "" {
			msg = parsed.Error.Message
		}
		return "", fmt.Errorf("LLM API error: %s", msg)
	}
	if len(parsed.Choices) == 0 {
		return "", fmt.Errorf("LLM returned no choices")
	}
	return parsed.Choices[0].Message.Content, nil
}

// normalizeImage приводит присланные данные к чистому base64 и определяет
// MIME-тип снимка по магическим байтам.
func normalizeImage(raw string) (string, string, error) {
	s := strings.TrimSpace(raw)
	if strings.HasPrefix(s, "data:") { // на случай data:image/jpeg;base64,/9j/...
		if i := strings.Index(s, ";base64,"); i >= 0 {
			s = s[i+len(";base64,"):]
		}
	}
	if s == "" {
		return "", "", fmt.Errorf("image is empty")
	}
	if len(s) > maxPhraseImageSize {
		return "", "", fmt.Errorf("image is too large")
	}

	// Восстанавливаем padding, если его нет (raw base64).
	if m := len(s) % 4; m != 0 {
		s += strings.Repeat("=", 4-m)
	}
	// Поддержка URL-safe варианта base64.
	if _, err := base64.StdEncoding.DecodeString(s); err != nil {
		alt := strings.NewReplacer("-", "+", "_", "/").Replace(s)
		if _, err2 := base64.StdEncoding.DecodeString(alt); err2 != nil {
			return "", "", fmt.Errorf("invalid base64")
		}
		s = alt
	}

	return s, detectMediaType(s), nil
}

// detectMediaType определяет MIME-тип по префиксу base64
// (магические байты JPEG/PNG/GIF/WebP стоят в начале файла).
func detectMediaType(b64 string) string {
	switch {
	case strings.HasPrefix(b64, "/9j/"):
		return "image/jpeg"
	case strings.HasPrefix(b64, "iVBOR"):
		return "image/png"
	case strings.HasPrefix(b64, "R0lGOD"):
		return "image/gif"
	case strings.HasPrefix(b64, "UklGR"):
		return "image/webp"
	default:
		return "image/jpeg"
	}
}

// firstPhraseLine оставляет первую непустую строку ответа LLM.
func firstPhraseLine(content string) string {
	for _, line := range strings.Split(content, "\n") {
		if s := normalizePhrase(line); s != "" {
			return s
		}
	}
	return ""
}

// normalizePhrase нормализует кодовое слово для сравнения:
// убирает кавычки/пунктуацию по краям и схлопывает пробелы.
func normalizePhrase(s string) string {
	s = strings.TrimSpace(s)
	s = strings.Trim(s, "\"'`«»“”‘’.,;:!?")
	s = strings.Join(strings.Fields(s), " ")
	return s
}
