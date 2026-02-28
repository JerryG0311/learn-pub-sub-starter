package pubsub

import (
	"encoding/json"
	"fmt"

	amqp "github.com/rabbitmq/amqp091-go"
)

func SubscribeJSON[T any](
	conn *amqp.Connection,
	exchange,
	queueName,
	key string,
	queueType SimpleQueueType, // an enum to rep "durable" or "transient"
	handler func(T),
) error {
	// 1. Ensures the queue exists and is bound to the exchange
	_, queue, err := DeclareAndBind(conn, exchange, queueName, key, queueType)
	if err != nil {
		return err
	}

	// 2. Open a dedicated channel for this consumer
	ch, err := conn.Channel()
	if err != nil {
		return err
	}

	// 3. Begin consuming the message stream
	msgs, err := ch.Consume(
		queue.Name,
		"",    // Let RabbitMQ generate a unique consumer tag
		false, // auto-ack: false (we want manual acknowledgments)
		false, // exclusive
		false, // non-local
		false, // no-wait
		nil,   // args
	)
	if err != nil {
		return err
	}

	// 4. Process messages in a background goroutine
	go func() {
		defer ch.Close()
		for msg := range msgs {
			var target T
			err := json.Unmarshal(msg.Body, &target)
			if err != nil {
				fmt.Printf("Error unmarshaling message: %v\n", err)
				continue
			}
			// Pass the data to the game logic handler
			handler(target)

			msg.Ack(false)
		}
	}()

	return nil
}
