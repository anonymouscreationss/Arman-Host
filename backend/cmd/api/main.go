package main

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	_ "github.com/lib/pq"
)

type api struct {
	db     *sql.DB
	logger *slog.Logger
	mailer mailer
}

type envelope struct {
	Success bool        `json:"success"`
	Data    interface{} `json:"data"`
	Message interface{} `json:"message"`
	Error   interface{} `json:"error"`
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	var db *sql.DB
	if dsn := os.Getenv("DATABASE_URL"); dsn != "" {
		var err error
		db, err = sql.Open("postgres", dsn)
		if err != nil {
			logger.Error("database_open_failed", "error", err)
			os.Exit(1)
		}
		defer db.Close()
	}

	app := &api{db: db, logger: logger, mailer: newConfiguredMailer()}
	server := &http.Server{
		Addr:              ":" + port,
		Handler:           app.routes(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	logger.Info("api_started", "port", port, "database_configured", db != nil)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Error("api_stopped", "error", err)
		os.Exit(1)
	}
}

func (a *api) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/health", a.health)
	mux.HandleFunc("GET /api/v1/ready", a.ready)
	mux.HandleFunc("GET /api/v1/config", a.config)
	mux.HandleFunc("GET /api/v1/resources", a.resources)
	mux.HandleFunc("POST /api/v1/auth/register", a.register)
	mux.HandleFunc("POST /api/v1/auth/login", a.login)
	mux.HandleFunc("POST /api/v1/auth/refresh", a.refresh)
	mux.HandleFunc("POST /api/v1/auth/verify-email", a.verifyEmail)
	mux.HandleFunc("POST /api/v1/auth/forgot-password", a.forgotPassword)
	mux.HandleFunc("POST /api/v1/auth/reset-password", a.resetPassword)
	mux.Handle("GET /api/v1/auth/me", a.requireAuth(http.HandlerFunc(a.me)))
	mux.Handle("POST /api/v1/auth/resend-verification", a.requireAuth(http.HandlerFunc(a.resendVerification)))
	mux.Handle("POST /api/v1/auth/logout", a.requireAuth(http.HandlerFunc(a.logout)))
	mux.Handle("GET /api/v1/profiles/me", a.requireAuth(http.HandlerFunc(a.profile)))
	mux.Handle("PATCH /api/v1/profiles/me", a.requireAuth(http.HandlerFunc(a.updateProfile)))
	mux.Handle("GET /api/v1/bookmarks", a.requireAuth(http.HandlerFunc(a.bookmarks)))
	mux.Handle("GET /api/v1/resources/mine", a.requireAuth(http.HandlerFunc(a.myResources)))
	mux.Handle("POST /api/v1/resources", a.requireAuth(http.HandlerFunc(a.createResource)))
	mux.Handle("PATCH /api/v1/resources/{id}", a.requireAuth(http.HandlerFunc(a.updateResource)))
	mux.Handle("DELETE /api/v1/resources/{id}", a.requireAuth(http.HandlerFunc(a.deleteResource)))
	mux.Handle("POST /api/v1/resources/upload", a.requireAuth(http.HandlerFunc(a.uploadResourceFile)))
	mux.HandleFunc("GET /api/v1/files/{key}", a.downloadResourceFile)
	mux.HandleFunc("GET /api/v1/resources/{id}", a.resourceDetail)
	mux.Handle("POST /api/v1/resources/{id}/bookmark", a.requireAuth(http.HandlerFunc(a.addBookmark)))
	mux.Handle("DELETE /api/v1/resources/{id}/bookmark", a.requireAuth(http.HandlerFunc(a.removeBookmark)))
	mux.Handle("/", http.FileServer(http.Dir("../web/dist")))

	return withRequestID(withCORS(mux))
}

