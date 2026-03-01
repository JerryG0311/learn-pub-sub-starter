package main

import (
	"fmt"
	"log"
	"strconv"
	"time"

	"github.com/bootdotdev/learn-pub-sub-starter/internal/gamelogic"
	"github.com/bootdotdev/learn-pub-sub-starter/internal/pubsub"
	"github.com/bootdotdev/learn-pub-sub-starter/internal/routing"
	amqp "github.com/rabbitmq/amqp091-go"
)

func main() {
	// 1. Connection string
	publishConnString := "amqp://guest:guest@localhost:5672/"

	// 2. Connect to RabbitMQ
	conn, err := amqp.Dial(publishConnString)
	if err != nil {
		log.Fatalf("Could not connect to RabbitMQ: %v", err)
	}
	defer conn.Close()
	fmt.Println("Connected to RabbitMQ successfully!")

	// 3. Prompt for username
	username, err := gamelogic.ClientWelcome()
	if err != nil {
		log.Fatalf("Could not get username: %v", err)
	}

	gs := gamelogic.NewGameState(username)

	err = pubsub.SubscribeJSON(
		conn,
		routing.ExchangePerilDirect,
		routing.PauseKey+"."+username,
		routing.PauseKey,
		pubsub.QueueTypeTransient,
		handlerPause(gs),
	)
	if err != nil {
		log.Fatalf("Could not subscribe to pause: %v", err)
	}

	publishCh, err := conn.Channel()
	if err != nil {
		log.Fatalf("Could not open channel: %v", err)
	}

	err = pubsub.SubscribeJSON(
		conn,
		routing.ExchangePerilTopic,
		routing.ArmyMovesPrefix+"."+username,
		routing.ArmyMovesPrefix+".*",
		pubsub.QueueTypeTransient,
		handlerMove(gs, publishCh),
	)
	if err != nil {
		log.Fatalf("Could not subscribe to army moves: %v", err)
	}

	err = pubsub.SubscribeJSON(
		conn,
		routing.ExchangePerilTopic,
		"war",
		routing.WarRecognitionsPrefix+".*",
		pubsub.QueueTypeDurable,
		handlerWar(gs, publishCh),
	)
	if err != nil {
		log.Fatalf("Could not subscribe to ware: %v", err)
	}

	// 4. process game commands (locally)
	for {
		words := gamelogic.GetInput()
		if len(words) == 0 {
			continue
		}

		switch words[0] {
		case "spawn":
			err := gs.CommandSpawn(words)
			if err != nil {
				fmt.Printf("Error: %v\n", err)
			}
		case "move":
			move, err := gs.CommandMove(words)
			if err != nil {
				fmt.Printf("Error: %v\n", err)
			} else {
				routingKey := routing.ArmyMovesPrefix + "." + move.Player.Username
				err := pubsub.PublishJSON(
					publishCh,
					routing.ExchangePerilTopic,
					routingKey,
					move,
				)
				if err != nil {
					fmt.Printf("Error publishing move: %v\n", err)
				} else {
					fmt.Printf("Move published for %s\n", move.Player.Username)
				}
			}
		case "status":
			gs.CommandStatus()
		case "help":
			gamelogic.PrintClientHelp()
		case "spam":
			if len(words) < 2 {
				fmt.Println("Usage: spam <number>")
				continue
			}
			n, err := strconv.Atoi(words[1])
			if err != nil {
				fmt.Printf("Error: %v is not a valid number\n", words[1])
				continue
			}

			for i := 0; i < n; i++ {
				logMsg := gamelogic.GetMaliciousLog()
				err := pubsub.PublishGob(
					publishCh,
					routing.ExchangePerilTopic,
					routing.GameLogSlug+"."+gs.GetPlayerSnap().Username,
					routing.GameLog{
						Username:    gs.GetPlayerSnap().Username,
						Message:     logMsg,
						CurrentTime: time.Now(),
					},
				)
				if err != nil {
					fmt.Printf("Error publishing spam log: %v\n", err)
					break
				}
			}
			fmt.Printf("published %v malicious logs\n", n)

		case "quit":
			gamelogic.PrintQuit()
			return
		default:
			fmt.Println("Unkown command. Type 'help' for options.")
		}
	}

}

func handlerPause(gs *gamelogic.GameState) func(routing.PlayingState) pubsub.AckType {
	return func(ps routing.PlayingState) pubsub.AckType {
		defer fmt.Print("> ")
		gs.HandlePause(ps)
		return pubsub.Ack
	}
}

func handlerMove(gs *gamelogic.GameState, ch *amqp.Channel) func(gamelogic.ArmyMove) pubsub.AckType {
	return func(move gamelogic.ArmyMove) pubsub.AckType {
		defer fmt.Print("> ")
		outcome := gs.HandleMove(move)

		switch outcome {
		case gamelogic.MoveOutComeSafe:
			return pubsub.Ack
		case gamelogic.MoveOutcomeMakeWar:
			// Publish war recognition
			err := pubsub.PublishJSON(
				ch,
				routing.ExchangePerilTopic,
				routing.WarRecognitionsPrefix+"."+gs.GetPlayerSnap().Username,
				gamelogic.RecognitionOfWar{
					Attacker: move.Player,
					Defender: gs.GetPlayerSnap(),
				},
			)
			if err != nil {
				fmt.Printf("Error publishing war: %v\n", err)
				return pubsub.NackRequeue
			}
			return pubsub.Ack
		case gamelogic.MoveOutcomeSamePlayer:
			return pubsub.NackDiscard
		default:
			return pubsub.NackDiscard
		}
	}
}

func handlerWar(gs *gamelogic.GameState, publishCh *amqp.Channel) func(gamelogic.RecognitionOfWar) pubsub.AckType {
	return func(row gamelogic.RecognitionOfWar) pubsub.AckType {
		defer fmt.Print("> ")
		outcome, winner, loser := gs.HandleWar(row)

		var message string
		switch outcome {
		case gamelogic.WarOutcomeNotInvolved:
			return pubsub.NackRequeue
		case gamelogic.WarOutcomeNoUnits:
			return pubsub.NackDiscard
		case gamelogic.WarOutcomeOpponentWon:
			message = fmt.Sprintf("%v won a war against %v", winner, loser)
		case gamelogic.WarOutcomeYouWon:
			message = fmt.Sprintf("%v won a war against %v", winner, loser)
		case gamelogic.WarOutcomeDraw:
			message = fmt.Sprintf("A war between %v and %v resulted in a draw", winner, loser)
		}

		err := pubsub.PublishGob(
			publishCh,
			routing.ExchangePerilTopic,
			routing.GameLogSlug+"."+row.Attacker.Username,
			routing.GameLog{
				Username:    row.Attacker.Username,
				Message:     message,
				CurrentTime: time.Now(),
			},
		)
		if err != nil {
			fmt.Printf("error publishing log: %v\n", err)
			return pubsub.NackRequeue
		}

		return pubsub.Ack
	}
}
