package main

import (
	"bytes"
	"database/sql"
	"encoding/csv"
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
	ID                 string
	UserID             string
	Title              string
	Description        string
	Playlist           string
	SourcePath         string
	ThumbnailURL       string
	Views              int
	CreatedAt          time.Time
	Status             string
	CTAText            string
	CTAHeroText        string
	CTAURL             string
	CTATimeSeconds     int
	CTAType            string
	CTAClicks          int
	ShareCount         int
	DownloadCount      int
	PlayerAutoplay     bool
	PlayerMuted        bool
	PlayerControls     bool
	PlayerStartSeconds int
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
	WebhookURL        string
	ProfilePictureURL string
	TotalVideos       int
	TotalViews        int
	UsernameError     string
}

type GalleryPageData struct {
	Videos    []VideoData
	UserEmail string
}

type CreatorPageData struct {
	Creator ProfileData
	Videos  []VideoData
}

type PlayerOptions struct {
	Autoplay     bool
	Muted        bool
	Controls     bool
	StartSeconds int
}

type VideoCTA struct {
	ID          string    `json:"id"`
	VideoID     string    `json:"videoId"`
	Text        string    `json:"text"`
	HeroText    string    `json:"heroText"`
	URL         string    `json:"url"`
	CTAType     string    `json:"ctaType"`
	TimeSeconds int       `json:"timeSeconds"`
	CreatedAt   time.Time `json:"createdAt"`
}

type FunnelData struct {
	ID        string
	UserID    string
	Name      string
	CreatedAt time.Time
}

type FunnelStepData struct {
	ID        string
	FunnelID  string
	StepType  string
	VideoID   string
	Position  int
	CreatedAt time.Time
}

type ViewPageData struct {
	Video         VideoData
	Creator       ProfileData
	RelatedVideos []VideoData
	CTAs          []VideoCTA
	IsEmbed       bool
	PlayerOptions PlayerOptions
}

type LeadData struct {
	Name           string
	Email          string
	CTAID          string
	CTAType        string
	CTATimeSeconds int
	CTAHeroText    string
	CreatedAt      time.Time
}

type CTAPerformanceData struct {
	CTAID           string
	Text            string
	HeroText        string
	CTAType         string
	TimeSeconds     int
	ImpressionCount int
	LeadCount       int
	UniqueLeadCount int
	ConversionRate  float64
}

type RetentionPoint struct {
	Second int `json:"second"`
	Views  int `json:"views"`
}

type DropoffBucketData struct {
	Label        string
	StartSecond  int
	EndSecond    int
	StartViews   int
	EndViews     int
	DropoffCount int
	DropoffRate  float64
}

type StatsPageData struct {
	Video            VideoData
	UserEmail        string
	ShareCount       int
	DownloadCount    int
	CTR              float64
	LeadCount        int
	Leads            []LeadData
	CTAPerformance   []CTAPerformanceData
	RetentionJSON    template.JS
	DropoffHeatmap   []DropoffBucketData
	RetentionPeak    int
	RetentionMaxTime int
}

type webhookPayload struct {
	Event      string `json:"event"`
	VideoID    string `json:"video_id"`
	VideoTitle string `json:"video_title"`
	UserEmail  string `json:"user_email"`
	Name       string `json:"name"`
	Email      string `json:"email"`
	CapturedAt string `json:"captured_at"`
}

type retentionPayload struct {
	VideoID string `json:"video_id"`
	Second  int    `json:"second"`
}

func parsePlayerOptions(r *http.Request) PlayerOptions {
	options := PlayerOptions{
		Autoplay:     false,
		Muted:        false,
		Controls:     true,
		StartSeconds: 0,
	}

	query := r.URL.Query()

	autoplayRaw := strings.TrimSpace(strings.ToLower(query.Get("autoplay")))
	if autoplayRaw == "1" || autoplayRaw == "true" || autoplayRaw == "yes" {
		options.Autoplay = true
	}

	mutedRaw := strings.TrimSpace(strings.ToLower(query.Get("muted")))
	if mutedRaw == "1" || mutedRaw == "true" || mutedRaw == "yes" {
		options.Muted = true
	}

	controlsRaw := strings.TrimSpace(strings.ToLower(query.Get("controls")))
	if controlsRaw == "0" || controlsRaw == "false" || controlsRaw == "no" {
		options.Controls = false
	}

	startRaw := strings.TrimSpace(query.Get("start"))
	if startRaw != "" {
		var parsedStart int
		if _, err := fmt.Sscanf(startRaw, "%d", &parsedStart); err == nil && parsedStart >= 0 {
			options.StartSeconds = parsedStart
		}
	}

	if options.Autoplay {
		options.Muted = true
	}

	return options
}

func resolvePlayerOptions(r *http.Request, video VideoData) PlayerOptions {
	options := PlayerOptions{
		Autoplay:     video.PlayerAutoplay,
		Muted:        video.PlayerMuted,
		Controls:     video.PlayerControls,
		StartSeconds: video.PlayerStartSeconds,
	}

	query := r.URL.Query()

	if raw := strings.TrimSpace(strings.ToLower(query.Get("autoplay"))); raw != "" {
		options.Autoplay = raw == "1" || raw == "true" || raw == "yes"
	}

	if raw := strings.TrimSpace(strings.ToLower(query.Get("muted"))); raw != "" {
		options.Muted = raw == "1" || raw == "true" || raw == "yes"
	}

	if raw := strings.TrimSpace(strings.ToLower(query.Get("controls"))); raw != "" {
		options.Controls = !(raw == "0" || raw == "false" || raw == "no")
	}

	if raw := strings.TrimSpace(query.Get("start")); raw != "" {
		var parsedStart int
		if _, err := fmt.Sscanf(raw, "%d", &parsedStart); err == nil && parsedStart >= 0 {
			options.StartSeconds = parsedStart
		}
	}

	if options.Autoplay {
		options.Muted = true
	}

	return options
}

