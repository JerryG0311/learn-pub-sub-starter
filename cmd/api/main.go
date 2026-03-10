package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"golang.org/x/crypto/bcrypt"

	"github.com/JerryG0311/Vidify/internal/pubsub"
	"github.com/JerryG0311/Vidify/internal/routing"
	"github.com/JerryG0311/Vidify/internal/storage"
	amqp "github.com/rabbitmq/amqp091-go"
)

type VideoData struct {
	ID           string
	UserID       string
	Title        string
	Description  string
	Playlist     string
	SourcePath   string
	ThumbnailURL string
	Views        int
	CreatedAt    time.Time
	Status       string
}

type User struct {
	ID       int
	Email    string
	Password string
}

type ProfileData struct {
	Email             string
	DisplayName       string
	Username          string
	Bio               string
	Website           string
	Instagram         string
	ProfilePictureURL string
	TotalVideos       int
	TotalViews        int
	UsernameError     string
}

type GalleryPageData struct {
	Videos    []VideoData
	UserEmail string
}

func writeJSON(w http.ResponseWriter, status int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		log.Printf("Failed to write JSON response: %v", err)
	}
}

func sanitizeProfilePhotoFilename(userEmail, originalFilename string) string {
	ext := strings.ToLower(filepath.Ext(originalFilename))
	if ext == "" {
		ext = ".jpg"
	}

	safeEmail := strings.NewReplacer("@", "_at_", ".", "_", "+", "_plus_").Replace(strings.ToLower(userEmail))
	return fmt.Sprintf("profile-photos/%s-%d%s", safeEmail, time.Now().Unix(), ext)
}

func deriveProfileIdentity(email string) (string, string) {
	localPart := strings.TrimSpace(strings.Split(email, "@")[0])
	if localPart == "" {
		return email, ""
	}

	displayName := strings.NewReplacer(".", " ", "_", " ", "-", " ").Replace(localPart)
	displayName = strings.TrimSpace(displayName)
	if displayName == "" {
		displayName = email
	} else {
		displayName = strings.Title(displayName)
	}

	username := "@" + strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(localPart, " ", ""), ".", ""), "-", ""))
	return displayName, username
}

func normalizeUsername(value, fallbackEmail string) string {
	trimmed := strings.TrimSpace(strings.ToLower(value))
	if trimmed == "" {
		_, fallback := deriveProfileIdentity(fallbackEmail)
		return fallback
	}

	trimmed = strings.TrimPrefix(trimmed, "@")
	var builder strings.Builder
	for _, r := range trimmed {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '.' {
			builder.WriteRune(r)
		}
	}

	finalUsername := builder.String()
	if finalUsername == "" {
		_, fallback := deriveProfileIdentity(fallbackEmail)
		return fallback
	}
	return "@" + finalUsername
}

func normalizeDisplayName(value, fallbackEmail string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed != "" {
		return trimmed
	}
	fallbackDisplayName, _ := deriveProfileIdentity(fallbackEmail)
	return fallbackDisplayName
}

func isUsernameFormatValid(value string) bool {
	trimmed := strings.TrimPrefix(strings.TrimSpace(strings.ToLower(value)), "@")
	if len(trimmed) < 3 || len(trimmed) > 30 {
		return false
	}

	for _, r := range trimmed {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '.' {
			continue
		}
		return false
	}
	return true
}

func ensureUniqueUsername(db *sql.DB, username, currentEmail string) (string, error) {
	base := strings.TrimPrefix(normalizeUsername(username, currentEmail), "@")
	candidate := base

	for i := 0; i < 500; i++ {
		var count int
		err := db.QueryRow("SELECT COUNT(1) FROM users WHERE username = ? AND email != ?", "@"+candidate, currentEmail).Scan(&count)
		if err != nil {
			return "", err
		}
		if count == 0 {
			return "@" + candidate, nil
		}
		candidate = fmt.Sprintf("%s%d", base, i+1)
	}
	return "", fmt.Errorf("unable to generate a unique username")
}

func getLoggedInUser(r *http.Request) string {
	cookie, err := r.Cookie("session_user")
	if err != nil {
		return ""
	}
	return cookie.Value
}