type registerRequest struct {
	Name     string `json:"name"`
	Username string `json:"username"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type tokenClaims struct {
	UserID string
	Type   string
	Expiry time.Time
}

type authContextKey struct{}

func (a *api) register(w http.ResponseWriter, r *http.Request) {
	if a.db == nil {
		writeJSON(w, http.StatusServiceUnavailable, envelope{
			Success: false, Data: nil,
			Message: "Authentication requires a configured database.",
			Error:   map[string]string{"code": "DATABASE_NOT_CONFIGURED"},
		})
		return
	}
	if len(a.signingSecret("access")) == 0 || len(a.signingSecret("refresh")) == 0 {
		writeJSON(w, http.StatusServiceUnavailable, envelope{
			Success: false, Data: nil,
			Message: "Authentication secrets are not configured.",
			Error:   map[string]string{"code": "AUTH_SECRETS_NOT_CONFIGURED"},
		})
		return
	}
	if a.mailer == nil {
		writeEmailDeliveryError(w, errEmailDeliveryUnavailable)
		return
	}
	var input registerRequest
	if err := decodeJSON(r, &input); err != nil {
		writeJSON(w, http.StatusBadRequest, envelope{Success: false, Data: nil, Message: "Please provide valid registration details.", Error: map[string]string{"code": "INVALID_REQUEST"}})
		return
	}
	input.Name = strings.TrimSpace(input.Name)
	input.Username = strings.TrimSpace(input.Username)
	input.Email = strings.ToLower(strings.TrimSpace(input.Email))
	if len(input.Name) < 2 || len(input.Username) < 3 || len(input.Email) < 5 || len(input.Password) < 10 {
		writeJSON(w, http.StatusBadRequest, envelope{Success: false, Data: nil, Message: "Name, username, email, and a password of at least 10 characters are required.", Error: map[string]string{"code": "INVALID_REGISTRATION"}})
		return
	}
	passwordHash, err := hashPassword(input.Password)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, envelope{Success: false, Data: nil, Message: "Registration could not be completed.", Error: map[string]string{"code": "PASSWORD_HASH_FAILED"}})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, envelope{Success: false, Data: nil, Message: "Registration could not be completed.", Error: map[string]string{"code": "TRANSACTION_FAILED"}})
		return
	}
	defer tx.Rollback()

	var userID string
	err = tx.QueryRowContext(ctx, `
		INSERT INTO users (name, username, email, password_hash)
		VALUES ($1, $2, $3, $4)
		RETURNING id`, input.Name, input.Username, input.Email, string(passwordHash)).Scan(&userID)
	if err != nil {
		status := http.StatusInternalServerError
		code := "USER_CREATE_FAILED"
		message := "Registration could not be completed."
		if strings.Contains(err.Error(), "duplicate key") {
			status = http.StatusConflict
			code = "IDENTITY_ALREADY_EXISTS"
			message = "That email or username is already in use."
		}
		writeJSON(w, status, envelope{Success: false, Data: nil, Message: message, Error: map[string]string{"code": code}})
		return
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO profiles (user_id) VALUES ($1)`, userID); err != nil {
		writeJSON(w, http.StatusInternalServerError, envelope{Success: false, Data: nil, Message: "Profile setup could not be completed.", Error: map[string]string{"code": "PROFILE_CREATE_FAILED"}})
		return
	}
	if _, err = tx.ExecContext(ctx, `
		INSERT INTO user_roles (user_id, role_id)
		SELECT $1, id FROM roles WHERE name = 'student'`, userID); err != nil {
		writeJSON(w, http.StatusInternalServerError, envelope{Success: false, Data: nil, Message: "Account role setup could not be completed.", Error: map[string]string{"code": "ROLE_CREATE_FAILED"}})
		return
	}
	if err = tx.Commit(); err != nil {
		writeJSON(w, http.StatusInternalServerError, envelope{Success: false, Data: nil, Message: "Registration could not be completed.", Error: map[string]string{"code": "TRANSACTION_COMMIT_FAILED"}})
		return
	}
	user := map[string]interface{}{"id": userID, "name": input.Name, "username": input.Username, "email": input.Email}
	if err := a.sendVerificationEmail(ctx, userID, input.Email); err != nil {
		writeEmailDeliveryError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, envelope{Success: true, Data: map[string]interface{}{
		"user": user, "verificationRequired": true,
	}, Message: "Account created. Check your email to verify it.", Error: nil})
}

