package main

import (
	"context"
	"database/sql"
	"net/http"
	"strings"
)

func (a *api) sendVerificationEmail(ctx context.Context, userID, email string) error {
	if a.mailer == nil {
		return errEmailDeliveryUnavailable
	}
	token, err := randomToken(40)
	if err != nil {
		return err
	}
	if _, err := a.db.ExecContext(ctx, `
		INSERT INTO email_verification_tokens (user_id, token_hash, expires_at)
		VALUES ($1, $2, now() + interval '24 hours')`,
		userID, hashToken(token)); err != nil {
		return err
	}
	link, err := a.mailer.VerificationURL(token)
	if err != nil {
		return err
	}
	return a.mailer.Send(ctx, email, "Verify your ARMAN account", "Welcome to ARMAN.\n\nVerify your email address here:\n"+link+"\n\nThis link expires in 24 hours.")
}

func (a *api) verifyEmail(w http.ResponseWriter, r *http.Request) {
	if a.db == nil {
		writeJSON(w, http.StatusServiceUnavailable, envelope{Success: false, Data: nil, Message: "Email verification requires a configured database.", Error: map[string]string{"code": "DATABASE_NOT_CONFIGURED"}})
		return
	}
	var input struct {
		Token string `json:"token"`
	}
	if err := decodeJSON(r, &input); err != nil || strings.TrimSpace(input.Token) == "" {
		writeJSON(w, http.StatusBadRequest, envelope{Success: false, Data: nil, Message: "A verification token is required.", Error: map[string]string{"code": "INVALID_REQUEST"}})
		return
	}
	result, err := a.db.ExecContext(r.Context(), `
		UPDATE users SET email_verified_at = now(), updated_at = now()
		WHERE id = (
			SELECT user_id FROM email_verification_tokens
			WHERE token_hash = $1 AND used_at IS NULL AND expires_at > now()
		) AND deleted_at IS NULL`, hashToken(strings.TrimSpace(input.Token)))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, envelope{Success: false, Data: nil, Message: "Email verification could not be completed.", Error: map[string]string{"code": "EMAIL_VERIFY_FAILED"}})
		return
	}
	count, _ := result.RowsAffected()
	if count == 0 {
		writeJSON(w, http.StatusBadRequest, envelope{Success: false, Data: nil, Message: "That verification link is invalid or expired.", Error: map[string]string{"code": "INVALID_VERIFICATION_TOKEN"}})
		return
	}
	_, _ = a.db.ExecContext(r.Context(), `
		UPDATE email_verification_tokens SET used_at = now()
		WHERE token_hash = $1 AND used_at IS NULL`, hashToken(strings.TrimSpace(input.Token)))
	writeJSON(w, http.StatusOK, envelope{Success: true, Data: map[string]bool{"verified": true}, Message: "Email verified.", Error: nil})
}

func (a *api) resendVerification(w http.ResponseWriter, r *http.Request) {
	userID, _ := r.Context().Value(authContextKey{}).(string)
	var email string
	var verified sql.NullTime
	if err := a.db.QueryRowContext(r.Context(), `SELECT email, email_verified_at FROM users WHERE id = $1 AND deleted_at IS NULL`, userID).Scan(&email, &verified); err != nil {
		writeJSON(w, http.StatusNotFound, envelope{Success: false, Data: nil, Message: "Account was not found.", Error: map[string]string{"code": "USER_NOT_FOUND"}})
		return
	}
	if verified.Valid {
		writeJSON(w, http.StatusConflict, envelope{Success: false, Data: nil, Message: "This email is already verified.", Error: map[string]string{"code": "EMAIL_ALREADY_VERIFIED"}})
		return
	}
	if err := a.sendVerificationEmail(r.Context(), userID, email); err != nil {
		writeEmailDeliveryError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, envelope{Success: true, Data: map[string]bool{"sent": true}, Message: "Verification email sent.", Error: nil})
}

func (a *api) forgotPassword(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Email string `json:"email"`
	}
	if err := decodeJSON(r, &input); err != nil || strings.TrimSpace(input.Email) == "" {
		writeJSON(w, http.StatusBadRequest, envelope{Success: false, Data: nil, Message: "An email address is required.", Error: map[string]string{"code": "INVALID_REQUEST"}})
		return
	}
	if a.mailer == nil {
		writeEmailDeliveryError(w, errEmailDeliveryUnavailable)
		return
	}
	var userID, email string
	err := a.db.QueryRowContext(r.Context(), `
		SELECT id, email FROM users
		WHERE email = $1 AND deleted_at IS NULL AND status <> 'banned'`,
		strings.ToLower(strings.TrimSpace(input.Email))).Scan(&userID, &email)
	if err != nil {
		// Do not reveal whether an account exists when delivery is configured.
		writeJSON(w, http.StatusAccepted, envelope{Success: true, Data: map[string]bool{"sent": true}, Message: "If an account exists, recovery instructions will be sent.", Error: nil})
		return
	}
	token, err := randomToken(40)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, envelope{Success: false, Data: nil, Message: "Recovery could not be started.", Error: map[string]string{"code": "RESET_TOKEN_FAILED"}})
		return
	}
	if _, err = a.db.ExecContext(r.Context(), `
		INSERT INTO password_reset_tokens (user_id, token_hash, expires_at)
		VALUES ($1, $2, now() + interval '30 minutes')`, userID, hashToken(token)); err != nil {
		writeJSON(w, http.StatusInternalServerError, envelope{Success: false, Data: nil, Message: "Recovery could not be started.", Error: map[string]string{"code": "RESET_TOKEN_STORE_FAILED"}})
		return
	}
	link, err := a.mailer.PasswordResetURL(token)
	if err == nil {
		err = a.mailer.Send(r.Context(), email, "Reset your ARMAN password", "A password reset was requested for your ARMAN account.\n\nReset it here:\n"+link+"\n\nThis link expires in 30 minutes. If you did not request this, you can ignore this email.")
	}
	if err != nil {
		writeEmailDeliveryError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, envelope{Success: true, Data: map[string]bool{"sent": true}, Message: "If an account exists, recovery instructions will be sent.", Error: nil})
}

