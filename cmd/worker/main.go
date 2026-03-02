package main

import (
	"database/sql"
	"fmt"
	"log"
	"os/exec"

	"github.com/JerryG0311/Vidify/internal/pubsub"
	"github.com/JerryG0311/Vidify/internal/routing"
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

	connString := "amqp://guest:guest@localhost:5672/"
	conn, err := amqp.Dial(connString)
	if err != nil {
		log.Fatalf("Failed to connect to RabbitMQ: %v", err)
	}
	defer conn.Close()

	ch, err := conn.Channel()
	if err != nil {
		log.Fatalf("Failed to open a channel: %v", err)
	}

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
	//-- UPDATED STATUS TO PROCESSING --
	_, err := db.Exec("UPDATE videos SET status = ? WHERE id = ?", "PROCESSING", job.ID)
	if err != nil {
		log.Printf("DB Error (processing): %v", err)
	}

	thumbFile := fmt.Sprintf("data/%s_thumb.jpg", job.ID)
	outputFile := fmt.Sprintf("data/%s_processed.%s", job.ID, job.TargetFormat)

	// 1. Generate Instant Preview (Thumbnail)
	// Grabs one frame from the 1-second mark

	thumbCmd := exec.Command("ffmpeg", "-i", job.SourcePath, "-ss", "00:00:01.000", "-vframes", "1", thumbFile)
	thumbCmd.Run()

	// 2. MAIN TRANSCODE

	// Later on I'll setup S3 and replace SourchePath with an S3 link

	cmd := exec.Command("ffmpeg", "-i", job.SourcePath, outputFile)
	if err := cmd.Run(); err != nil {
		fmt.Printf("Main transcode failed for %s\n", job.ID)
		db.Exec("UPDATE videos SET status = ? WHERE id = ?", "FAILED", job.ID)
		return pubsub.NackDiscard
	}

	// -- UPDATE STATUS TO COMPLETED --

	db.Exec("UPDATE videos SET status = ? WHERE id = ?", "COMPLETED", job.ID)

	fmt.Printf("Transcoding complete! Save to: %s\n", outputFile)
	return pubsub.Ack
}