func (a *api) login(w http.ResponseWriter, r *http.Request) {
	if a.db == nil {
		writeJSON(w, http.StatusServiceUnavailable, envelope{Success: false, Data: nil, Message: "Authentication requires a configured database.", Error: map[string]string{"code": "DATABASE_NOT_CONFIGURED"}})
		return
	}
	var input loginRequest
	if err := decodeJSON(r, &input); err != nil {
		writeJSON(w, http.StatusBadRequest, envelope{Success: false, Data: nil, Message: "Please provide valid sign-in details.", Error: map[string]string{"code": "INVALID_REQUEST"}})
		return
	}
	input.Email = strings.ToLower(strings.TrimSpace(input.Email))
	var userID, name, username, email, passwordHash, status string
	var emailVerified sql.NullTime
	err := a.db.QueryRowContext(r.Context(), `
		SELECT id, name, username, email, password_hash, status, email_verified_at
		FROM users WHERE email = $1 AND deleted_at IS NULL`, input.Email).
		Scan(&userID, &name, &username, &email, &passwordHash, &status, &emailVerified)
	if err != nil || status != "active" || !verifyPassword(passwordHash, input.Password) {
		writeJSON(w, http.StatusUnauthorized, envelope{Success: false, Data: nil, Message: "Email or password is incorrect.", Error: map[string]string{"code": "INVALID_CREDENTIALS"}})
		return
	}
	if !emailVerified.Valid {
		writeJSON(w, http.StatusForbidden, envelope{Success: false, Data: nil, Message: "Verify your email before signing in.", Error: map[string]string{"code": "EMAIL_NOT_VERIFIED"}})
		return
	}
	_, _ = a.db.ExecContext(r.Context(), `UPDATE users SET last_login_at = now(), updated_at = now() WHERE id = $1`, userID)
	user := map[string]interface{}{"id": userID, "name": name, "username": username, "email": email}
	auth, err := a.issueSession(r.Context(), userID, user)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, envelope{Success: false, Data: nil, Message: "Sign-in could not be completed.", Error: map[string]string{"code": "SESSION_CREATE_FAILED"}})
		return
	}
	writeJSON(w, http.StatusOK, envelope{Success: true, Data: auth, Message: nil, Error: nil})
}

func (a *api) refresh(w http.ResponseWriter, r *http.Request) {
	if a.db == nil {
		writeJSON(w, http.StatusServiceUnavailable, envelope{Success: false, Data: nil, Message: "Authentication requires a configured database.", Error: map[string]string{"code": "DATABASE_NOT_CONFIGURED"}})
		return
	}
	var input struct {
		RefreshToken string `json:"refreshToken"`
	}
	if err := decodeJSON(r, &input); err != nil || input.RefreshToken == "" {
		writeJSON(w, http.StatusBadRequest, envelope{Success: false, Data: nil, Message: "A refresh token is required.", Error: map[string]string{"code": "INVALID_REQUEST"}})
		return
	}
	var userID string
	err := a.db.QueryRowContext(r.Context(), `
		SELECT user_id FROM refresh_tokens
		WHERE token_hash = $1 AND revoked_at IS NULL AND expires_at > now()`,
		hashToken(input.RefreshToken)).Scan(&userID)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, envelope{Success: false, Data: nil, Message: "Your session has expired. Please sign in again.", Error: map[string]string{"code": "INVALID_REFRESH_TOKEN"}})
		return
	}
	var name, username, email string
	if err := a.db.QueryRowContext(r.Context(), `SELECT name, username, email FROM users WHERE id = $1 AND status = 'active' AND deleted_at IS NULL`, userID).Scan(&name, &username, &email); err != nil {
		writeJSON(w, http.StatusUnauthorized, envelope{Success: false, Data: nil, Message: "Your account is no longer available.", Error: map[string]string{"code": "USER_NOT_FOUND"}})
		return
	}
	access, err := a.signToken(userID, "access", 15*time.Minute)
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, envelope{Success: false, Data: nil, Message: "Authentication secrets are not configured.", Error: map[string]string{"code": "AUTH_SECRETS_NOT_CONFIGURED"}})
		return
	}
	writeJSON(w, http.StatusOK, envelope{Success: true, Data: map[string]interface{}{
		"user":        map[string]interface{}{"id": userID, "name": name, "username": username, "email": email},
		"accessToken": access, "refreshToken": input.RefreshToken,
	}, Message: nil, Error: nil})
}

func (a *api) me(w http.ResponseWriter, r *http.Request) {
	userID, _ := r.Context().Value(authContextKey{}).(string)
	var name, username, email string
	err := a.db.QueryRowContext(r.Context(), `
		SELECT name, username, email FROM users
		WHERE id = $1 AND status = 'active' AND deleted_at IS NULL`, userID).
		Scan(&name, &username, &email)
	if err != nil {
		writeJSON(w, http.StatusNotFound, envelope{Success: false, Data: nil, Message: "User account was not found.", Error: map[string]string{"code": "USER_NOT_FOUND"}})
		return
	}
	writeJSON(w, http.StatusOK, envelope{Success: true, Data: map[string]interface{}{"id": userID, "name": name, "username": username, "email": email}, Message: nil, Error: nil})
}

