package main

import (
	"database/sql"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type resourceInput struct {
	Title        string `json:"title"`
	Description  string `json:"description"`
	Type         string `json:"type"`
	Subject      string `json:"subject"`
	Course       string `json:"course"`
	Exam         string `json:"exam"`
	Language     string `json:"language"`
	FileURL      string `json:"fileUrl"`
	ThumbnailURL string `json:"thumbnailUrl"`
	Visibility   string `json:"visibility"`
}

var allowedResourceTypes = map[string]bool{
	"note": true, "pdf": true, "video": true, "link": true,
	"quiz": true, "flashcard": true, "audio": true,
}

func (a *api) createResource(w http.ResponseWriter, r *http.Request) {
	ownerID, _ := r.Context().Value(authContextKey{}).(string)
	var input resourceInput
	if err := decodeJSON(r, &input); err != nil {
		writeJSON(w, http.StatusBadRequest, envelope{Success: false, Data: nil, Message: "Please provide valid resource details.", Error: map[string]string{"code": "INVALID_REQUEST"}})
		return
	}
	if err := validateResourceInput(&input); err != nil {
		writeJSON(w, http.StatusBadRequest, envelope{Success: false, Data: nil, Message: err.Error(), Error: map[string]string{"code": "INVALID_RESOURCE"}})
		return
	}
	var id string
	err := a.db.QueryRowContext(r.Context(), `
		INSERT INTO resources (
			owner_id, title, description, resource_type, subject, course, exam,
			language, file_url, thumbnail_url, moderation_status, visibility
		) VALUES ($1, $2, $3, $4, NULLIF($5, ''), NULLIF($6, ''), NULLIF($7, ''),
		          $8, NULLIF($9, ''), NULLIF($10, ''), 'pending', $11)
		RETURNING id`,
		ownerID, input.Title, input.Description, input.Type, input.Subject,
		input.Course, input.Exam, input.Language, input.FileURL,
		input.ThumbnailURL, input.Visibility).Scan(&id)
	if err != nil {
		a.logger.Error("resource_create_failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, envelope{Success: false, Data: nil, Message: "Resource could not be published.", Error: map[string]string{"code": "RESOURCE_CREATE_FAILED"}})
		return
	}
	resource, err := a.getOwnedResource(r, id, ownerID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, envelope{Success: false, Data: nil, Message: "Resource was created but could not be loaded.", Error: map[string]string{"code": "RESOURCE_READ_FAILED"}})
		return
	}
	writeJSON(w, http.StatusCreated, envelope{Success: true, Data: resource, Message: "Resource submitted for moderation.", Error: nil})
}

func (a *api) myResources(w http.ResponseWriter, r *http.Request) {
	ownerID, _ := r.Context().Value(authContextKey{}).(string)
	rows, err := a.db.QueryContext(r.Context(), `
		SELECT id, title, COALESCE(description, ''), resource_type,
		       COALESCE(subject, ''), moderation_status, visibility,
		       COALESCE(file_url, ''), COALESCE(thumbnail_url, ''), created_at, updated_at
		FROM resources
		WHERE owner_id = $1 AND deleted_at IS NULL
		ORDER BY updated_at DESC`, ownerID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, envelope{Success: false, Data: nil, Message: "Your resources could not be loaded.", Error: map[string]string{"code": "MY_RESOURCES_QUERY_FAILED"}})
		return
	}
	defer rows.Close()
	items := make([]map[string]interface{}, 0)
	for rows.Next() {
		resource, err := scanManagedResource(rows)
		if err == nil {
			items = append(items, resource)
		}
	}
	writeJSON(w, http.StatusOK, envelope{Success: true, Data: items, Message: nil, Error: nil})
}

func (a *api) updateResource(w http.ResponseWriter, r *http.Request) {
	ownerID, _ := r.Context().Value(authContextKey{}).(string)
	id := r.PathValue("id")
	var input resourceInput
	if err := decodeJSON(r, &input); err != nil {
		writeJSON(w, http.StatusBadRequest, envelope{Success: false, Data: nil, Message: "Please provide valid resource details.", Error: map[string]string{"code": "INVALID_REQUEST"}})
		return
	}
	if err := validateResourceInput(&input); err != nil {
		writeJSON(w, http.StatusBadRequest, envelope{Success: false, Data: nil, Message: err.Error(), Error: map[string]string{"code": "INVALID_RESOURCE"}})
		return
	}
	result, err := a.db.ExecContext(r.Context(), `
		UPDATE resources SET
			title = $1, description = $2, resource_type = $3,
			subject = NULLIF($4, ''), course = NULLIF($5, ''), exam = NULLIF($6, ''),
			language = $7, file_url = NULLIF($8, ''), thumbnail_url = NULLIF($9, ''),
			visibility = $10, moderation_status = 'pending', updated_at = now()
		WHERE id = $11 AND owner_id = $12 AND deleted_at IS NULL`,
		input.Title, input.Description, input.Type, input.Subject, input.Course,
		input.Exam, input.Language, input.FileURL, input.ThumbnailURL,
		input.Visibility, id, ownerID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, envelope{Success: false, Data: nil, Message: "Resource could not be updated.", Error: map[string]string{"code": "RESOURCE_UPDATE_FAILED"}})
		return
	}
	count, _ := result.RowsAffected()
	if count == 0 {
		writeJSON(w, http.StatusNotFound, envelope{Success: false, Data: nil, Message: "Resource was not found or you do not own it.", Error: map[string]string{"code": "RESOURCE_NOT_FOUND"}})
		return
	}
	resource, err := a.getOwnedResource(r, id, ownerID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, envelope{Success: false, Data: nil, Message: "Resource was updated but could not be loaded.", Error: map[string]string{"code": "RESOURCE_READ_FAILED"}})
		return
	}
	writeJSON(w, http.StatusOK, envelope{Success: true, Data: resource, Message: "Resource resubmitted for moderation.", Error: nil})
}

