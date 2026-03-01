package main

import (
	"fmt"
	"log"

	"github.com/bootdotdev/learn-pub-sub-starter/internal/gamelogic"
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

	publishCh, err := conn.Channel()
	if err != nil {
		log.Fatalf("Could not open channel: %v", err)
	}

	err = pubsub.SubscribeGob(
		conn,
		routing.ExchangePerilTopic,
		"game_logs",
		routing.GameLogSlug+".*",
		pubsub.QueueTypeDurable,
		func(gl routing.GameLog) pubsub.AckType {
			defer fmt.Print("> ")
			err := gamelogic.WriteLog(gl)
			if err != nil {
				fmt.Printf("error writing log: %v/n", err)
				return pubsub.NackRequeue
			}
			return pubsub.Ack
		},
	)
	if err != nil {
		log.Fatalf("could not subscribe to game logs: %v", err)
	}
	fmt.Println("Server subscribed to game logs and listening...")

	// 1. Show the admin what commands are available
	gamelogic.PrintServerHelp()

	for {
		// 2. Wait for user to type something (e.g., "pause")
		words := gamelogic.GetInput()
		if len(words) == 0 {
			continue
		}

		// 3. Handle the commands
		switch words[0] {
		case "pause":
			fmt.Println("Sending pause message...")
			err = pubsub.PublishJSON(
				publishCh,
				routing.ExchangePerilDirect,
				routing.PauseKey,
				routing.PlayingState{IsPaused: true},
			)
		case "resume":
			fmt.Println("Sending resume message...")
			err = pubsub.PublishJSON(
				publishCh,
				routing.ExchangePerilDirect,
				routing.PauseKey,
				routing.PlayingState{IsPaused: false},
			)
		case "quit":
			fmt.Println("Exiting...")
			return // Breaking the loop and closing the server
		default:
			fmt.Println("Unkown command")
		}
		if err != nil {
			log.Printf("Error publishing: %v", err)
		}
	}
}
