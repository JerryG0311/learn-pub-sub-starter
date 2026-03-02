package main

import (
	"database/sql"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/JerryG0311/Vidify/internal/pubsub"
	"github.com/JerryG0311/Vidify/internal/routing"
	amqp "github.com/rabbitmq/amqp091-go"
)

func main() {

	// -- Database Setup --

	db, err := sql.Open("sqlite3", "vidify.db")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	statement, _ := db.Prepare(`CREATE TABLE IF NOT EXISTS videos (
		id TEXT PRIMARY KEY, 
		user_id TEXT,
		status TEXT,
		source_path TEXT,
		created_at DATETIME
	
	)`)
	statement.Exec()

	// 1. Establishing connection

	connString := "amqp://guest@localhost:5672/"
	conn, err := amqp.Dial(connString)
	if err != nil {
		log.Fatalf("Failed to connect to RabbitMQ: %v", err)
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

		file, header, err := r.FormFile("video")
		if err != nil {
			http.Error(w, "Failed to get file", http.StatusBadRequest)
			return
		}
		defer file.Close()

		// 2. Creating the 'data' directory if it doesn't already exist
		os.MkdirAll("data", os.ModePerm)

		// 3. Saving the file locally
		dstPath := filepath.Join("data", header.Filename)
		dst, err := os.Create(dstPath)
		if err != nil {
			http.Error(w, "Failed to save file", http.StatusInternalServerError)
			return
		}
		defer dst.Close()
		io.Copy(dst, file)

		// 4. Creating the job
		job := routing.VideoJob{
			ID:           fmt.Sprintf("vid-%d", time.Now().Unix()),
			SourcePath:   dstPath,
			TargetFormat: "mp4",
			UserID:       "jerry_g",
			CreatedAt:    time.Now(),
		}

		_, err = db.Exec(
			"INSERT INTO videos (id, user_id, status, source_path, created_at) VALUES (?, ?, ?, ?, ?)",
			job.ID, job.UserID, "PENDING", job.SourcePath, job.CreatedAt,
		)

		// 5. Publish to RabbitMQ
		err = pubsub.PublishJSON(ch, routing.ExchangeVideoTopic, routing.VideoUploadKey, job)
		if err != nil {
			log.Printf("Failed to publish: %v", err)
			http.Error(w, "Failed to queue job", http.StatusInternalServerError)
			return
		}

		fmt.Fprintf(w, "Upload successful! Job %s created for %s", job.ID, header.Filename)
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
		rows, err := db.Query("SELECT id, status, source_path FROM videos ORDER BY created_at DESC")
		if err != nil {
			http.Error(w, "failed to query videos", http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		// Start building simple HTML table
		fmt.Fprint(w, "<html><body><h1>Vidify Video Gallery</h1><table border='1'>")
		fmt.Fprint(w, "<tr><th>ID</th><th>Status</th><th>Action</th></tr>")

		for rows.Next() {
			var id, status, SourcePath string
			rows.Scan(&id, &status, &SourcePath)

			displayAction := "Processing..."
			if status == "COMPLETED" {
				// Link to the processsed file now being served via the /data/ route
				videoURL := fmt.Sprintf("/data/%s_processed.mp4", id)
				displayAction = fmt.Sprintf("<a href='%s'>▶️ Watch</a>", videoURL)
			}

			fmt.Fprintf(w, "<tr><td>%s</td><td>%s</td><td>%s</td></tr>", id, status, displayAction)
		}

		fmt.Fprint(w, "</table></body></html>")
	})

	fmt.Println("Vidify web server running on http://localhost:8080")
	log.Fatal(http.ListenAndServe(":8080", nil))

}
