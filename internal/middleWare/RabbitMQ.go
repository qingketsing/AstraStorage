package middleware

import (
	"fmt"
	"log"

	amqp "github.com/rabbitmq/amqp091-go"
)

type RabbitMQ struct {
	conn    *amqp.Connection
	channel *amqp.Channel
}

func NewRabbitMQConnection(url string) (*RabbitMQ, error) {
	conn, err := amqp.Dial(url)
	if err != nil {
		return nil, fmt.Errorf("连接 RabbitMQ 失败: %w", err)
	}

	channel, err := conn.Channel()
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("创建 RabbitMQ Channel 失败: %w", err)
	}

	log.Printf("RabbitMQ 连接成功: %s", url)
	return &RabbitMQ{conn: conn, channel: channel}, nil
}

func (r *RabbitMQ) Close() error {
	if r.channel != nil {
		r.channel.Close()
	}
	if r.conn != nil {
		return r.conn.Close()
	}
	return nil
}

// Publish 发布消息到队列
func (r *RabbitMQ) Publish(queueName string, message []byte) error {
	// 声明队列
	_, err := r.channel.QueueDeclare(
		queueName, // 队列名称
		true,      // 持久化
		false,     // 自动删除
		false,     // 排他
		false,     // 不等待
		nil,       // 参数
	)
	if err != nil {
		return fmt.Errorf("声明队列失败: %w", err)
	}

	// 发布消息
	err = r.channel.Publish(
		"",        // exchange
		queueName, // routing key
		false,     // mandatory
		false,     // immediate
		amqp.Publishing{
			ContentType: "text/plain",
			Body:        message,
		},
	)
	return err
}

// Consume 消费队列消息
func (r *RabbitMQ) Consume(queueName string) (<-chan amqp.Delivery, error) {
	// 声明队列
	_, err := r.channel.QueueDeclare(
		queueName,
		true,
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("声明队列失败: %w", err)
	}

	// 开始消费
	msgs, err := r.channel.Consume(
		queueName, // 队列
		"",        // consumer
		true,      // auto-ack
		false,     // exclusive
		false,     // no-local
		false,     // no-wait
		nil,       // args
	)
	return msgs, err
}
