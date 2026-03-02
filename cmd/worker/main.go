package main

import (
	"fmt"
	"log"
	"os/exec"

	"github.com/JerryG0311/Vidify/internal/pubsub"
	"github.com/JerryG0311/Vidify/internal/routing"
	amqp "github.com/rabbitmq/amqp091-go"
)

func main() {
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

	// 1. Generate Instant Preview (Thumbnail)
	// Grabs one frame from the 1-second mark
	thumbFile := fmt.Sprintf("%s_thumb.jpg", job.ID)
	thumbCmd := exec.Command("ffmpeg", "-i", job.SourcePath, "-ss", "00:00:01.000", "-vframes", "1", thumbFile)
	if err := thumbCmd.Run(); err == nil {
		fmt.Printf("Instant Preview created: %s\n", thumbFile)
	}

	// 2. MAIN TRANSCODE

	// Later on I'll setup S3 and replace SourchePath with an S3 link
	outputFile := fmt.Sprintf("%s_processed.%s", job.ID, job.TargetFormat)
	cmd := exec.Command("ffmpeg", "-i", job.SourcePath, outputFile)

	if err := cmd.Run(); err != nil {
		fmt.Printf("Main transcode failed for %s\n", job.ID)
		return pubsub.NackDiscard
	}

	fmt.Printf("Transcoding complete! Save to: %s\n", outputFile)
	return pubsub.Ack
}