func main() {

	var err error
	// -- Database  --

	db, err := sql.Open("sqlite3", "./data/vidify.db")
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	// -- RabbitMQ Setup --

	connString := os.Getenv("RABBITMQ_URL")
	if connString == "" {
		connString = "amqp://guest@localhost:5672/"
	}

	var conn *amqp.Connection
	for i := 0; i < 5; i++ {
		fmt.Printf("Connecting to RabbitMQ (attempt %d)... ", i+1)
		conn, err = amqp.Dial(connString)
		if err == nil {
			fmt.Println("Connected!")
			break
		}
		time.Sleep(2 * time.Second)
	}
	if err != nil {
		log.Fatalf("Could not connect to RabbitMQ: %v", err)
	}
	defer conn.Close()

	// 2. Creating a channel to declare Exchange
	ch, _ := conn.Channel()
	defer ch.Close()

	// Declare Exchanges and Queues
	ch.ExchangeDeclare(routing.ExchangeVideoTopic, "topic", true, false, false, false, nil)
	ch.ExchangeDeclare(routing.ExchangeVideoDLX, "fanout", true, false, false, false, nil)
	ch.QueueDeclare(routing.VideoDLQueue, true, false, false, false, nil)
	ch.QueueBind(routing.VideoDLQueue, "", routing.ExchangeVideoDLX, false, nil)

	// ---- AUTH HANDLERS ----
	http.HandleFunc("/signup", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			email := r.FormValue("email")
			pass := r.FormValue("password")
			hashed, _ := bcrypt.GenerateFromPassword([]byte(pass), bcrypt.DefaultCost)
			displayName, rawUsername := deriveProfileIdentity(email)

			username, err := ensureUniqueUsername(db, rawUsername, email)
			if err != nil {
				log.Printf("Error generating unique username: %v", err)
				http.Error(w, "unable to create account", http.StatusInternalServerError)
				return
			}
			_, err = db.Exec("INSERT INTO users (email, password, display_name, username) VALUES (?, ?, ?, ?)", email, string(hashed), displayName, username)
			if err != nil {
				http.Error(w, "User already exists", http.StatusConflict)
				return
			}
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		tmpl, err := template.ParseFiles("web/templates/auth.html")
		if err != nil {
			http.Error(w, "Auth template missing: "+err.Error(), 500)
			return
		}
		tmpl.Execute(w, map[string]string{"Type": "signup"})
	})

	http.HandleFunc("/login", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			email := r.FormValue("email")
			pass := r.FormValue("password")

			var hashedPass string
			err := db.QueryRow("SELECT password FROM users WHERE email = ?", email).Scan(&hashedPass)
			if err != nil || bcrypt.CompareHashAndPassword([]byte(hashedPass), []byte(pass)) != nil {
				http.Error(w, "Invalid credentials", http.StatusUnauthorized)
				return
			}

			http.SetCookie(w, &http.Cookie{
				Name: "session_user", Value: email, Path: "/", HttpOnly: true,
			})
			http.Redirect(w, r, "/gallery", http.StatusSeeOther)
			return
		}
		tmpl, err := template.ParseFiles("web/templates/auth.html")
		if err != nil {
			http.Error(w, "Auth template missing: "+err.Error(), 500)
			return
		}
		tmpl.Execute(w, map[string]string{"Type": "login"})
	})

	http.HandleFunc("/logout", func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "session_user", Value: "", Path: "/", MaxAge: -1})
		http.Redirect(w, r, "/login", http.StatusSeeOther)
	})

	http.HandleFunc("/profile", func(w http.ResponseWriter, r *http.Request) {
		userEmail := getLoggedInUser(r)
		if userEmail == "" {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}

		var data ProfileData
		data.Email = userEmail

		switch r.URL.Query().Get("error") {
		case "invalid_username":
			data.UsernameError = "Username must be 3-30 characters and use only letters, numbers, periods, or underscores."
		case "username_take":
			data.UsernameError = "That username is already taken. Try another one."
		}

		query := `
			SELECT 
				IFNULL(u.display_name, ''),
				IFNULL(u.username, ''),
				IFNULL(u.bio, ''),
				IFNULL(u.website, ''),
				IFNULL(u.instagram, ''),
				IFNULL(u.profile_picture_url, ''), 
				COUNT(v.id), 
				IFNULL(SUM(v.views), 0) 
			FROM users u 
			LEFT JOIN videos v ON u.email = v.user_id 
			WHERE u.email = ?
			GROUP BY u.email`

		err := db.QueryRow(query, userEmail).Scan(
			&data.DisplayName,
			&data.Username,
			&data.Bio,
			&data.Website,
			&data.Instagram,
			&data.ProfilePictureURL,
			&data.TotalVideos,
			&data.TotalViews,
		)

		if err != nil {
			if err == sql.ErrNoRows {
				data.Bio = ""
				data.Website = ""
				data.Instagram = ""
				data.TotalVideos = 0
				data.TotalViews = 0
			} else {
				log.Printf("Database error: %v", err)
				http.Error(w, "Internal Server Error", 500)
				return
			}
		}

		if strings.TrimSpace(data.DisplayName) == "" || strings.TrimSpace(data.Username) == "" {
			fallbackDisplayName, fallbackUsername := deriveProfileIdentity(userEmail)
			if strings.TrimSpace(data.DisplayName) == "" {
				data.DisplayName = fallbackDisplayName
			}
			if strings.TrimSpace(data.Username) == "" {
				data.Username = fallbackUsername
			}
		}

		tmpl, err := template.ParseFiles("web/templates/profile.html")
		if err != nil {
			log.Printf("Template error: %v", err)
			http.Error(w, "Template not found", 500)
			return
		}
		tmpl.Execute(w, data)
	})

	http.HandleFunc("/profile/update", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		userEmail := getLoggedInUser(r)
		if userEmail == "" {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}

		if err := r.ParseForm(); err != nil {
			log.Printf("Error parsing profile update form: %v", err)
			http.Error(w, "Invalid form submission", http.StatusBadRequest)
			return
		}

		displayName := normalizeDisplayName(r.FormValue("display_name"), userEmail)
		rawUsername := r.FormValue("username")
		if !isUsernameFormatValid(rawUsername) {
			http.Redirect(w, r, "/profile?error=invalid_username", http.StatusSeeOther)
			return
		}
		username := normalizeUsername(rawUsername, userEmail)
		newBio := strings.TrimSpace(r.FormValue("bio"))
		website := strings.TrimSpace(r.FormValue("website"))
		instagram := strings.TrimSpace(r.FormValue("instagram"))

		_, err := db.Exec(
			"UPDATE users SET display_name = ?, username = ?, bio = ?, website = ?, instagram = ? WHERE email = ?",
			displayName,
			username,
			newBio,
			website,
			instagram,
			userEmail,
		)
		if err != nil {
			if strings.Contains(err.Error(), "users.username") || strings.Contains(err.Error(), "idx_users_username") {
				http.Redirect(w, r, "/profile?error=username_taken", http.StatusSeeOther)
				return
			}
			log.Printf("Error updating profile: %v", err)
			http.Error(w, "Failed to update profile", http.StatusInternalServerError)
			return
		}

		http.Redirect(w, r, "/profile", http.StatusSeeOther)
	})

	http.HandleFunc("/profile/photo", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
			return
		}

		userEmail := getLoggedInUser(r)
		if userEmail == "" {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "You must be logged in to update your profile photo."})
			return
		}

		if err := r.ParseMultipartForm(10 << 20); err != nil {
			log.Printf("Error parsing profile photo upload form: %v", err)
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid upload payload."})
			return
		}

		file, header, err := r.FormFile("pfp")
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Please choose an image to upload."})
			return
		}
		defer file.Close()

		contentType := header.Header.Get("Content-Type")
		switch contentType {
		case "image/jpeg", "image/png", "image/webp":
		default:
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Please upload a JPG, PNG, or WebP image."})
			return
		}

		fileBytes, err := io.ReadAll(file)
		if err != nil {
			log.Printf("Error reading uploaded profile photo: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to read uploaded image."})
			return
		}

		if len(fileBytes) == 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "The uploaded image was empty."})
			return
		}

		finalFilename := sanitizeProfilePhotoFilename(userEmail, header.Filename)
		reader := bytes.NewReader(fileBytes)
		pfpURL, uploadErr := storage.UploadToS3(finalFilename, reader)
		if uploadErr != nil {
			log.Printf("S3 profile photo upload error: %v", uploadErr)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to upload profile photo."})
			return
		}

		_, err = db.Exec("UPDATE users SET profile_picture_url = ? WHERE email = ?", pfpURL, userEmail)
		if err != nil {
			log.Printf("Error updating profile photo URL in database: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to save profile photo."})
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{"profilePictureURL": pfpURL})
	})

	// ---- New Web Server Code ---

	// 1. Parse the file from the request ("video" is the key used in the curl command)

	http.HandleFunc("/upload", func(w http.ResponseWriter, r *http.Request) {
		userEmail := getLoggedInUser(r)
		if userEmail == "" {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}

		if r.Method == http.MethodGet {
			tmpl, err := template.ParseFiles("web/templates/upload.html")
			if err != nil {
				http.Error(w, "Template not found", 500)
				return
			}
			tmpl.Execute(w, nil)
			return
		}
		if r.Method == http.MethodPost {
			r.ParseMultipartForm(500 << 20)
			title := r.FormValue("title")
			description := r.FormValue("description")
			playlist := r.FormValue("playlist")

			file, header, err := r.FormFile("video")
			if err != nil {
				http.Error(w, "File error", 400)
				return
			}
			defer file.Close()

			if title == "" {
				title = header.Filename
			}

			job := routing.VideoJob{
				ID:           fmt.Sprintf("vid-%d", time.Now().Unix()),
				SourcePath:   "",
				TargetFormat: "mp4",
				UserID:       userEmail,
				CreatedAt:    time.Now(),
			}

			s3URL, err := storage.UploadToS3(header.Filename, file)
			if err != nil {
				http.Error(w, "S3 Upload failed", 500)
				return
			}
			job.SourcePath = s3URL

			_, err = db.Exec(
				"INSERT INTO videos (id, user_id, status, source_path, thumbnail_url, title, description, playlist, created_at, views) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
				job.ID, userEmail, "PENDING", job.SourcePath, "", title, description, playlist, job.CreatedAt, 0,
			)
			pubsub.PublishJSON(ch, routing.ExchangeVideoTopic, routing.VideoUploadKey, job)
			w.WriteHeader(http.StatusOK)
			return
		}
	})

	http.HandleFunc("/status/", func(w http.ResponseWriter, r *http.Request) {
		id := filepath.Base(r.URL.Path)

		var status string
		err := db.QueryRow("SELECT status FROM videos WHERE id = ?", id).Scan(&status)
		if err != nil {
			http.Error(w, "Not found", 404)
			return
		}

		fmt.Fprintf(w, "Video ID: %s\nStatus: %s", id, status)
	})

	http.Handle("/data/", http.StripPrefix("/data/", http.FileServer(http.Dir("./data"))))

	http.HandleFunc("/gallery", func(w http.ResponseWriter, r *http.Request) {
		userEmail := getLoggedInUser(r)

		// If not logged in, send to login page
		if userEmail == "" {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}

		// 1. Fetch only videos belonging to THIS logged-in user
		rows, err := db.Query("SELECT id, status, title, playlist, source_path, thumbnail_url, views FROM videos WHERE user_id = ? ORDER BY created_at DESC", userEmail)
		if err != nil {
			log.Printf("Database Query Error: %v", err)
			http.Error(w, "Unable to load your library", http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		var videos []VideoData
		for rows.Next() {
			var v VideoData
			var thumb, playlist sql.NullString

			// Ensure we scan correctly into NullStrings
			err := rows.Scan(&v.ID, &v.Status, &v.Title, &playlist, &v.SourcePath, &thumb, &v.Views)
			if err != nil {
				log.Printf("Scan error for video %s: %v", v.ID, err)
				continue
			}

			if playlist.Valid {
				v.Playlist = playlist.String
			}
			if thumb.Valid && thumb.String != "" {
				v.ThumbnailURL = thumb.String
			} else {
				v.ThumbnailURL = fmt.Sprintf("https://%s.s3.us-east-2.amazonaws.com/%s_thumb.jpg", os.Getenv("S3_BUCKET_NAME"), v.ID)
			}
			videos = append(videos, v)
		}

		// 2. Load the template (Parse it fresh to avoid nil pointers)
		tmpl, err := template.ParseFiles("web/templates/gallery.html")
		if err != nil {
			log.Printf("Template loading error: %v", err)
			http.Error(w, "Dashboard layout file missing", http.StatusInternalServerError)
			return
		}

		// 3. Prepare data safely
		data := GalleryPageData{
			Videos:    videos,
			UserEmail: userEmail,
		}

		// Execute with explicit error checking
		err = tmpl.Execute(w, data)
		if err != nil {
			log.Printf("Template execution error: %v", err)
		}
	})

	http.HandleFunc("/view/", func(w http.ResponseWriter, r *http.Request) {
		id := filepath.Base(r.URL.Path)
		// 1. Check if "?embed=true" is in the URL
		isEmbed := r.URL.Query().Get("embed") == "true"
		// 2. Increment views (only if NOT an embed, or keep it to track both)
		db.Exec("UPDATE videos SET views = views + 1 WHERE id = ?", id)

		var v VideoData
		query := `SELECT id, title, description, playlist, source_path, thumbnail_url, views, created_at 
				FROM videos WHERE id = ?`

		err := db.QueryRow(query, id).Scan(
			&v.ID, &v.Title, &v.Description, &v.Playlist, &v.SourcePath, &v.ThumbnailURL, &v.Views, &v.CreatedAt,
		)

		if err != nil {
			http.Redirect(w, r, "/gallery", 303)
			return
		}

		tmpl, _ := template.ParseFiles("web/templates/view.html")
		tmpl.Execute(w, map[string]interface{}{"Video": v, "IsEmbed": isEmbed})
	})

	http.HandleFunc("/delete/", func(w http.ResponseWriter, r *http.Request) {
		id := filepath.Base(r.URL.Path)
		storage.DeleteFromS3(id + "_processed.mp4")
		storage.DeleteFromS3(id + "_thumb.jpg")
		db.Exec("DELETE FROM videos WHERE id = ?", id)
		http.Redirect(w, r, "/gallery", 303)
	})

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/gallery", http.StatusSeeOther)
	})

	http.HandleFunc("/edit/", func(w http.ResponseWriter, r *http.Request) {
		id := filepath.Base(r.URL.Path)
		if r.Method == http.MethodPost {
			newTitle := r.FormValue("title")
			if newTitle == "" {
				newTitle = r.URL.Query().Get("title")
			}
			db.Exec("UPDATE videos SET title = ? WHERE id = ?", newTitle, id)
			w.WriteHeader(http.StatusOK) // Fix: was InternalServerError
			return
		}
		http.Redirect(w, r, "/gallery", 303)
	})

	http.HandleFunc("/manage-thumb/", func(w http.ResponseWriter, r *http.Request) {
		id := filepath.Base(r.URL.Path)
		if r.Method == http.MethodPost {
			action := r.FormValue("thumb_action")
			var finalThumbURL string
			if action == "change" {
				file, header, _ := r.FormFile("new_thumbnail")
				defer file.Close()
				thumbName := fmt.Sprintf("%s_custom_%d%s", id, time.Now().Unix(), filepath.Ext(header.Filename))
				finalThumbURL, _ = storage.UploadToS3(thumbName, file)
			} else if action == "remove" {
				finalThumbURL = ""
			} else {
				db.QueryRow("SELECT thumbnail_url FROM videos WHERE id = ?", id).Scan(&finalThumbURL)
			}
			db.Exec("UPDATE videos SET thumbnail_url = ? WHERE id = ?", finalThumbURL, id)
		}
		http.Redirect(w, r, "/gallery", 303)
	})

	http.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("web/static"))))

	fmt.Println("Vidify web server running on http://localhost:8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