func (a *api) profile(w http.ResponseWriter, r *http.Request) {
	userID, _ := r.Context().Value(authContextKey{}).(string)
	var name, username, email, bio, education, visibility string
	var showStatistics, showAchievements, allowMessages bool
	err := a.db.QueryRowContext(r.Context(), `
		SELECT u.name, u.username, u.email, p.bio, p.education, p.visibility,
		       p.show_statistics, p.show_achievements, p.allow_messages
		FROM users u JOIN profiles p ON p.user_id = u.id
		WHERE u.id = $1 AND u.status = 'active' AND u.deleted_at IS NULL`, userID).
		Scan(&name, &username, &email, &bio, &education, &visibility,
			&showStatistics, &showAchievements, &allowMessages)
	if err != nil {
		writeJSON(w, http.StatusNotFound, envelope{Success: false, Data: nil, Message: "Profile was not found.", Error: map[string]string{"code": "PROFILE_NOT_FOUND"}})
		return
	}
	writeJSON(w, http.StatusOK, envelope{Success: true, Data: map[string]interface{}{
		"user": map[string]interface{}{"id": userID, "name": name, "username": username, "email": email},
		"bio":  bio, "education": education, "visibility": visibility,
		"showStatistics": showStatistics, "showAchievements": showAchievements,
		"allowMessages": allowMessages, "studyInterests": []string{},
	}, Message: nil, Error: nil})
}

func (a *api) updateProfile(w http.ResponseWriter, r *http.Request) {
	userID, _ := r.Context().Value(authContextKey{}).(string)
	var input struct {
		Visibility       *string `json:"visibility"`
		ShowStatistics   *bool   `json:"showStatistics"`
		ShowAchievements *bool   `json:"showAchievements"`
		AllowMessages    *bool   `json:"allowMessages"`
	}
	if err := decodeJSON(r, &input); err != nil {
		writeJSON(w, http.StatusBadRequest, envelope{Success: false, Data: nil, Message: "Please provide valid privacy settings.", Error: map[string]string{"code": "INVALID_REQUEST"}})
		return
	}
	if input.Visibility != nil && *input.Visibility != "public" && *input.Visibility != "private" {
		writeJSON(w, http.StatusBadRequest, envelope{Success: false, Data: nil, Message: "Visibility must be public or private.", Error: map[string]string{"code": "INVALID_VISIBILITY"}})
		return
	}
	_, err := a.db.ExecContext(r.Context(), `
		UPDATE profiles SET
			visibility = COALESCE($1, visibility),
			show_statistics = COALESCE($2, show_statistics),
			show_achievements = COALESCE($3, show_achievements),
			allow_messages = COALESCE($4, allow_messages),
			updated_at = now()
		WHERE user_id = $5`,
		nullableString(input.Visibility), nullableBool(input.ShowStatistics),
		nullableBool(input.ShowAchievements), nullableBool(input.AllowMessages), userID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, envelope{Success: false, Data: nil, Message: "Privacy settings could not be saved.", Error: map[string]string{"code": "PROFILE_UPDATE_FAILED"}})
		return
	}
	a.profile(w, r)
}

func (a *api) resourceDetail(w http.ResponseWriter, r *http.Request) {
	if a.db == nil {
		writeJSON(w, http.StatusServiceUnavailable, envelope{Success: false, Data: nil, Message: "Resources require a configured database.", Error: map[string]string{"code": "DATABASE_NOT_CONFIGURED"}})
		return
	}
	id := r.PathValue("id")
	var title, description, resourceType, subject, moderation string
	var fileURL, thumbnailURL sql.NullString
	err := a.db.QueryRowContext(r.Context(), `
		SELECT title, COALESCE(description, ''), resource_type, COALESCE(subject, ''),
		       moderation_status, file_url, thumbnail_url
		FROM resources
		WHERE id = $1 AND deleted_at IS NULL AND moderation_status = 'approved'`, id).
		Scan(&title, &description, &resourceType, &subject, &moderation, &fileURL, &thumbnailURL)
	if err != nil {
		writeJSON(w, http.StatusNotFound, envelope{Success: false, Data: nil, Message: "That resource could not be found.", Error: map[string]string{"code": "RESOURCE_NOT_FOUND"}})
		return
	}
	item := map[string]interface{}{
		"id": id, "title": title, "description": description, "type": resourceType,
		"subject": subject, "moderationStatus": moderation,
		"fileUrl": nullableValue(fileURL), "thumbnailUrl": nullableValue(thumbnailURL),
	}
	writeJSON(w, http.StatusOK, envelope{Success: true, Data: item, Message: nil, Error: nil})
}