func renderVideoPage(db *sql.DB, w http.ResponseWriter, r *http.Request, id string, isEmbed bool) {
	if id == "" {
		http.Redirect(w, r, "/gallery", http.StatusSeeOther)
		return
	}

	var playerOptions PlayerOptions
	if !isEmbed {
		db.Exec("UPDATE videos SET views = views + 1 WHERE id = ?", id)
	}

	var v VideoData
	var thumbnail sql.NullString

	query := `
		SELECT 
		id, 
		user_id, 
		title, 
		description, 
		playlist, 
		source_path, 
		thumbnail_url, 
		views, 
		created_at, 
		status, 
		IFNULL(cta_text, ''), 
		IFNULL(cta_hero_text, ''),
		IFNULL(cta_url, ''), 
		IFNULL(cta_time_seconds, 0), 
		IFNULL(cta_type, 'button'),
		IFNULL(cta_clicks, 0),
		IFNULL(player_autoplay, 0),
		IFNULL(player_muted, 0),
		IFNULL(player_controls, 1),
		IFNULL(player_start_seconds, 0)
		FROM videos
		WHERE id = ?`

	err := db.QueryRow(query, id).Scan(
		&v.ID,
		&v.UserID,
		&v.Title,
		&v.Description,
		&v.Playlist,
		&v.SourcePath,
		&thumbnail,
		&v.Views,
		&v.CreatedAt,
		&v.Status,
		&v.CTAText,
		&v.CTAHeroText,
		&v.CTAURL,
		&v.CTATimeSeconds,
		&v.CTAType,
		&v.CTAClicks,
		&v.PlayerAutoplay,
		&v.PlayerMuted,
		&v.PlayerControls,
		&v.PlayerStartSeconds,
	)
	if err != nil {
		http.Redirect(w, r, "/gallery", http.StatusSeeOther)
		return
	}

	if thumbnail.Valid {
		v.ThumbnailURL = thumbnail.String
	}
	playerOptions = resolvePlayerOptions(r, v)

	var creator ProfileData
	creatorQuery := `
		SELECT
			email,
			IFNULL(display_name, ''),
			IFNULL(username, ''),
			IFNULL(bio, ''),
			IFNULL(website, ''),
			IFNULL(instagram, ''),
			IFNULL(profile_picture_url, '')
		FROM users
		WHERE email = ?`

	err = db.QueryRow(creatorQuery, v.UserID).Scan(
		&creator.Email,
		&creator.DisplayName,
		&creator.Username,
		&creator.Bio,
		&creator.Website,
		&creator.Instagram,
		&creator.ProfilePictureURL,
	)
	if err != nil && err != sql.ErrNoRows {
		log.Printf("Creator lookup error for video %s: %v", v.ID, err)
	}

	var ctas []VideoCTA
	ctaRows, err := db.Query(`
		SELECT id, video_id, cta_text, IFNULL(cta_hero_text, ''), cta_url, IFNULL(cta_type, 'button'), cta_time_seconds, created_at
		FROM video_ctas
		WHERE video_id = ?
		ORDER BY cta_time_seconds ASC, created_at ASC
	`, v.ID)
	if err == nil {
		defer ctaRows.Close()

		for ctaRows.Next() {
			var cta VideoCTA
			if err := ctaRows.Scan(
				&cta.ID,
				&cta.VideoID,
				&cta.Text,
				&cta.HeroText,
				&cta.URL,
				&cta.CTAType,
				&cta.TimeSeconds,
				&cta.CreatedAt,
			); err != nil {
				log.Printf("CTA scan error for %s: %v", v.ID, err)
				continue
			}
			ctas = append(ctas, cta)
		}
	} else {
		log.Printf("CTA query error for %s: %v", v.ID, err)
	}

	var relatedVideos []VideoData
	relatedRows, err := db.Query(`
		SELECT id, user_id, title, description, playlist, source_path, thumbnail_url, views, created_at, status
		FROM videos
		WHERE user_id = ? AND id != ?
		ORDER BY created_at DESC
		LIMIT 6`, v.UserID, v.ID)
	if err == nil {
		defer relatedRows.Close()

		for relatedRows.Next() {
			var rv VideoData
			var relatedPlaylist, relatedThumb sql.NullString

			if err := relatedRows.Scan(
				&rv.ID,
				&rv.UserID,
				&rv.Title,
				&rv.Description,
				&relatedPlaylist,
				&rv.SourcePath,
				&relatedThumb,
				&rv.Views,
				&rv.CreatedAt,
				&rv.Status,
			); err != nil {
				log.Printf("Related video scan error for %s: %v", v.ID, err)
				continue
			}

			if relatedPlaylist.Valid {
				rv.Playlist = relatedPlaylist.String
			}
			if relatedThumb.Valid {
				rv.ThumbnailURL = relatedThumb.String
			}

			relatedVideos = append(relatedVideos, rv)
		}
	} else {
		log.Printf("Related videos query error for %s: %v", v.ID, err)
	}

	tmpl, err := template.ParseFiles("web/templates/view.html")
	if err != nil {
		log.Printf("View template error: %v", err)
		http.Error(w, "View template not found", http.StatusInternalServerError)
		return
	}

	data := ViewPageData{
		Video:         v,
		Creator:       creator,
		RelatedVideos: relatedVideos,
		CTAs:          ctas,
		IsEmbed:       isEmbed,
		PlayerOptions: playerOptions,
	}

	if err := tmpl.Execute(w, data); err != nil {
		log.Printf("View template execution error: %v", err)
	}
}

func writeJSON(w http.ResponseWriter, status int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		log.Printf("Failed to write JSON response: %v", err)
	}
}

func retentionViewsAtOrAfter(points []RetentionPoint, target int) int {
	for _, point := range points {
		if point.Second >= target {
			return point.Views
		}
	}
	if len(points) > 0 {
		return points[len(points)-1].Views
	}
	return 0
}

func retentionViewsAtOrBefore(points []RetentionPoint, target int) int {
	lastViews := 0
	for _, point := range points {
		if point.Second > target {
			break
		}
		lastViews = point.Views
	}
	if lastViews == 0 && len(points) > 0 {
		return points[0].Views
	}
	return lastViews
}