func (a *api) deleteResource(w http.ResponseWriter, r *http.Request) {
	ownerID, _ := r.Context().Value(authContextKey{}).(string)
	result, err := a.db.ExecContext(r.Context(), `
		UPDATE resources SET deleted_at = now(), updated_at = now()
		WHERE id = $1 AND owner_id = $2 AND deleted_at IS NULL`,
		r.PathValue("id"), ownerID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, envelope{Success: false, Data: nil, Message: "Resource could not be deleted.", Error: map[string]string{"code": "RESOURCE_DELETE_FAILED"}})
		return
	}
	count, _ := result.RowsAffected()
	if count == 0 {
		writeJSON(w, http.StatusNotFound, envelope{Success: false, Data: nil, Message: "Resource was not found or you do not own it.", Error: map[string]string{"code": "RESOURCE_NOT_FOUND"}})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *api) uploadResourceFile(w http.ResponseWriter, r *http.Request) {
	storageDir := os.Getenv("STORAGE_LOCAL_DIR")
	if storageDir == "" {
		writeJSON(w, http.StatusServiceUnavailable, envelope{Success: false, Data: nil, Message: "File storage is not configured yet.", Error: map[string]string{"code": "STORAGE_NOT_CONFIGURED"}})
		return
	}
	const maxUpload = 25 << 20
	r.Body = http.MaxBytesReader(w, r.Body, maxUpload)
	if err := r.ParseMultipartForm(maxUpload); err != nil {
		writeJSON(w, http.StatusBadRequest, envelope{Success: false, Data: nil, Message: "The file is missing or larger than 25 MB.", Error: map[string]string{"code": "INVALID_UPLOAD"}})
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, envelope{Success: false, Data: nil, Message: "A file is required.", Error: map[string]string{"code": "FILE_REQUIRED"}})
		return
	}
	defer file.Close()
	extension := strings.ToLower(filepath.Ext(header.Filename))
	allowedExtensions := map[string]bool{".pdf": true, ".png": true, ".jpg": true, ".jpeg": true, ".webp": true, ".txt": true, ".mp3": true, ".mp4": true}
	if !allowedExtensions[extension] {
		writeJSON(w, http.StatusBadRequest, envelope{Success: false, Data: nil, Message: "That file type is not supported.", Error: map[string]string{"code": "UNSUPPORTED_FILE_TYPE"}})
		return
	}
	key, err := randomToken(24)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, envelope{Success: false, Data: nil, Message: "File upload could not be started.", Error: map[string]string{"code": "FILE_KEY_FAILED"}})
		return
	}
	key += extension
	if err := os.MkdirAll(storageDir, 0o750); err != nil {
		writeJSON(w, http.StatusInternalServerError, envelope{Success: false, Data: nil, Message: "File storage is unavailable.", Error: map[string]string{"code": "STORAGE_UNAVAILABLE"}})
		return
	}
	destination, err := os.OpenFile(filepath.Join(storageDir, key), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o640)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, envelope{Success: false, Data: nil, Message: "File storage is unavailable.", Error: map[string]string{"code": "FILE_CREATE_FAILED"}})
		return
	}
	defer destination.Close()
	written, err := io.Copy(destination, io.LimitReader(file, maxUpload+1))
	if err != nil || written > maxUpload {
		_ = os.Remove(filepath.Join(storageDir, key))
		writeJSON(w, http.StatusBadRequest, envelope{Success: false, Data: nil, Message: "The file could not be stored.", Error: map[string]string{"code": "FILE_WRITE_FAILED"}})
		return
	}
	writeJSON(w, http.StatusCreated, envelope{Success: true, Data: map[string]interface{}{
		"key": key, "fileUrl": "/api/v1/files/" + key, "size": written,
	}, Message: "File uploaded.", Error: nil})
}

