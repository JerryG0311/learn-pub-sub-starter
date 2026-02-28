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

	_, _, err = pubsub.DeclareAndBind(
		conn,
		routing.ExchangePerilTopic,
		"game_logs",
		routing.GameLogSlug+".*",
		pubsub.QueueTypeDurable,
	)
	if err != nil {
		log.Fatalf("could not subscribe to game logs: %v", err)
	}
	fmt.Println("Server subsribed to game logs!")

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
				conn, // Pass the connection, the PublishJSON helper handles the channel
				routing.ExchangePerilDirect,
				routing.PauseKey,
				routing.PlayingState{IsPaused: true},
			)
		case "resume":
			fmt.Println("Sending resume message...")
			err = pubsub.PublishJSON(
				conn,
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
