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

		fmt.Fprint(w, `
			<html>
			<head>
				<style>
					body { font-family: 'Helvetica Neue', Helvetica, Arial, sans-serif; margin: 40px; background: #f0f2f5; color: #1a1a1a; }
					.header-flex { display: flex; justify-content: space-between; align-items: center; margin-bottom: 30px; }
					
					/* View Toggle Buttons */
					.view-btn { background: #fff; border: 1px solid #ddd; padding: 8px 16px; cursor: pointer; border-radius: 6px; font-weight: bold; transition: 0.2s; }
					.view-btn.active { background: #00adef; color: white; border-color: #00adef; }
					
					/* List View (Table) Styles */
					table { width: 100%; border-collapse: separate; border-spacing: 0 10px; }
					th { text-align: left; padding: 10px 15px; color: #666; font-size: 0.85em; text-transform: uppercase; letter-spacing: 1px; }
					td { background: white; padding: 15px; border-top: 1px solid #eee; border-bottom: 1px solid #eee; vertical-align: middle; }
					td:first-child { border-left: 1px solid #eee; border-top-left-radius: 8px; border-bottom-left-radius: 8px; }
					td:last-child { border-right: 1px solid #eee; border-top-right-radius: 8px; border-bottom-right-radius: 8px; }

					/* Fixed Thumbnail Size for List View */
					img { 
						width: 160px !important; 
						height: 90px !important; 
						border-radius: 6px; 
						object-fit: cover; 
						display: block;
						background: #000;
					}

					/* Grid Layout Logic */
					.grid-view #video-table, 
					.grid-view thead, 
					.grid-view tbody, 
					.grid-view tr, 
					.grid-view td { display: block; width: 100%; border: none; background: transparent; }
					
					.grid-view #video-table tbody { 
						display: grid; 
						grid-template-columns: repeat(auto-fill, minmax(300px, 1fr)); 
						gap: 25px; 
					}

					.grid-view tr { 
						background: white; 
						padding: 0; 
						border-radius: 12px; 
						overflow: hidden;
						box-shadow: 0 4px 12px rgba(0,0,0,0.05); 
						transition: transform 0.3s ease, box-shadow 0.3s ease; 
					}
					
					.grid-view tr:hover { transform: translateY(-8px); box-shadow: 0 12px 20px rgba(0,0,0,0.1); }
					
					.grid-view td { padding: 15px; }
					.grid-view td:first-child { padding: 0; border-radius: 0; } /* Thumbnail cell */

					/* Let image expand in Grid View */
					.grid-view img { 
						width: 100% !important; 
						height: auto !important; 
						aspect-ratio: 16/9; 
						border-radius: 0;
					}

					/* Buttons & Tags */
					.btn { text-decoration: none; background: #00adef; color: white; padding: 8px 16px; border-radius: 6px; font-weight: bold; display: inline-block; margin-right: 5px; font-size: 0.9em; }
					.status { font-weight: bold; text-transform: uppercase; font-size: 0.7em; padding: 4px 10px; border-radius: 20px; }
					.completed { background: #e6fffa; color: #2c7a7b; }
					.pending { background: #fffaf0; color: #9c4221; }
				</style>
			</head>
			<body>
						<div class="header-flex">
							<h1>My Library</h1>
							
							<div style="flex-grow: 1; margin: 0 40px; max-width: 400px;">
								<input type="text" id="searchInput" onkeyup="filterVideos()" 
									placeholder="Search videos by title..." 
									style="width: 100%; padding: 10px 15px; border-radius: 20px; border: 1px solid #ddd; outline: none; box-sizing: border-box;">
							</div>

							<div>
								<button class="view-btn active" id="listBtn" onclick="toggleView('list')">List</button>
								<button class="view-btn" id="gridBtn" onclick="toggleView('grid')">Grid</button>
							</div>
						</div>

				<div id="wrapper">
					<table id="video-table">
						<thead>
							<tr>
								<th>Preview</th>
								<th>Video Details</th>
								<th>Status</th>
								<th>Actions</th>
							</tr>
						</thead>
						<tbody>`)

		for rows.Next() {
			var id, status, title, sourcePath, thumbURL string
			rows.Scan(&id, &status, &title, &sourcePath, &thumbURL)

			if thumbURL == "" {
				thumbURL = fmt.Sprintf("https://%s.s3.us-east-2.amazonaws.com/%s_thumb.jpg", os.Getenv("S3_BUCKET_NAME"), id)
			}

			statusClass := "pending"
			if status == "COMPLETED" {
				statusClass = "completed"
			}

			actions := "---"
			if status == "COMPLETED" {
				actions = fmt.Sprintf(`
						<a class="btn" href="/view/%s">Watch</a>
						<a class="btn" style="background:#2c7a7b;" href="%s" download>Save</a>`, id, sourcePath)
			}

			deleteBtn := fmt.Sprintf(`
					<form action="/delete/%s" method="POST" style="display:inline;" onsubmit="return confirm('Delete permanently?');">
						<button type="submit" style="background:none; color:#e53e3e; border:none; cursor:pointer; font-size:0.85em; font-weight:bold; margin-top:10px;">Delete</button>
					</form>`, id)

			fmt.Fprintf(w, `
					<tr>
						<td><img src="%s" onerror="this.src='https://via.placeholder.com/160x90?text=Processing...'"></td>
						<td>
							<strong style="font-size: 1.1em; display:block; margin-bottom:5px;">%s</strong>
							<a href="/edit/%s" style="color:#00adef; text-decoration:none; font-size:0.85em;">Edit Title</a>
						</td>
						<td><span class="status %s">%s</span></td>
						<td>%s %s</td>
					</tr>`, thumbURL, title, id, statusClass, status, actions, deleteBtn)
		}

		fmt.Fprint(w, `
						</tbody>
					</table>
				</div>

				<script>
					function toggleView(type) {
						const wrapper = document.getElementById('wrapper');
						const listBtn = document.getElementById('listBtn');
						const gridBtn = document.getElementById('gridBtn');

						if (type === 'grid') {
							wrapper.classList.add('grid-view');
							gridBtn.classList.add('active');
							listBtn.classList.remove('active');
						} else {
							wrapper.classList.remove('grid-view');
							listBtn.classList.add('active');
							gridBtn.classList.remove('active');
						}
					}
					function filterVideos() {
                    const input = document.getElementById('searchInput');
                    const filter = input.value.toLowerCase();
                    const table = document.getElementById('video-table');
                    const rows = table.getElementsByTagName('tr');  
                    for (let i = 1; i < rows.length; i++) {
                        const titleCell = rows[i].getElementsByTagName('td')[1];
                        if (titleCell) {
                            const titleText = titleCell.textContent || titleCell.innerText;
                            if (titleText.toLowerCase().indexOf(filter) > -1) {
                                rows[i].style.display = "";
                            } else {
                                rows[i].style.display = "none";
                            }
                        }
                    }
                }
				</script>
			</body>
			</html>`)
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

		storage.DeleteFromS3(id + "_processed.mp4")
		storage.DeleteFromS3(id + "_thumb.jpg")

		_, err := db.Exec("DELETE FROM videos WHERE id = ?", id)
		if err != nil {
			log.Printf("DB Delete Error: %v", err)
		}

		log.Printf("Successfully deleted video %s from S3 and DB", id)
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
