package main

import (
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/JerryG0311/Vidify/internal/pubsub"
	"github.com/JerryG0311/Vidify/internal/routing"
	"github.com/JerryG0311/Vidify/internal/storage"
	amqp "github.com/rabbitmq/amqp091-go"
)

func main() {

	var err error
	// -- Database  --

	db, err := sql.Open("sqlite3", "vidify.db")
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	query := `CREATE TABLE IF NOT EXISTS videos (
		id TEXT PRIMARY KEY, 
		user_id TEXT,
		status TEXT,
		source_path TEXT,
		thumbnail_url TEXT,
		title TEXT,
		description TEXT,
		created_at DATETIME
	
	)`
	_, err = db.Exec(query)
	if err != nil {
		log.Fatalf("Failed to create table: %v", err)
	}

	// 1. Establishing connection

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
		fmt.Printf("Failed: %v. Retrying in 2s...\n", err)
		time.Sleep(2 * time.Second)
	}

	if err != nil {
		log.Fatalf("Could not connect to RabbitMQ after retries: %v", err)
	}
	defer conn.Close()

	fmt.Println("Vidify API started. Connecting to RabbitMQ...")

	// 2. Creating a channel to declare Exchange
	ch, err := conn.Channel()
	if err != nil {
		log.Fatalf("Failed to open a channel: %v", err)
	}
	defer ch.Close()

	// 3. Declare video topic exchange
	err = ch.ExchangeDeclare(
		routing.ExchangeVideoTopic, // name
		"topic",                    // type
		true,                       // durable
		false,                      // auto-delete
		false,                      // internal
		false,                      // no-wait
		nil,                        // arguments
	)
	if err != nil {
		log.Fatalf("Failed to declare exchange: %v", err)
	}

	// 3.5a Declare Dead Letter Exchange
	err = ch.ExchangeDeclare(
		routing.ExchangeVideoDLX,
		"fanout",
		true,  // durable
		false, // auto-deleted
		false,
		false,
		nil,
	)

	// 3.5b Declare the "Failed Jobs" queue
	_, err = ch.QueueDeclare(
		routing.VideoDLQueue,
		true,  // durable
		false, // auto-delete
		false, // exclusive
		false,
		nil,
	)

	// 3.5c Bind the Failed Queue to the DLX
	err = ch.QueueBind(routing.VideoDLQueue, "", routing.ExchangeVideoDLX, false, nil)

	// ---- New Web Server Code ---

	// 1. Parse the file from the request ("video" is the key used in the curl command)

	http.HandleFunc("/upload", func(w http.ResponseWriter, r *http.Request) {

		if r.Method != http.MethodPost {
			http.Error(w, "Only POST allowed", http.StatusMethodNotAllowed)
			return
		}

		title := r.FormValue("title")
		description := r.FormValue("description")

		file, header, err := r.FormFile("video")
		if err != nil {
			http.Error(w, "Failed to get file", http.StatusBadRequest)
			return
		}
		defer file.Close()

		if title == "" {
			title = header.Filename
		}

		// Upload directly to S3 (No local 'data' folder needed)
		log.Printf("Starting S3 upload for file: %s to bucket: %s", header.Filename, os.Getenv("S3_BUCKET_NAME"))
		s3URL, err := storage.UploadToS3(header.Filename, file)
		if err != nil {
			log.Printf("S3 Upload Error: %v", err)
			http.Error(w, "Failed to upload to cloud", http.StatusInternalServerError)
			return
		}

		// 4. Creating the job
		job := routing.VideoJob{
			ID:           fmt.Sprintf("vid-%d", time.Now().Unix()),
			SourcePath:   s3URL, // Now pointing to S3
			TargetFormat: "mp4",
			UserID:       "jerry_g",
			CreatedAt:    time.Now(),
		}

		_, err = db.Exec(
			"INSERT INTO videos (id, user_id, status, source_path, title, description, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)",
			job.ID, job.UserID, "PENDING", job.SourcePath, title, description, job.CreatedAt,
		)

		// 5. Publish to RabbitMQ
		err = pubsub.PublishJSON(ch, routing.ExchangeVideoTopic, routing.VideoUploadKey, job)
		if err != nil {
			log.Printf("Failed to publish: %v", err)
			http.Error(w, "Failed to queue job", http.StatusInternalServerError)
			return
		}

		http.Redirect(w, r, "/gallery", http.StatusSeeOther)
		fmt.Printf(" [API] Log: Received %s, published job%s\n", header.Filename, job.ID)

	})

	http.HandleFunc("/status/", func(w http.ResponseWriter, r *http.Request) {
		id := filepath.Base(r.URL.Path)

		var status string
		err := db.QueryRow("SELECT status FROM videos WHERE id = ?", id).Scan(&status)
		if err != nil {
			http.Error(w, "Video ID not found in database", http.StatusNotFound)
			return
		}

		fmt.Fprintf(w, "Video ID: %s\nStatus: %s", id, status)
	})

	http.Handle("/data/", http.StripPrefix("/data/", http.FileServer(http.Dir("./data"))))

	http.HandleFunc("/gallery", func(w http.ResponseWriter, r *http.Request) {
		rows, err := db.Query("SELECT id, status, title, source_path, thumbnail_url FROM videos ORDER BY created_at DESC")
		if err != nil {
			http.Error(w, "Failed to query videos", http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		// Professional Header & Styling
		fmt.Fprint(w, `
        <html>
        <head>
            <style>
                body { font-family: sans-serif; margin: 40px; background: #f4f7f6; }
                table { width: 100%; border-collapse: collapse; background: white; border-radius: 8px; overflow: hidden; box-shadow: 0 4px 6px rgba(0,0,0,0.1); }
                th, td { padding: 15px; text-align: left; border-bottom: 1px solid #eee; }
                th { background: #00adef; color: white; }
                img { border-radius: 4px; background: #ddd; object-fit: cover; }
                .btn { text-decoration: none; background: #00adef; color: white; padding: 8px 16px; border-radius: 4px; font-weight: bold; }
                .status { font-weight: bold; text-transform: uppercase; font-size: 0.8em; padding: 4px 8px; border-radius: 12px; }
                .completed { background: #e6fffa; color: #2c7a7b; }
                .pending { background: #fffaf0; color: #9c4221; }
            </style>
        </head>
        <body>
            <h1>Vidify Library</h1>
            <table>
                <thead>
                    <tr>
                        <th>Preview</th>
                        <th>Title</th>
                        <th>Status</th>
                        <th>Action</th>
                    </tr>
                </thead>
                <tbody>`)

		for rows.Next() {
			var id, status, title, SourcePath, thumbURL string
			rows.Scan(&id, &status, &title, &SourcePath, &thumbURL)

			if thumbURL == "" {
				thumbURL = fmt.Sprintf("/data/%s_thumb.jpg", id)
			}
			statusClass := "pending"
			if status == "COMPLETED" {
				statusClass = "completed"
			}

			displayAction := "---"
			if status == "COMPLETED" {
				displayAction = fmt.Sprintf("<a class='btn' href='/view/%s'>Watch</a>", id)
			}

			deleteAction := fmt.Sprintf(`
				<form action="/delete/%s" method="POST" style="display:inline;" onsubmit="return confirm('Delete this video?');">
					<button type="submit" style="background:#ff4d4d; color:white; border:none; padding:8px 12px; border-radius:4px; cursor:pointer; font-weight:bold;">Delete</button>
				</form>`, id)

			// We use exactly 6 variables for the 6 %s placeholders below
			fmt.Fprintf(w, `
						<tr>
				<td><img src="%s" width="160" height="90" onerror="this.src='https://via.placeholder.com/160x90?text=No+Preview'"></td>
				<td>
					<strong style="font-size: 1.2em;">%s</strong><br>
					<a href="/edit/%s" style="font-size: 0.8em; color: #00adef; text-decoration: none;">Edit Title</a>
				</td>
				<td><span class="status %s">%s</span></td>
				<td>%s %s</td> 
			</tr>`,
				thumbURL, title, id, statusClass, status, displayAction, deleteAction)
		}

		fmt.Fprint(w, "</tbody></table></body></html>")
	})

	http.HandleFunc("/view/", func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Path[len("/view/"):]
		if id == "" {
			http.Redirect(w, r, "/gallery", http.StatusSeeOther)
			return
		}
		var title, s3URL string

		// Get the S3 URL from the DB
		err := db.QueryRow("SELECT title, source_path FROM videos WHERE id = ?", id).Scan(&title, &s3URL)
		if err != nil {
			log.Printf("View Error: %v", err)
			http.Error(w, "Video not found", http.StatusNotFound)
			return
		}

		// Vifeo Player Page
		fmt.Fprintf(w, `
		<html>
		<head>
			<title>Vidify - %s</title>
			<style>
				body { background: #111; color: white; font-family: sans-serif; text-align: center; padding: 50px; }
				video { width: 80%%; max-width: 1000px; border: 3px solid #00adef; border-radius: 12px; }
				.nav { margin-bottom: 20px; }
				a { color: #00adef; text-decoration: none; font-weight: bold; }
			</style>
		</head>
		<body>
			<div class="nav"><a href="/gallery">← Back to Gallery</a></div>
			<h1>%s</h1>
			<video controls autoplay>
				<source src="%s" type="video/mp4">
			</video>
		</body>
		</html>`, title, title, s3URL)
	})

	http.HandleFunc("/delete/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		id := filepath.Base(r.URL.Path)

		_, err := db.Exec("DELETE FROM videos WHERE id = ?", id)
		if err != nil {
			log.Printf("DB Delete Error: %v", err)
			http.Error(w, "Failed to delete record", http.StatusInternalServerError)
			return
		}

		filesToDelete := []string{
			filepath.Join("data", id+"_processed.mp4"),
			filepath.Join("data", id+"_thumb.jpg"),
		}

		for _, f := range filesToDelete {
			os.Remove(f)
		}

		http.Redirect(w, r, "/gallery", http.StatusSeeOther)
	})

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `
			<html>
			<head>
				<style>
					body { font-family: sans-serif; margin: 40px; background: #f4f7f6; text-align: center; }
					.card { background: white; padding: 30px; border-radius: 8px; display: inline-block; box-shadow: 0 4px 6px rgba(0,0,0,0.1); width: 400px; }
					input, textarea { width: 100%; margin-bottom: 15px; padding: 10px; border: 1px solid #ddd; border-radius: 4px; box-sizing: border-box; }
					button { background: #00adef; color: white; border: none; padding: 12px 20px; border-radius: 4px; cursor: pointer; font-weight: bold; width: 100%; }
				</style>
			</head>
			<body>
				<h1>🚀 Upload to Vidify</h1>
				<div class="card">
					<form action="/upload" method="POST" enctype="multipart/form-data">
						<input type="text" name="title" placeholder="Video Title" required>
						<textarea name="description" placeholder="Description (optional)" rows="3"></textarea>
						<input type="file" name="video" accept="video/*" required>
						<button type="submit">Publish to Library</button>
					</form>
					<br><a href="/gallery">View Your Gallery →</a>
				</div>
			</body></html>
		`)
	})

	http.HandleFunc("/edit/", func(w http.ResponseWriter, r *http.Request) {
		id := filepath.Base(r.URL.Path)

		if r.Method == http.MethodPost {
			newTitle := r.FormValue("title")
			_, err := db.Exec("UPDATE videos SET title = ? WHERE id = ?", newTitle, id)
			if err != nil {
				http.Error(w, "Failed to update", http.StatusInternalServerError)
				return
			}
			http.Redirect(w, r, "/gallery", http.StatusSeeOther)
			return
		}

		var currentTitle string
		db.QueryRow("SELECT title FROM videos WHERE id = ?", id).Scan(&currentTitle)

		fmt.Fprintf(w, `
			<html><body>
				<h1>Edit Video Title</h1>
				<form method="POST">
					<input type="text" name="title" value="%s" style="padding:10px; width:300px;">
					<button type="submit" style="padding:10px; background:#00adef; color:white; border:none;">Update Title</button>
				</form>
				<br><a href="/gallery">Cancel</a>
			</body></html>
		`, currentTitle)
	})

	fmt.Println("Vidify web server running on http://localhost:8080")
	log.Fatal(http.ListenAndServe(":8080", nil))

}