func (a *api) resetPassword(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Token    string `json:"token"`
		Password string `json:"password"`
	}
	if err := decodeJSON(r, &input); err != nil || strings.TrimSpace(input.Token) == "" || len(input.Password) < 10 {
		writeJSON(w, http.StatusBadRequest, envelope{Success: false, Data: nil, Message: "A valid reset token and password of at least 10 characters are required.", Error: map[string]string{"code": "INVALID_REQUEST"}})
		return
	}
	passwordHash, err := hashPassword(input.Password)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, envelope{Success: false, Data: nil, Message: "Password reset could not be completed.", Error: map[string]string{"code": "PASSWORD_HASH_FAILED"}})
		return
	}
	tx, err := a.db.BeginTx(r.Context(), nil)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, envelope{Success: false, Data: nil, Message: "Password reset could not be completed.", Error: map[string]string{"code": "TRANSACTION_FAILED"}})
		return
	}
	defer tx.Rollback()
	var userID string
	if err := tx.QueryRowContext(r.Context(), `
		SELECT user_id FROM password_reset_tokens
		WHERE token_hash = $1 AND used_at IS NULL AND expires_at > now()`,
		hashToken(strings.TrimSpace(input.Token))).Scan(&userID); err != nil {
		writeJSON(w, http.StatusBadRequest, envelope{Success: false, Data: nil, Message: "That reset link is invalid or expired.", Error: map[string]string{"code": "INVALID_RESET_TOKEN"}})
		return
	}
	if _, err = tx.ExecContext(r.Context(), `UPDATE users SET password_hash = $1, updated_at = now() WHERE id = $2 AND deleted_at IS NULL`, passwordHash, userID); err != nil {
		writeJSON(w, http.StatusInternalServerError, envelope{Success: false, Data: nil, Message: "Password reset could not be completed.", Error: map[string]string{"code": "PASSWORD_UPDATE_FAILED"}})
		return
	}
	if _, err = tx.ExecContext(r.Context(), `UPDATE password_reset_tokens SET used_at = now() WHERE token_hash = $1`, hashToken(strings.TrimSpace(input.Token))); err != nil {
		writeJSON(w, http.StatusInternalServerError, envelope{Success: false, Data: nil, Message: "Password reset could not be completed.", Error: map[string]string{"code": "RESET_TOKEN_UPDATE_FAILED"}})
		return
	}
	if _, err = tx.ExecContext(r.Context(), `UPDATE refresh_tokens SET revoked_at = now() WHERE user_id = $1 AND revoked_at IS NULL`, userID); err != nil {
		writeJSON(w, http.StatusInternalServerError, envelope{Success: false, Data: nil, Message: "Password reset could not be completed.", Error: map[string]string{"code": "SESSION_REVOKE_FAILED"}})
		return
	}
	if err = tx.Commit(); err != nil {
		writeJSON(w, http.StatusInternalServerError, envelope{Success: false, Data: nil, Message: "Password reset could not be completed.", Error: map[string]string{"code": "TRANSACTION_COMMIT_FAILED"}})
		return
	}
	writeJSON(w, http.StatusOK, envelope{Success: true, Data: map[string]bool{"reset": true}, Message: "Password updated. Please sign in again.", Error: nil})
}

func (a *api) logout(w http.ResponseWriter, r *http.Request) {
	userID, _ := r.Context().Value(authContextKey{}).(string)
	var input struct {
		RefreshToken string `json:"refreshToken"`
	}
	if err := decodeJSON(r, &input); err != nil {
		writeJSON(w, http.StatusBadRequest, envelope{Success: false, Data: nil, Message: "Please provide a valid session request.", Error: map[string]string{"code": "INVALID_REQUEST"}})
		return
	}
	if input.RefreshToken != "" {
		_, _ = a.db.ExecContext(r.Context(), `UPDATE refresh_tokens SET revoked_at = now() WHERE user_id = $1 AND token_hash = $2`, userID, hashToken(input.RefreshToken))
	} else {
		_, _ = a.db.ExecContext(r.Context(), `UPDATE refresh_tokens SET revoked_at = now() WHERE user_id = $1 AND revoked_at IS NULL`, userID)
	}
	w.WriteHeader(http.StatusNoContent)
}

func writeEmailDeliveryError(w http.ResponseWriter, err error) {
	code := "EMAIL_DELIVERY_FAILED"
	message := "The email could not be sent."
	if err == errEmailDeliveryUnavailable {
		code = "EMAIL_DELIVERY_NOT_CONFIGURED"
		message = "Email delivery is not configured yet."
	}
	writeJSON(w, http.StatusServiceUnavailable, envelope{Success: false, Data: nil, Message: message, Error: map[string]string{"code": code}})
}

var errEmailDeliveryUnavailable = &emailDeliveryError{}

type emailDeliveryError struct{}

func (*emailDeliveryError) Error() string { return "email delivery is not configured" }
