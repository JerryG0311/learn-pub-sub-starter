package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"

	"github.com/bootdotdev/learn-pub-sub-starter/internal/pubsub"
	"github.com/bootdotdev/learn-pub-sub-starter/internal/routing"
	amqp "github.com/rabbitmq/amqp091-go"
)

func main() {
	connectionString := "amqp://guest:guest@localhost:5672/"

	// 1. Dial the connection
	conn, err := amqp.Dial(connectionString)
	if err != nil {
		log.Fatalf("Could not connect to RabbitMQ: %v", err)
	}

	// 2. Defer close
	defer conn.Close()
	fmt.Println("Connection to RabbitMQ was successful!")

	//3. Create a new channel
	ch, err := conn.Channel()
	if err != nil {
		log.Fatalf("Could not open channel: %v", err)
	}
	defer ch.Close()

	// 3.1 Publish a pause message
	// This should be in main() after the channel is opened
	err = pubsub.PublishJSON(
		ch,
		routing.ExchangePerilDirect,
		routing.PauseKey,
		routing.PlayingState{
			IsPaused: true,
		},
	)
	if err != nil {
		log.Fatalf("could not publish JSON: %v", err)
	}
	fmt.Println("Pause message published!")

	// 4. Wait for a signal (Ctrl+C)
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, os.Interrupt)

	<-signalChan
	fmt.Println("Shutting down...")
}
