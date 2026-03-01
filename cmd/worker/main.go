package main

import (
	"fmt"
	"log"

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
	fmt.Printf("🎥 Processing video %s for user %s...\n", job.ID, job.UserID)
	fmt.Printf("   -> Transcoding %s to %s format\n", job.SourcePath, job.TargetFormat)

	fmt.Println("✅ Transcoding complete!")
	return pubsub.Ack
}