func (a *api) bookmarks(w http.ResponseWriter, r *http.Request) {
	userID, _ := r.Context().Value(authContextKey{}).(string)
	rows, err := a.db.QueryContext(r.Context(), `
		SELECT r.id, r.title, r.resource_type, COALESCE(r.subject, '')
		FROM bookmarks b JOIN resources r ON r.id = b.resource_id
		WHERE b.user_id = $1 AND r.deleted_at IS NULL
		ORDER BY b.created_at DESC`, userID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, envelope{Success: false, Data: nil, Message: "Saved resources could not be loaded.", Error: map[string]string{"code": "BOOKMARK_QUERY_FAILED"}})
		return
	}
	defer rows.Close()
	items := make([]map[string]interface{}, 0)
	for rows.Next() {
		var id, title, resourceType, subject string
		if err := rows.Scan(&id, &title, &resourceType, &subject); err == nil {
			items = append(items, map[string]interface{}{"id": id, "title": title, "type": resourceType, "subject": subject})
		}
	}
	writeJSON(w, http.StatusOK, envelope{Success: true, Data: items, Message: nil, Error: nil})
}

func (a *api) addBookmark(w http.ResponseWriter, r *http.Request) {
	userID, _ := r.Context().Value(authContextKey{}).(string)
	id := r.PathValue("id")
	result, err := a.db.ExecContext(r.Context(), `
		INSERT INTO bookmarks (user_id, resource_id)
		SELECT $1, id FROM resources
		WHERE id = $2 AND deleted_at IS NULL AND moderation_status = 'approved'
		ON CONFLICT (user_id, resource_id) DO NOTHING`, userID, id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, envelope{Success: false, Data: nil, Message: "Resource could not be saved.", Error: map[string]string{"code": "BOOKMARK_CREATE_FAILED"}})
		return
	}
	if count, _ := result.RowsAffected(); count == 0 {
		writeJSON(w, http.StatusNotFound, envelope{Success: false, Data: nil, Message: "That resource could not be found.", Error: map[string]string{"code": "RESOURCE_NOT_FOUND"}})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *api) removeBookmark(w http.ResponseWriter, r *http.Request) {
	userID, _ := r.Context().Value(authContextKey{}).(string)
	_, err := a.db.ExecContext(r.Context(), `DELETE FROM bookmarks WHERE user_id = $1 AND resource_id = $2`, userID, r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, envelope{Success: false, Data: nil, Message: "Resource could not be unsaved.", Error: map[string]string{"code": "BOOKMARK_DELETE_FAILED"}})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *api) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := r.Header.Get("Authorization")
		if !strings.HasPrefix(header, "Bearer ") {
			writeJSON(w, http.StatusUnauthorized, envelope{Success: false, Data: nil, Message: "Authentication is required.", Error: map[string]string{"code": "AUTHENTICATION_REQUIRED"}})
			return
		}
		claims, err := a.parseToken(strings.TrimSpace(strings.TrimPrefix(header, "Bearer ")), "access")
		if err != nil {
			writeJSON(w, http.StatusUnauthorized, envelope{Success: false, Data: nil, Message: "Your session is invalid or expired.", Error: map[string]string{"code": "INVALID_SESSION"}})
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), authContextKey{}, claims.UserID)))
	})
}

func (a *api) issueSession(ctx context.Context, userID string, user map[string]interface{}) (map[string]interface{}, error) {
	access, err := a.signToken(userID, "access", 15*time.Minute)
	if err != nil {
		return nil, err
	}
	refresh, err := randomToken(48)
	if err != nil {
		return nil, err
	}
	refreshHash := hashToken(refresh)
	_, err = a.db.ExecContext(ctx, `
		INSERT INTO refresh_tokens (user_id, token_hash, expires_at)
		VALUES ($1, $2, now() + interval '30 days')`, userID, refreshHash)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{"user": user, "accessToken": access, "refreshToken": refresh}, nil
}