func (a *api) downloadResourceFile(w http.ResponseWriter, r *http.Request) {
	storageDir := os.Getenv("STORAGE_LOCAL_DIR")
	if storageDir == "" {
		writeJSON(w, http.StatusServiceUnavailable, envelope{Success: false, Data: nil, Message: "File storage is not configured yet.", Error: map[string]string{"code": "STORAGE_NOT_CONFIGURED"}})
		return
	}
	key := filepath.Base(r.PathValue("key"))
	if key == "." || key == string(filepath.Separator) || strings.Contains(key, "..") {
		http.NotFound(w, r)
		return
	}
	var visibility, moderationStatus, ownerID string
	err := a.db.QueryRowContext(r.Context(), `
		SELECT visibility, moderation_status, COALESCE(owner_id::text, '')
		FROM resources WHERE file_url = $1 AND deleted_at IS NULL`,
		"/api/v1/files/"+key).Scan(&visibility, &moderationStatus, &ownerID)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if visibility != "public" || moderationStatus != "approved" {
		requestUserID := a.optionalUserID(r)
		if requestUserID == "" || requestUserID != ownerID {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
	}
	http.ServeFile(w, r, filepath.Join(storageDir, key))
}

func (a *api) getOwnedResource(r *http.Request, id, ownerID string) (map[string]interface{}, error) {
	var title, description, resourceType, subject, moderationStatus, visibility, fileURL, thumbnailURL string
	var createdAt, updatedAt time.Time
	err := a.db.QueryRowContext(r.Context(), `
		SELECT title, COALESCE(description, ''), resource_type, COALESCE(subject, ''),
		       moderation_status, visibility, COALESCE(file_url, ''), COALESCE(thumbnail_url, ''),
		       created_at, updated_at
		FROM resources WHERE id = $1 AND owner_id = $2 AND deleted_at IS NULL`,
		id, ownerID).Scan(&title, &description, &resourceType, &subject,
		&moderationStatus, &visibility, &fileURL, &thumbnailURL, &createdAt, &updatedAt)
	if err != nil {
		return nil, err
	}
	return managedResource(id, title, description, resourceType, subject, moderationStatus, visibility, fileURL, thumbnailURL, createdAt, updatedAt), nil
}

func scanManagedResource(rows *sql.Rows) (map[string]interface{}, error) {
	var id, title, description, resourceType, subject, moderationStatus, visibility, fileURL, thumbnailURL string
	var createdAt, updatedAt time.Time
	if err := rows.Scan(&id, &title, &description, &resourceType, &subject,
		&moderationStatus, &visibility, &fileURL, &thumbnailURL, &createdAt, &updatedAt); err != nil {
		return nil, err
	}
	return managedResource(id, title, description, resourceType, subject, moderationStatus, visibility, fileURL, thumbnailURL, createdAt, updatedAt), nil
}

func managedResource(id, title, description, resourceType, subject, moderationStatus, visibility, fileURL, thumbnailURL string, createdAt, updatedAt time.Time) map[string]interface{} {
	return map[string]interface{}{
		"id": id, "title": title, "description": description, "type": resourceType,
		"subject": subject, "moderationStatus": moderationStatus, "visibility": visibility,
		"fileUrl": emptyToNil(fileURL), "thumbnailUrl": emptyToNil(thumbnailURL),
		"createdAt": createdAt, "updatedAt": updatedAt,
	}
}

func validateResourceInput(input *resourceInput) error {
	input.Title = strings.TrimSpace(input.Title)
	input.Description = strings.TrimSpace(input.Description)
	input.Type = strings.ToLower(strings.TrimSpace(input.Type))
	input.Subject = strings.TrimSpace(input.Subject)
	input.Course = strings.TrimSpace(input.Course)
	input.Exam = strings.TrimSpace(input.Exam)
	input.Language = strings.TrimSpace(input.Language)
	input.FileURL = strings.TrimSpace(input.FileURL)
	input.ThumbnailURL = strings.TrimSpace(input.ThumbnailURL)
	input.Visibility = strings.ToLower(strings.TrimSpace(input.Visibility))
	if len(input.Title) < 2 || len(input.Title) > 240 {
		return errors.New("A title between 2 and 240 characters is required.")
	}
	if !allowedResourceTypes[input.Type] {
		return errors.New("Choose a supported resource type.")
	}
	if input.Visibility == "" {
		input.Visibility = "private"
	}
	if input.Visibility != "private" && input.Visibility != "public" {
		return errors.New("Visibility must be public or private.")
	}
	if input.Language == "" {
		input.Language = "en"
	}
	if input.FileURL != "" && !strings.HasPrefix(input.FileURL, "/api/v1/files/") && !strings.HasPrefix(input.FileURL, "https://") {
		return errors.New("File links must use ARMAN storage or HTTPS.")
	}
	if len(input.Description) > 10000 {
		return errors.New("Description must be 10,000 characters or fewer.")
	}
	return nil
}

func emptyToNil(value string) interface{} {
	if value == "" {
		return nil
	}
	return value
}

func (a *api) optionalUserID(r *http.Request) string {
	header := r.Header.Get("Authorization")
	if !strings.HasPrefix(header, "Bearer ") {
		return ""
	}
	claims, err := a.parseToken(strings.TrimSpace(strings.TrimPrefix(header, "Bearer ")), "access")
	if err != nil {
		return ""
	}
	return claims.UserID
}