func buildDropoffHeatmap(points []RetentionPoint) []DropoffBucketData {
	if len(points) == 0 {
		return nil
	}

	maxSecond := points[len(points)-1].Second
	bucketSize := 10
	var heatmap []DropoffBucketData

	for start := 0; start <= maxSecond; start += bucketSize {
		end := start + bucketSize - 1
		if end > maxSecond {
			end = maxSecond
		}

		startViews := retentionViewsAtOrAfter(points, start)
		endViews := retentionViewsAtOrBefore(points, end)
		if endViews > startViews {
			endViews = startViews
		}

		dropoffCount := startViews - endViews
		dropoffRate := 0.0
		if startViews > 0 {
			dropoffRate = (float64(dropoffCount) / float64(startViews)) * 100
		}

		heatmap = append(heatmap, DropoffBucketData{
			Label:        fmt.Sprintf("%ds–%ds", start, end),
			StartSecond:  start,
			EndSecond:    end,
			StartViews:   startViews,
			EndViews:     endViews,
			DropoffCount: dropoffCount,
			DropoffRate:  dropoffRate,
		})
	}

	return heatmap
}

func sendWebHook(webhookURL string, payload webhookPayload) {
	if strings.TrimSpace(webhookURL) == "" {
		return
	}

	body, err := json.Marshal(payload)
	if err != nil {
		log.Printf("Webhook marshal error: %v", err)
		return
	}

	req, err := http.NewRequest(http.MethodPost, webhookURL, bytes.NewBuffer(body))
	if err != nil {
		log.Printf("Webhook request creation error: %v", err)
		return
	}

	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{
		Timeout: 5 * time.Second,
	}

	resp, err := client.Do(req)
	if err != nil {
		log.Printf("webhook send error: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		log.Printf("Webhook returned non-2xx status: %d", resp.StatusCode)
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
		case "username_taken":
			data.UsernameError = "That username is already taken. Try another one."
		}

		query := `
			SELECT 
				IFNULL(u.display_name, ''),
				IFNULL(u.username, ''),
				IFNULL(u.bio, ''),
				IFNULL(u.website, ''),
				IFNULL(u.instagram, ''),
				IFNULL(u.webhook_url, ''),
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
			&data.WebhookURL,
			&data.ProfilePictureURL,
			&data.TotalVideos,
			&data.TotalViews,
		)

		if err != nil {
			if err == sql.ErrNoRows {
				data.Bio = ""
				data.Website = ""
				data.Instagram = ""
				data.WebhookURL = ""
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
		webhookURL := strings.TrimSpace(r.FormValue("webhook_url"))

		_, err := db.Exec(
			"UPDATE users SET display_name = ?, username = ?, bio = ?, website = ?, instagram = ?, webhook_url = ? WHERE email = ?",
			displayName,
			username,
			newBio,
			website,
			instagram,
			webhookURL,
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

	http.HandleFunc("/test-webhook", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Redirect(w, r, "/profile", http.StatusSeeOther)
			return
		}

		userEmail := getLoggedInUser(r)
		if userEmail == "" {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}

		var webhookURL string
		err := db.QueryRow("SELECT IFNULL(webhook_url, '') FROM users WHERE email = ?", userEmail).Scan(&webhookURL)
		if err != nil {
			log.Printf("Webhook lookup error for test event: %v", err)
			http.Redirect(w, r, "/profile", http.StatusSeeOther)
			return
		}

		payload := webhookPayload{
			Event:      "test_lead",
			VideoID:    "vidify-test",
			VideoTitle: "Vidify Test Video",
			UserEmail:  userEmail,
			Name:       "Test Lead",
			Email:      "test@vidify.app",
			CapturedAt: time.Now().UTC().Format(time.RFC3339),
		}

		go sendWebHook(webhookURL, payload)

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
		rows, err := db.Query("SELECT id, status, title, playlist, source_path, thumbnail_url, views, IFNULL(cta_text, ''), IFNULL(cta_hero_text, ''), IFNULL(cta_url, ''), IFNULL(cta_time_seconds, 0), IFNULL(cta_type, 'button'), IFNULL(player_autoplay, 0), IFNULL(player_muted, 0), IFNULL(player_controls, 1), IFNULL(player_start_seconds, 0) FROM videos WHERE user_id = ? ORDER BY created_at DESC", userEmail)
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

			// scan into NullStrings
			err := rows.Scan(&v.ID, &v.Status, &v.Title, &playlist, &v.SourcePath, &thumb, &v.Views, &v.CTAText, &v.CTAHeroText, &v.CTAURL, &v.CTATimeSeconds, &v.CTAType, &v.PlayerAutoplay, &v.PlayerMuted, &v.PlayerControls, &v.PlayerStartSeconds)
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

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/@") {
			http.Redirect(w, r, "/gallery", http.StatusSeeOther)
			return
		}

		username := strings.TrimSpace(strings.TrimPrefix(r.URL.Path, "/@"))
		if username == "" {
			http.Redirect(w, r, "/gallery", http.StatusSeeOther)
			return
		}

		lookupUsername := normalizeUsername(username, "")

		var creator ProfileData
		query := `
			SELECT
				u.email, 
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
			WHERE u.username = ?
			GROUP BY u.email, u.display_name, u.username, u.bio, u.website, u.instagram, u.profile_picture_url
		`
		err := db.QueryRow(query, lookupUsername).Scan(
			&creator.Email,
			&creator.DisplayName,
			&creator.Username,
			&creator.Bio,
			&creator.Website,
			&creator.Instagram,
			&creator.ProfilePictureURL,
			&creator.TotalVideos,
			&creator.TotalViews,
		)
		if err != nil {
			if err == sql.ErrNoRows {
				http.NotFound(w, r)
				return
			}
			log.Printf("Creator profile query error: %v", err)
			http.Error(w, "Unable to load creator page", http.StatusInternalServerError)
			return
		}

		videoRows, err := db.Query(`
			SELECT id, user_id, title, description, playlist, source_path, thumbnail_url, views, created_at, status
			FROM videos
			WHERE user_id = ?
			ORDER BY created_at DESC
		`, creator.Email)
		if err != nil {
			log.Printf("Creator videos query error: %v", err)
			http.Error(w, "Unable to load creator videos", http.StatusInternalServerError)
			return
		}
		defer videoRows.Close()

		var videos []VideoData
		for videoRows.Next() {
			var v VideoData
			var playlist, thumb sql.NullString

			if err := videoRows.Scan(
				&v.ID,
				&v.UserID,
				&v.Title,
				&v.Description,
				&playlist,
				&v.SourcePath,
				&thumb,
				&v.Views,
				&v.CreatedAt,
				&v.Status,
			); err != nil {
				log.Printf("Creator video scan error: %v", err)
				continue
			}

			if playlist.Valid {
				v.Playlist = playlist.String
			}
			if thumb.Valid {
				v.ThumbnailURL = thumb.String
			}

			videos = append(videos, v)
		}

		tmpl, err := template.ParseFiles("web/templates/creator.html")
		if err != nil {
			log.Printf("Creator template error: %v", err)
			http.Error(w, "Creator template not found", http.StatusInternalServerError)
			return
		}

		err = tmpl.Execute(w, CreatorPageData{
			Creator: creator,
			Videos:  videos,
		})
		if err != nil {
			log.Printf("Creator template execution error: %v", err)
		}
	})

	http.HandleFunc("/view/", func(w http.ResponseWriter, r *http.Request) {
		id := filepath.Base(r.URL.Path)
		isEmbed := r.URL.Query().Get("embed") == "true"
		renderVideoPage(db, w, r, id, isEmbed)
	})

	http.HandleFunc("/player/", func(w http.ResponseWriter, r *http.Request) {
		id := filepath.Base(r.URL.Path)
		renderVideoPage(db, w, r, id, true)
	})

	http.HandleFunc("/embed/", func(w http.ResponseWriter, r *http.Request) {
		id := filepath.Base(r.URL.Path)
		if id == "" {
			http.Redirect(w, r, "/gallery", http.StatusSeeOther)
			return
		}

		http.Redirect(w, r, "/player/"+id, http.StatusSeeOther)
	})

	http.HandleFunc("/f/", func(w http.ResponseWriter, r *http.Request) {
		funnelID := filepath.Base(r.URL.Path)
		if funnelID == "" {
			http.Redirect(w, r, "/gallery", http.StatusSeeOther)
			return
		}

		var firstStep FunnelStepData
		err := db.QueryRow(`
			SELECT id, funnel_id, step_type, IFNULL(video_id, ''), position, created_at
			FROM funnel_steps
			WHERE funnel_id = ?
			ORDER BY position ASC, created_at ASC
			LIMIT 1
		`, funnelID).Scan(
			&firstStep.ID,
			&firstStep.FunnelID,
			&firstStep.StepType,
			&firstStep.VideoID,
			&firstStep.Position,
			&firstStep.CreatedAt,
		)
		if err != nil {
			if err == sql.ErrNoRows {
				http.NotFound(w, r)
				return
			}
			log.Printf("Funnel first-step lookup error for %s: %v", funnelID, err)
			http.Error(w, "Unable to load funnel", http.StatusInternalServerError)
			return
		}

		switch firstStep.StepType {
		case "video":
			if strings.TrimSpace(firstStep.VideoID) == "" {
				http.Error(w, "Funnel video step is missing a video", http.StatusInternalServerError)
				return
			}

			target := fmt.Sprintf("/player/%s?funnel=%s&step=%s", firstStep.VideoID, funnelID, firstStep.ID)
			http.Redirect(w, r, target, http.StatusSeeOther)
			return
		default:
			log.Printf("Unsupported first funnel step type for %s: %s", funnelID, firstStep.StepType)
			http.Error(w, "Unsupported funnel step type", http.StatusInternalServerError)
			return
		}
	})

	// Secure proxy endpoint for direct video access without exposing S3 URL
	http.HandleFunc("/video-file/", func(w http.ResponseWriter, r *http.Request) {
		id := filepath.Base(r.URL.Path)
		if id == "" {
			http.Error(w, "missing video id", http.StatusBadRequest)
			return
		}

		var sourcePath string
		err := db.QueryRow("SELECT source_path FROM videos WHERE id = ?", id).Scan(&sourcePath)
		if err != nil {
			http.Error(w, "video not found", http.StatusNotFound)
			return
		}

		// Fetch the video from storage (S3) server-side so the client never sees the bucket URL
		resp, err := http.Get(sourcePath)
		if err != nil {
			log.Printf("Video proxy fetch error for %s: %v", id, err)
			http.Error(w, "unable to load video", http.StatusInternalServerError)
			return
		}
		defer resp.Body.Close()

		w.Header().Set("Content-Type", "video/mp4")
		w.Header().Set("Cache-Control", "public, max-age=86400")

		io.Copy(w, resp.Body)
	})

	http.HandleFunc("/delete/", func(w http.ResponseWriter, r *http.Request) {
		id := filepath.Base(r.URL.Path)
		storage.DeleteFromS3(id + "_processed.mp4")
		storage.DeleteFromS3(id + "_thumb.jpg")
		db.Exec("DELETE FROM videos WHERE id = ?", id)
		http.Redirect(w, r, "/gallery", 303)
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

	http.HandleFunc("/manage-cta/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Redirect(w, r, "/gallery", http.StatusSeeOther)
			return
		}

		userEmail := getLoggedInUser(r)
		if userEmail == "" {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}

		id := filepath.Base(r.URL.Path)
		if id == "" {
			http.Redirect(w, r, "/gallery", http.StatusSeeOther)
			return
		}

		if err := r.ParseForm(); err != nil {
			log.Printf("CTA form parse error for %s: %v", id, err)
			http.Redirect(w, r, "/gallery", http.StatusSeeOther)
			return
		}

		ctaText := strings.TrimSpace(r.FormValue("cta_text"))
		ctaHeroText := strings.TrimSpace(r.FormValue("cta_hero_text"))
		ctaURL := strings.TrimSpace(r.FormValue("cta_url"))
		ctaType := strings.TrimSpace(r.FormValue("cta_type"))
		if ctaType != "email" && ctaType != "email_gate" {
			ctaType = "button"
		}
		ctaTimeRaw := strings.TrimSpace(r.FormValue("cta_time_seconds"))
		ctaTimeSeconds := 0
		if ctaTimeRaw != "" {
			if _, err := fmt.Sscanf(ctaTimeRaw, "%d", &ctaTimeSeconds); err != nil || ctaTimeSeconds < 0 {
				log.Printf("Invalid CTA seconds for %s: %q", id, ctaTimeRaw)
				http.Redirect(w, r, "/gallery", http.StatusSeeOther)
				return
			}
		}

		if ctaType == "email" || ctaType == "email_gate" {
			if ctaText == "" {
				ctaText = ""
				ctaHeroText = ""
				ctaURL = ""
				ctaTimeSeconds = 0
				ctaType = "button"
			} else {
				ctaURL = "__email_capture__"
				if ctaType == "email_gate" {
					ctaTimeSeconds = 0
				}
			}
		} else {
			if ctaText == "" || ctaURL == "" {
				ctaText = ""
				ctaHeroText = ""
				ctaURL = ""
				ctaTimeSeconds = 0
				ctaType = "button"
			}
		}

		result, err := db.Exec(
			"UPDATE videos SET cta_text = ?, cta_hero_text = ?, cta_url = ?, cta_time_seconds = ?, cta_type = ? WHERE id = ? AND user_id = ?",
			ctaText,
			ctaHeroText,
			ctaURL,
			ctaTimeSeconds,
			ctaType,
			id,
			userEmail,
		)
		if err != nil {
			log.Printf("CTA update error for %s: %v", id, err)
			http.Redirect(w, r, "/gallery", http.StatusSeeOther)
			return
		}

		rowsAffected, _ := result.RowsAffected()
		if rowsAffected == 0 {
			log.Printf("CTA update skipped for %s: no matching video for user %s", id, userEmail)
		}

		http.Redirect(w, r, "/gallery", http.StatusSeeOther)
	})

	http.HandleFunc("/cta-timeline/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		userEmail := getLoggedInUser(r)
		if userEmail == "" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		id := filepath.Base(r.URL.Path)
		if id == "" {
			http.Error(w, "missing video id", http.StatusBadRequest)
			return
		}

		var ownerEmail string
		err := db.QueryRow("SELECT user_id FROM videos WHERE id = ?", id).Scan(&ownerEmail)
		if err != nil {
			http.Error(w, "video not found", http.StatusNotFound)
			return
		}
		if ownerEmail != userEmail {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}

		rows, err := db.Query(`
			SELECT id, video_id, cta_text, IFNULL(cta_hero_text, ''), cta_url, IFNULL(cta_type, 'button'), cta_time_seconds, created_at
			FROM video_ctas
			WHERE video_id = ?
			ORDER BY cta_time_seconds ASC, created_at ASC
		`, id)
		if err != nil {
			http.Error(w, "failed to load CTAs", http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		var ctas []VideoCTA
		for rows.Next() {
			var cta VideoCTA
			if err := rows.Scan(
				&cta.ID,
				&cta.VideoID,
				&cta.Text,
				&cta.HeroText,
				&cta.URL,
				&cta.CTAType,
				&cta.TimeSeconds,
				&cta.CreatedAt,
			); err != nil {
				log.Printf("CTA timeline scan error for %s: %v", id, err)
				continue
			}
			ctas = append(ctas, cta)
		}

		writeJSON(w, http.StatusOK, ctas)
	})

	http.HandleFunc("/manage-cta-timeline/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Redirect(w, r, "/gallery", http.StatusSeeOther)
			return
		}

		userEmail := getLoggedInUser(r)
		if userEmail == "" {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}

		id := filepath.Base(r.URL.Path)
		if id == "" {
			http.Redirect(w, r, "/gallery", http.StatusSeeOther)
			return
		}

		var ownerEmail string
		err := db.QueryRow("SELECT user_id FROM videos WHERE id = ?", id).Scan(&ownerEmail)
		if err != nil {
			log.Printf("CTA timeline owner lookup error for %s: %v", id, err)
			http.Redirect(w, r, "/gallery", http.StatusSeeOther)
			return
		}
		if ownerEmail != userEmail {
			http.Redirect(w, r, "/gallery", http.StatusSeeOther)
			return
		}

		if err := r.ParseForm(); err != nil {
			log.Printf("CTA timeline parse error for %s: %v", id, err)
			http.Redirect(w, r, "/gallery", http.StatusSeeOther)
			return
		}

		texts := r.Form["cta_text[]"]
		heroTexts := r.Form["cta_hero_text[]"]
		urls := r.Form["cta_url[]"]
		types := r.Form["cta_type[]"]
		times := r.Form["cta_time_seconds[]"]

		tx, err := db.Begin()
		if err != nil {
			log.Printf("CTA timeline tx begin error for %s: %v", id, err)
			http.Redirect(w, r, "/gallery", http.StatusSeeOther)
			return
		}
		defer tx.Rollback()

		if _, err := tx.Exec("DELETE FROM video_ctas WHERE video_id = ?", id); err != nil {
			log.Printf("CTA timeline delete error for %s: %v", id, err)
			http.Redirect(w, r, "/gallery", http.StatusSeeOther)
			return
		}

		for i := 0; i < len(texts) && i < len(heroTexts) && i < len(urls) && i < len(types) && i < len(times); i++ {
			text := strings.TrimSpace(texts[i])
			heroText := strings.TrimSpace(heroTexts[i])
			url := strings.TrimSpace(urls[i])
			ctaType := strings.TrimSpace(types[i])
			timeRaw := strings.TrimSpace(times[i])

			if ctaType != "email" && ctaType != "email_gate" {
				ctaType = "button"
			}

			if text == "" || timeRaw == "" {
				continue
			}

			var timeSeconds int
			if _, err := fmt.Sscanf(timeRaw, "%d", &timeSeconds); err != nil || timeSeconds < 0 {
				continue
			}

			if ctaType == "button" {
				if url == "" {
					continue
				}
			} else {
				url = "__email_capture__"
				if ctaType == "email_gate" {
					timeSeconds = 0
				}
			}

			ctaID := fmt.Sprintf("cta-%d-%d", time.Now().UnixNano(), i)

			if _, err := tx.Exec(`
				INSERT INTO video_ctas (id, video_id, cta_text, cta_hero_text, cta_url, cta_type, cta_time_seconds)
				VALUES (?, ?, ?, ?, ?, ?, ?)
			`, ctaID, id, text, heroText, url, ctaType, timeSeconds); err != nil {
				log.Printf("CTA timeline insert error for %s: %v", id, err)
				http.Redirect(w, r, "/gallery", http.StatusSeeOther)
				return
			}
		}

		if err := tx.Commit(); err != nil {
			log.Printf("CTA timeline commit error for %s: %v", id, err)
			http.Redirect(w, r, "/gallery", http.StatusSeeOther)
			return
		}

		http.Redirect(w, r, "/gallery", http.StatusSeeOther)
	})

	http.HandleFunc("/manage-player/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Redirect(w, r, "/gallery", http.StatusSeeOther)
			return
		}

		userEmail := getLoggedInUser(r)
		if userEmail == "" {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}

		id := filepath.Base(r.URL.Path)
		if id == "" {
			http.Redirect(w, r, "/gallery", http.StatusSeeOther)
			return
		}

		if err := r.ParseForm(); err != nil {
			log.Printf("Player settings form parse error for %s: %v", id, err)
			http.Redirect(w, r, "/gallery", http.StatusSeeOther)
			return
		}

		playerAutoplay := r.FormValue("player_autoplay") == "1"
		playerMuted := r.FormValue("player_muted") == "1"
		playerControls := r.FormValue("player_controls") == "1"
		playerStartRaw := strings.TrimSpace(r.FormValue("player_start_seconds"))
		playerStartSeconds := 0
		if playerStartRaw != "" {
			if _, err := fmt.Sscanf(playerStartRaw, "%d", &playerStartSeconds); err != nil || playerStartSeconds < 0 {
				log.Printf("Invalid player start seconds for %s: %q", id, playerStartRaw)
				http.Redirect(w, r, "/gallery", http.StatusSeeOther)
				return
			}
		}

		if playerAutoplay {
			playerMuted = true
		}

		result, err := db.Exec(
			"UPDATE videos SET player_autoplay = ?, player_muted = ?, player_controls = ?, player_start_seconds = ? WHERE id = ? AND user_id = ?",
			playerAutoplay,
			playerMuted,
			playerControls,
			playerStartSeconds,
			id,
			userEmail,
		)
		if err != nil {
			log.Printf("Player settings update error for %s: %v", id, err)
			http.Redirect(w, r, "/gallery", http.StatusSeeOther)
			return
		}

		rowsAffected, _ := result.RowsAffected()
		if rowsAffected == 0 {
			log.Printf("Player settings update skipped for %s: no matching video for user %s", id, userEmail)
		}

		http.Redirect(w, r, "/gallery", http.StatusSeeOther)
	})

	http.HandleFunc("/capture-lead/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		id := filepath.Base(r.URL.Path)
		if id == "" {
			http.Error(w, "missing video id", http.StatusBadRequest)
			return
		}

		name := strings.TrimSpace(r.FormValue("name"))
		email := strings.TrimSpace(r.FormValue("email"))
		if email == "" || !strings.Contains(email, "@") {
			http.Error(w, "invalid email", http.StatusBadRequest)
			return
		}

		ctaID := strings.TrimSpace(r.FormValue("cta_id"))
		ctaType := strings.TrimSpace(r.FormValue("cta_type"))
		ctaHeroText := strings.TrimSpace(r.FormValue("cta_hero_text"))
		ctaTimeSeconds := 0
		ctaTimeRaw := strings.TrimSpace(r.FormValue("cta_time_seconds"))
		if ctaTimeRaw != "" {
			if _, err := fmt.Sscanf(ctaTimeRaw, "%d", &ctaTimeSeconds); err != nil || ctaTimeSeconds < 0 {
				ctaTimeSeconds = 0
			}
		}

		_, err := db.Exec(`
			INSERT INTO leads (
				video_id,
				email,
				name,
				cta_id,
				cta_type,
				cta_time_seconds,
				cta_hero_text
			) VALUES (?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(video_id, email) DO UPDATE SET
				name = CASE
					WHEN excluded.name != '' THEN excluded.name
					ELSE leads.name
				END,
				cta_id = excluded.cta_id,
				cta_type = excluded.cta_type,
				cta_time_seconds = excluded.cta_time_seconds,
				cta_hero_text = excluded.cta_hero_text
		`, id, email, name, ctaID, ctaType, ctaTimeSeconds, ctaHeroText)
		if err != nil {
			log.Printf("Lead capture error for %s: %v", id, err)
			http.Error(w, "failed to capture lead", http.StatusInternalServerError)
			return
		}

		var videoTitle string
		var ownerEmail string
		var webhookURL string

		err = db.QueryRow(`
			SELECT v.title, v.user_id, IFNULL(u.webhook_url, '')
			FROM videos v
			LEFT JOIN users u ON u.email = v.user_id
			WHERE v.id = ?
		`, id).Scan(&videoTitle, &ownerEmail, &webhookURL)
		if err != nil {
			log.Printf("Webhook lookup error for %s: %v", id, err)
			w.WriteHeader(http.StatusNoContent)
			return
		}

		payload := webhookPayload{
			Event:      "lead_captured",
			VideoID:    id,
			VideoTitle: videoTitle,
			UserEmail:  ownerEmail,
			Name:       name,
			Email:      email,
			CapturedAt: time.Now().UTC().Format(time.RFC3339),
		}

		go sendWebHook(webhookURL, payload)

		w.WriteHeader(http.StatusNoContent)
	})

	http.HandleFunc("/cta-impression/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		videoID := filepath.Base(r.URL.Path)
		if videoID == "" {
			http.Error(w, "missing video id", http.StatusBadRequest)
			return
		}

		var payload struct {
			VideoID        string `json:"videoId"`
			CTAID          string `json:"ctaId"`
			CTAType        string `json:"ctaType"`
			CTATimeSeconds int    `json:"ctaTimeSeconds"`
			CTAHeroText    string `json:"ctaHeroText"`
			CTAText        string `json:"ctaText"`
		}

		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, "invalid json body", http.StatusBadRequest)
			return
		}

		ctaID := strings.TrimSpace(payload.CTAID)
		if ctaID == "" {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		result, err := db.Exec(`
			UPDATE video_ctas
			SET impression_count = IFNULL(impression_count, 0) + 1
			WHERE id = ? AND video_id = ?
		`, ctaID, videoID)
		if err != nil {
			log.Printf("CTA impression update error for video %s cta %s: %v", videoID, ctaID, err)
			http.Error(w, "failed to track cta impression", http.StatusInternalServerError)
			return
		}

		rowsAffected, err := result.RowsAffected()
		if err != nil {
			log.Printf("CTA impression rows affected error for video %s cta %s: %v", videoID, ctaID, err)
			http.Error(w, "failed to track cta impression", http.StatusInternalServerError)
			return
		}

		if rowsAffected == 0 {
			log.Printf("CTA impression skipped: no matching CTA for video %s cta %s", videoID, ctaID)
			w.WriteHeader(http.StatusNoContent)
			return
		}

		w.WriteHeader(http.StatusNoContent)
	})

	http.HandleFunc("/cta-click/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		id := filepath.Base(r.URL.Path)
		if id == "" {
			http.Error(w, "missing video id", http.StatusBadRequest)
			return
		}

		_, err := db.Exec("UPDATE videos SET cta_clicks = cta_clicks + 1 WHERE id = ?", id)
		if err != nil {
			log.Printf("CTA click update error for %s: %v", id, err)
			http.Error(w, "failed to record cta click", http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusNoContent)
	})

	http.HandleFunc("/share-click/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		id := filepath.Base(r.URL.Path)
		if id == "" {
			http.Error(w, "missing video id", http.StatusBadRequest)
			return
		}

		_, err := db.Exec("UPDATE videos SET share_count = share_count + 1 WHERE id = ?", id)
		if err != nil {
			log.Printf("Share click update error for %s: %v", id, err)
			http.Error(w, "failed to record share click", http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusNoContent)
	})

	http.HandleFunc("/download-click/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		id := filepath.Base(r.URL.Path)
		if id == "" {
			http.Error(w, "missing video id", http.StatusBadRequest)
			return
		}

		_, err := db.Exec("UPDATE videos SET download_count = download_count + 1 WHERE id = ?", id)
		if err != nil {
			log.Printf("Download click update error for %s: %v", id, err)
			http.Error(w, "failed to record download click", http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusNoContent)
	})

	http.HandleFunc("/track", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var payload retentionPayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, "invalid payload", http.StatusBadRequest)
			return
		}

		payload.VideoID = strings.TrimSpace(payload.VideoID)
		if payload.VideoID == "" {
			http.Error(w, "missing video_id", http.StatusBadRequest)
			return
		}

		if payload.Second < 0 {
			http.Error(w, "invalid second", http.StatusBadRequest)
			return
		}

		_, err := db.Exec(`
			INSERT INTO video_retention (video_id, second, views)
			VALUES (?, ?, 1)
			ON CONFLICT(video_id, second)
			DO UPDATE SET views = views + 1
		`, payload.VideoID, payload.Second)
		if err != nil {
			log.Printf("Retention tracking error for %s at second %d: %v", payload.VideoID, payload.Second, err)
			http.Error(w, "failed to track retention", http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusNoContent)
	})

	http.HandleFunc("/stats/", func(w http.ResponseWriter, r *http.Request) {
		userEmail := getLoggedInUser(r)
		if userEmail == "" {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}

		path := strings.TrimPrefix(r.URL.Path, "/stats/")
		isExport := strings.HasSuffix(path, "/export")
		id := strings.Trim(path, "/")
		if isExport {
			id = strings.TrimSuffix(id, "/export")
			id = strings.Trim(id, "/")
		}
		if id == "" {
			http.Redirect(w, r, "/gallery", http.StatusSeeOther)
			return
		}

		var v VideoData
		var thumbnail, playlist sql.NullString
		err := db.QueryRow(`
			SELECT 
			id, 
			user_id, 
			title, 
			description, 
			playlist, 
			source_path, 
			thumbnail_url, 
			views, 
			created_at, 
			status, 
			IFNULL(cta_text, ''), 
			IFNULL(cta_hero_text, ''),
			IFNULL(cta_url, ''), 
			IFNULL(cta_time_seconds, 0), 
			IFNULL(cta_type, 'button'),
			IFNULL(cta_clicks, 0),
			IFNULL(share_count, 0),
			IFNULL(download_count, 0)
			FROM videos
			WHERE id = ? AND user_id = ?
		`, id, userEmail).Scan(
			&v.ID,
			&v.UserID,
			&v.Title,
			&v.Description,
			&playlist,
			&v.SourcePath,
			&thumbnail,
			&v.Views,
			&v.CreatedAt,
			&v.Status,
			&v.CTAText,
			&v.CTAHeroText,
			&v.CTAURL,
			&v.CTATimeSeconds,
			&v.CTAType,
			&v.CTAClicks,
			&v.ShareCount,
			&v.DownloadCount,
		)
		if err != nil {
			if err == sql.ErrNoRows {
				http.Redirect(w, r, "/gallery", http.StatusSeeOther)
				return
			}
			log.Printf("Stats query error for %s: %v", id, err)
			http.Error(w, "Unable to load video stats", http.StatusInternalServerError)
			return
		}

		if playlist.Valid {
			v.Playlist = playlist.String
		}
		if thumbnail.Valid {
			v.ThumbnailURL = thumbnail.String
		}

		shareCount := v.ShareCount
		downloadCount := v.DownloadCount
		ctr := 0.0
		if v.Views > 0 {
			ctr = (float64(v.CTAClicks) / float64(v.Views)) * 100
		}

		leadRows, err := db.Query("SELECT IFNULL(name, ''), email, IFNULL(cta_id, ''), IFNULL(cta_type, ''), IFNULL(cta_time_seconds, 0), IFNULL(cta_hero_text, ''), created_at FROM leads WHERE video_id = ? ORDER BY created_at DESC", id)
		if err != nil {
			log.Printf("Lead query error for %s: %v", id, err)
			http.Error(w, "unable to load video leads", http.StatusInternalServerError)
			return
		}
		defer leadRows.Close()

		var leads []LeadData
		for leadRows.Next() {
			var lead LeadData
			if err := leadRows.Scan(&lead.Name, &lead.Email, &lead.CTAID, &lead.CTAType, &lead.CTATimeSeconds, &lead.CTAHeroText, &lead.CreatedAt); err != nil {
				log.Printf("Lead scan error for %s: %v", id, err)
				continue
			}
			leads = append(leads, lead)
		}

		var ctaPerformance []CTAPerformanceData

		ctaRows, err := db.Query(`
			SELECT
				vc.id,
				vc.cta_text,
				IFNULL(vc.cta_hero_text, ''),
				IFNULL(vc.cta_type, 'button'),
				IFNULL(vc.cta_time_seconds, 0),
				IFNULL(vc.impression_count, 0),
				COUNT(l.id) AS lead_count,
				COUNT(DISTINCT l.email) AS unique_lead_count
			FROM video_ctas vc
			LEFT JOIN leads l ON l.video_id = vc.video_id AND l.cta_id = vc.id
			WHERE vc.video_id = ?
			GROUP BY vc.id, vc.cta_text, vc.cta_hero_text, vc.cta_type, vc.cta_time_seconds, vc.impression_count
			ORDER BY vc.cta_time_seconds ASC, vc.created_at ASC
		`, id)
		if err != nil {
			log.Printf("CTA performance query error for %s: %v", id, err)
			http.Error(w, "unable to load cta analytics", http.StatusInternalServerError)
			return
		}
		defer ctaRows.Close()

		for ctaRows.Next() {
			var item CTAPerformanceData
			if err := ctaRows.Scan(
				&item.CTAID,
				&item.Text,
				&item.HeroText,
				&item.CTAType,
				&item.TimeSeconds,
				&item.ImpressionCount,
				&item.LeadCount,
				&item.UniqueLeadCount,
			); err != nil {
				log.Printf("CTA performance scan error for %s: %v", id, err)
				continue
			}
			if item.ImpressionCount > 0 {
				item.ConversionRate = (float64(item.LeadCount) / float64(item.ImpressionCount)) * 100
			}
			ctaPerformance = append(ctaPerformance, item)
		}
		legacyLeadCount := 0
		legacyUniqueLeadCount := 0
		err = db.QueryRow(`
			SELECT COUNT(id), COUNT(DISTINCT email)
			FROM leads
			WHERE video_id = ? AND (cta_id = '' OR cta_id IS NULL)
		`, id).Scan(&legacyLeadCount, &legacyUniqueLeadCount)
		if err != nil {
			log.Printf("Legacy CTA performance query error for %s: %v", id, err)
			http.Error(w, "unable to load cta analytics", http.StatusInternalServerError)
			return
		}

		if strings.TrimSpace(v.CTAText) != "" || strings.TrimSpace(v.CTAHeroText) != "" || strings.TrimSpace(v.CTAType) != "button" || legacyLeadCount > 0 {
			ctaPerformance = append([]CTAPerformanceData{{
				CTAID:           "",
				Text:            v.CTAText,
				HeroText:        v.CTAHeroText,
				CTAType:         v.CTAType,
				TimeSeconds:     v.CTATimeSeconds,
				ImpressionCount: 0,
				LeadCount:       legacyLeadCount,
				UniqueLeadCount: legacyUniqueLeadCount,
				ConversionRate:  0,
			}}, ctaPerformance...)
		}

		retentionRows, err := db.Query("SELECT second, views FROM video_retention WHERE video_id = ? ORDER BY second ASC", id)
		if err != nil {
			log.Printf("Retention query error for %s: %v", id, err)
			http.Error(w, "unable to load video retention", http.StatusInternalServerError)
			return
		}
		defer retentionRows.Close()

		var retentionPoints []RetentionPoint
		retentionPeak := 0
		retentionMaxTime := 0
		for retentionRows.Next() {
			var point RetentionPoint
			if err := retentionRows.Scan(&point.Second, &point.Views); err != nil {
				log.Printf("Retention scan error for %s: %v", id, err)
				continue
			}
			retentionPoints = append(retentionPoints, point)
			if point.Views > retentionPeak {
				retentionPeak = point.Views
			}
			if point.Second > retentionMaxTime {
				retentionMaxTime = point.Second
			}
		}

		retentionJSONBytes, err := json.Marshal(retentionPoints)
		if err != nil {
			log.Printf("Retention JSON marshal error for %s: %v", id, err)
			retentionJSONBytes = []byte("[]")
		}
		dropoffHeatmap := buildDropoffHeatmap(retentionPoints)

		if isExport {
			filename := fmt.Sprintf("vidify-leads-%s.csv", id)
			w.Header().Set("Content-Type", "text/csv")
			w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))

			csvWriter := csv.NewWriter(w)
			defer csvWriter.Flush()

			if err := csvWriter.Write([]string{"Name", "Email", "CTA ID", "CTA Type", "CTA Time (s)", "CTA Hero Text", "Captured At"}); err != nil {
				log.Printf("CSV header write error for %s: %v", id, err)
				http.Error(w, "Unable to export leads", http.StatusInternalServerError)
				return
			}

			for _, lead := range leads {
				if err := csvWriter.Write([]string{
					lead.Name,
					lead.Email,
					lead.CTAID,
					lead.CTAType,
					fmt.Sprintf("%d", lead.CTATimeSeconds),
					lead.CTAHeroText,
					lead.CreatedAt.Format("2006-01-02 15:04:05"),
				}); err != nil {
					log.Printf("CSV row write error for %s: %v", id, err)
					http.Error(w, "Unable to export leads", http.StatusInternalServerError)
					return
				}
			}

			return
		}
		tmpl, err := template.ParseFiles("web/templates/stats.html")
		if err != nil {
			log.Printf("Stats template error: %v", err)
			http.Error(w, "Stats template not found", http.StatusInternalServerError)
			return
		}

		data := StatsPageData{
			Video:            v,
			UserEmail:        userEmail,
			ShareCount:       shareCount,
			DownloadCount:    downloadCount,
			CTR:              ctr,
			LeadCount:        len(leads),
			Leads:            leads,
			CTAPerformance:   ctaPerformance,
			RetentionJSON:    template.JS(retentionJSONBytes),
			DropoffHeatmap:   dropoffHeatmap,
			RetentionPeak:    retentionPeak,
			RetentionMaxTime: retentionMaxTime,
		}

		if err := tmpl.Execute(w, data); err != nil {
			log.Printf("Stats template execution error: %v", err)
		}
	})

	http.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("web/static"))))

	fmt.Println("Vidify web server running on http://localhost:8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
