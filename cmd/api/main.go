package main

import (
	"fmt"
	"log"
	"time"

	"github.com/JerryG0311/Vidify/internal/pubsub"
	"github.com/JerryG0311/Vidify/internal/routing"
	amqp "github.com/rabbitmq/amqp091-go"
)

func main() {

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

	// 4. Simulate a video upload
	job := routing.VideoJob{
		ID:           "vid-001",
		SourcePath:   "s3://raw-bucket/my_audio_vlog.mov",
		TargetFormat: "mp4",
		UserID:       "jerry_g",
		CreatedAt:    time.Now(),
	}

	// 5. Publish the jon
	err = pubsub.PublishJSON(
		ch,
		routing.ExchangeVideoTopic,
		routing.VideoUploadKey,
		job,
	)
	if err != nil {
		log.Fatalf("Failed to publish video job: %v", err)
	}

	fmt.Printf("🚀 [API] Upload successful! Published job %s for user %s\n", job.ID, job.UserID)
}