func (a *api) signingSecret(kind string) []byte {
	name := "JWT_SECRET"
	if kind == "refresh" {
		name = "JWT_REFRESH_SECRET"
	}
	secret := os.Getenv(name)
	if secret == "" {
		secret = os.Getenv("SESSION_SECRET")
	}
	return []byte(secret)
}

func (a *api) signToken(userID, tokenType string, duration time.Duration) (string, error) {
	secret := a.signingSecret(tokenType)
	if len(secret) == 0 {
		return "", errors.New("signing secret is not configured")
	}
	now := time.Now().UTC()
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	payload := base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf(`{"sub":%q,"typ":%q,"iat":%d,"exp":%d}`, userID, tokenType, now.Unix(), now.Add(duration).Unix())))
	input := header + "." + payload
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(input))
	return input + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func (a *api) parseToken(token, expectedType string) (tokenClaims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return tokenClaims{}, errors.New("invalid token")
	}
	secret := a.signingSecret(expectedType)
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(parts[0] + "." + parts[1]))
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || !hmac.Equal(signature, mac.Sum(nil)) {
		return tokenClaims{}, errors.New("invalid signature")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return tokenClaims{}, err
	}
	var claims struct {
		Sub string `json:"sub"`
		Typ string `json:"typ"`
		Exp int64  `json:"exp"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil || claims.Sub == "" || claims.Typ != expectedType || time.Now().Unix() >= claims.Exp {
		return tokenClaims{}, errors.New("expired token")
	}
	return tokenClaims{UserID: claims.Sub, Type: claims.Typ, Expiry: time.Unix(claims.Exp, 0)}, nil
}

func decodeJSON(r *http.Request, target interface{}) error {
	defer r.Body.Close()
	decoder := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	return decoder.Decode(target)
}

func randomToken(size int) (string, error) {
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

const passwordIterations = 310000

func hashPassword(password string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	derived := pbkdf2SHA256([]byte(password), salt, passwordIterations, 32)
	return fmt.Sprintf("pbkdf2-sha256$%d$%s$%s",
		passwordIterations,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(derived),
	), nil
}

func verifyPassword(encoded, password string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 4 || parts[0] != "pbkdf2-sha256" {
		return false
	}
	iterations, err := strconv.Atoi(parts[1])
	if err != nil || iterations < 100000 || iterations > 2000000 {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[2])
	if err != nil || len(salt) < 16 {
		return false
	}
	expected, err := base64.RawStdEncoding.DecodeString(parts[3])
	if err != nil || len(expected) != 32 {
		return false
	}
	actual := pbkdf2SHA256([]byte(password), salt, iterations, len(expected))
	return subtle.ConstantTimeCompare(actual, expected) == 1
}

func pbkdf2SHA256(password, salt []byte, iterations, keyLength int) []byte {
	const hashLength = 32
	blocks := (keyLength + hashLength - 1) / hashLength
	derived := make([]byte, 0, blocks*hashLength)
	for block := 1; block <= blocks; block++ {
		mac := hmac.New(sha256.New, password)
		_, _ = mac.Write(salt)
		_, _ = mac.Write([]byte{byte(block >> 24), byte(block >> 16), byte(block >> 8), byte(block)})
		u := mac.Sum(nil)
		t := append([]byte(nil), u...)
		for iteration := 1; iteration < iterations; iteration++ {
			mac = hmac.New(sha256.New, password)
			_, _ = mac.Write(u)
			u = mac.Sum(nil)
			for index := range t {
				t[index] ^= u[index]
			}
		}
		derived = append(derived, t...)
	}
	return derived[:keyLength]
}

func nullableString(value *string) interface{} {
	if value == nil {
		return nil
	}
	return *value
}

func nullableBool(value *bool) interface{} {
	if value == nil {
		return nil
	}
	return *value
}

func nullableValue(value sql.NullString) interface{} {
	if !value.Valid {
		return nil
	}
	return value.String
}

func (a *api) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, envelope{Success: true, Data: map[string]string{
		"service": "arman-api",
		"status":  "healthy",
	}, Message: nil, Error: nil})
}

func (a *api) ready(w http.ResponseWriter, r *http.Request) {
	if a.db == nil {
		writeJSON(w, http.StatusServiceUnavailable, envelope{
			Success: false,
			Data:    map[string]string{"status": "database_not_configured"},
			Message: "Database configuration is required for readiness.",
			Error:   map[string]string{"code": "DATABASE_NOT_CONFIGURED"},
		})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := a.db.PingContext(ctx); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, envelope{
			Success: false,
			Data:    map[string]string{"status": "database_unavailable"},
			Message: "Database is currently unavailable.",
			Error:   map[string]string{"code": "DATABASE_UNAVAILABLE"},
		})
		return
	}
	writeJSON(w, http.StatusOK, envelope{Success: true, Data: map[string]string{
		"status": "ready",
	}, Message: nil, Error: nil})
}

func (a *api) config(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, envelope{Success: true, Data: map[string]interface{}{
		"brand":   "ARMAN",
		"tagline": "Aspire. Learn. Achieve.",
		"features": map[string]bool{
			"authentication": (os.Getenv("JWT_SECRET") != "" && os.Getenv("JWT_REFRESH_SECRET") != "") || os.Getenv("SESSION_SECRET") != "",
			"database":       a.db != nil,
			"ai":             os.Getenv("AI_SERVICE_URL") != "",
			"search":         os.Getenv("OPENSEARCH_URL") != "",
			"storage":        os.Getenv("STORAGE_ENDPOINT") != "",
		},
	}, Message: nil, Error: nil})
}

func (a *api) resources(w http.ResponseWriter, r *http.Request) {
	if a.db == nil {
		writeJSON(w, http.StatusOK, envelope{Success: true, Data: map[string]interface{}{
			"items":      []interface{}{},
			"nextCursor": nil,
		}, Message: "No database is configured yet.", Error: nil})
		return
	}

	limit := 20
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed >= 1 && parsed <= 50 {
			limit = parsed
		} else {
			limit = 20
		}
	}
	query := `
		SELECT id, title, COALESCE(description, ''), resource_type, COALESCE(subject, ''),
		       moderation_status, file_url, thumbnail_url
		FROM resources
		WHERE deleted_at IS NULL AND moderation_status = 'approved'
		`
	args := []interface{}{limit}
	if search := strings.TrimSpace(r.URL.Query().Get("q")); search != "" {
		query += ` AND (title ILIKE $2 OR description ILIKE $2 OR subject ILIKE $2)`
		args = append(args, "%"+search+"%")
	}
	query += ` ORDER BY created_at DESC LIMIT $1`
	rows, err := a.db.QueryContext(r.Context(), query, args...)
	if err != nil {
		a.logger.Error("resources_query_failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, envelope{
			Success: false,
			Data:    nil,
			Message: "Resources could not be loaded.",
			Error:   map[string]string{"code": "RESOURCE_QUERY_FAILED"},
		})
		return
	}
	defer rows.Close()

	items := make([]map[string]interface{}, 0)
	for rows.Next() {
		var id, title, description, resourceType, subject, moderation string
		var fileURL, thumbnailURL sql.NullString
		if err := rows.Scan(&id, &title, &description, &resourceType, &subject, &moderation, &fileURL, &thumbnailURL); err != nil {
			a.logger.Error("resources_scan_failed", "error", err)
			continue
		}
		item := map[string]interface{}{
			"id": id, "title": title, "description": description, "type": resourceType,
			"subject": subject, "moderationStatus": moderation,
		}
		if fileURL.Valid {
			item["fileUrl"] = fileURL.String
		} else {
			item["fileUrl"] = nil
		}
		if thumbnailURL.Valid {
			item["thumbnailUrl"] = thumbnailURL.String
		} else {
			item["thumbnailUrl"] = nil
		}
		items = append(items, item)
	}
	writeJSON(w, http.StatusOK, envelope{Success: true, Data: map[string]interface{}{
		"items": items, "nextCursor": nil,
	}, Message: nil, Error: nil})
}

func writeJSON(w http.ResponseWriter, status int, body envelope) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func withRequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := r.Header.Get("X-Request-ID")
		if requestID == "" {
			requestID = time.Now().UTC().Format("20060102T150405.000000000Z")
		}
		w.Header().Set("X-Request-ID", requestID)
		next.ServeHTTP(w, r)
	})
}

func withCORS(next http.Handler) http.Handler {
	allowed := strings.Split(os.Getenv("CORS_ORIGINS"), ",")
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		for _, candidate := range allowed {
			if strings.TrimSpace(candidate) != "" && strings.TrimSpace(candidate) == origin {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Vary", "Origin")
				break
			}
		}
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-Request-ID")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
