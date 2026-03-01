package pubsub

import (
	"bytes"
	"context"
	"encoding/gob"
	"encoding/json"

	amqp "github.com/rabbitmq/amqp091-go"
)

func PublishJSON[T any](
	ch *amqp.Channel,
	exchange,
	routingKey string,
	val T,
) error {
	// Marshal the value to JSON
	data, err := json.Marshal(val)
	if err != nil {
		return err
	}
	// Publish to the exchange with the routing key
	return ch.PublishWithContext(
		context.Background(),
		exchange,
		routingKey,
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
	// creating table for args to be passed in
	args := amqp.Table{
		"x-dead-letter-exchange": "peril_dlx",
	}

	queue, err := ch.QueueDeclare(
		queueName,
		queueType == QueueTypeDurable,   // durable
		queueType == QueueTypeTransient, // auto-delete
		queueType == QueueTypeTransient, // exclusive
		false,                           // noWait
		args,                            // args
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

func PublishGob[T any](
	ch *amqp.Channel,
	exchange,
	routingKey string,
	val T,
) error {
	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	err := enc.Encode(val)
	if err != nil {
		return err
	}

	return ch.PublishWithContext(
		context.Background(),
		exchange,
		routingKey,
		false,
		false,
		amqp.Publishing{
			ContentType: "application/gob",
			Body:        buf.Bytes(),
		},
	)
}
