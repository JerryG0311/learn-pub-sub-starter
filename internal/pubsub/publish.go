package pubsub

import (
	"context"
	"encoding/json"

	amqp "github.com/rabbitmq/amqp091-go"
)

func PublishJSON[T any](
	conn *amqp.Connection,
	exchange,
	key string,
	val T,
) error {
	// 1. Open a channel from the connection
	ch, err := conn.Channel()
	if err != nil {
		return err
	}
	defer ch.Close()

	// Marshal the value to JSON
	data, err := json.Marshal(val)
	if err != nil {
		return err
	}

	// Publish to the exchange with the routing key
	return ch.PublishWithContext(
		context.Background(),
		exchange,
		key,
		false, // mandatory
		false, // immediate
		amqp.Publishing{
			ContentType: "application/json",
			Body:        data,
		},
	)
}

type SimpleQueueType int

const (
	QueueTypeDurable SimpleQueueType = iota
	QueueTypeTransient
)

func DeclareAndBind(
	conn *amqp.Connection,
	exchange,
	queueName,
	key string,
	queueType SimpleQueueType, // This is the enum type that was made to rep "durable" or "transient"
) (*amqp.Channel, amqp.Queue, error) {
	// 1. Create the channel
	ch, err := conn.Channel()
	if err != nil {
		return nil, amqp.Queue{}, err
	}

	// 2. Set properties based on type
	durable := queueType == QueueTypeDurable
	autoDelete := queueType == QueueTypeTransient
	exclusive := queueType == QueueTypeTransient

	queue, err := ch.QueueDeclare(
		queueName,
		durable,
		autoDelete,
		exclusive,
		false, // noWait param
		nil,   // args param
	)
	if err != nil {
		return nil, amqp.Queue{}, err
	}

	// 4. Bind queue
	err = ch.QueueBind(queue.Name, key, exchange, false, nil)
	if err != nil {
		return nil, amqp.Queue{}, err
	}

	return ch, queue, nil
}
