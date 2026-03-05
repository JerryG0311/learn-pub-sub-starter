package main

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"os/exec"
	"time"

	"github.com/JerryG0311/Vidify/internal/pubsub"
	"github.com/JerryG0311/Vidify/internal/routing"
	"github.com/JerryG0311/Vidify/internal/storage"
	_ "github.com/mattn/go-sqlite3"
	amqp "github.com/rabbitmq/amqp091-go"
)

var db *sql.DB

func main() {
	// SETTING UP DATABASE CONNECTION
	var err error
	db, err = sql.Open("sqlite3", "vidify.db")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	connString := os.Getenv("RABBITMQ_URL")
	if connString == "" {
		connString = "amqp://guest:guest@localhost:5672/"
	}

	var conn *amqp.Connection

	// RETRY LOOP: Try to connect 5 times with a 2-second pause between each
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

	ch, err := conn.Channel()
	if err != nil {
		log.Fatalf("Failed to open a channel: %v", err)
	}

	defer ch.Close()

	err = ch.ExchangeDeclare(
		routing.ExchangeVideoTopic,
		"topic",
		true,
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		log.Fatalf("Failed to declare exchange: %v", err)
	}
	ch.Close()

	fmt.Println("Vidify Worker started. Waiting for video jobs...")

	err = pubsub.SubscribeJSON(
		conn,
		routing.ExchangeVideoTopic,
		routing.VideoQueue,
		routing.VideoUploadKey,
		pubsub.SimpleQueueDurable,
		handlerVideoJob,
	)

	if err != nil {
		log.Fatalf("Worker failed to subscribe: %v", err)
	}

	select {}
}

func handlerVideoJob(job routing.VideoJob) pubsub.AckType {
	fmt.Printf(" Worker received job %s. Starting transcode...\n", job.ID)

	// 1. Prepare Local Paths ( Temporary storage inside the container)
	inputLocal := fmt.Sprintf("/tmp/%s_input.mp4", job.ID)
	thumbLocal := fmt.Sprintf("/tmp/%s_thumb.jpg", job.ID)
	outputLocal := fmt.Sprintf("/tmp/%s_processed.mp4", job.ID)

	// Clean up local files when done
	defer os.Remove(inputLocal)
	defer os.Remove(thumbLocal)
	defer os.Remove(outputLocal)

	// 2. Download from S3 to local
	if err := storage.DownloadFromS3(job.SourcePath, inputLocal); err != nil {
		log.Printf("Download failed: %v", err)
		time.Sleep(5 * time.Second) // pause for 5 seconds before retyring
		return pubsub.NackRequeue
	}

	db.Exec("UPDATE videos SET status = ? WHERE ID = ?", "PROCESSING", job.ID)

	// 3. Generate Thumbnail
	err := exec.Command("ffmpeg", "-i", inputLocal, "-ss", "00:00:01.000", "-vframes", "1", thumbLocal).Run()
	if err != nil {
		log.Printf("Thumbnail generation failed: %v", err)
	}

	// 4. Main Transcode
	if err := exec.Command("ffmpeg", "-y", "-i", inputLocal, outputLocal).Run(); err != nil {
		log.Printf("Transcode failed: %v", err)
		db.Exec("UPDATE videos SET status = ? WHERE ID = ?", "FAILED", job.ID)
		return pubsub.NackDiscard
	}

	// 5. Upload Results Back to S3
	fmt.Printf("Transcoding complete. Uploading results to S3...\n")

	// Upload Processed Video
	processedS3URL, _ := storage.UploadFileToS3(fmt.Sprintf("%s_processed.mp4", job.ID), outputLocal)
	// Upload Thumbnail
	thumbS3URL, _ := storage.UploadFileToS3(fmt.Sprintf("%s_thumb.jpg", job.ID), thumbLocal)

	// 6. Update Databse with the NEW S3 URLs
	_, err = db.Exec(
		"UPDATE videos SET status = ?, source_path = ?, thumbnail_url = ? WHERE ID = ?",
		"COMPLETED", processedS3URL, thumbS3URL, job.ID,
	)
	if err != nil {
		log.Printf("Final DB Update Error: %v", err)
	}

	return pubsub.Ack
}
