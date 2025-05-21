package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/AungKyawPhyo1142/sentria-go/internal/config"
	"github.com/AungKyawPhyo1142/sentria-go/internal/core/service"
	"github.com/AungKyawPhyo1142/sentria-go/internal/models"
	amqp "github.com/rabbitmq/amqp091-go"
)

const (
	consumerTag       = "sentria-factchecker-consumer"
	connectRetryDelay = 5 * time.Second
	maxConnectRetries = 10
	factCheckTimeout  = 60 * time.Second
)

// JobConsumer is a struct that consumes jobs from a queue
type JobConsumer struct {
	conn             *amqp.Connection
	channel          *amqp.Channel
	cfg              *config.Config
	factCheckService *service.FactCheckService
	done             chan bool // channel to signal when the consumer is done
	isStopping       bool
	notifyConnClose  chan *amqp.Error
	notifyChanClose  chan *amqp.Error
}

func NewJobConsumer(cfg *config.Config, fcService *service.FactCheckService) (*JobConsumer, error) {

	if fcService == nil {
		return nil, fmt.Errorf("factCheckService cannot be nil")
	}

	jc := &JobConsumer{
		cfg:              cfg,
		factCheckService: fcService, // pass the factcheck service here,
		done:             make(chan bool),
	}

	if err := jc.connect(); err != nil {
		// Attempt to connect one more time after a delay if initial fails, then error out
		log.Printf("[JobConsumer] Initial connection failed: %v. Retrying once after delay...", err)
		time.Sleep(connectRetryDelay)
		if err = jc.connect(); err != nil {
			return nil, fmt.Errorf("failed to connect to RabbitMQ after initial attempts: %w", err)
		}
	}

	return jc, nil

	// var err error
	// log.Printf("[JobConsumer] Attempting to connect to RabbitMQ at %s", jc.cfg.RabbitMQ_URL)

	// jc.conn, err = amqp.Dial(cfg.RabbitMQ_URL)
	// if err != nil {
	// 	log.Printf("[JobConsumer] Failed to connect to RabbitMQ: %v", err)
	// 	return nil, err
	// }
	// log.Printf("[JobConsumer] Connected to RabbitMQ at %s", jc.cfg.RabbitMQ_URL)

	// jc.channel, err = jc.conn.Channel()
	// if err != nil {
	// 	log.Printf("[JobConsumer] Failed to open a channel: %v", err)
	// 	return nil, err
	// }
	// log.Printf("[JobConsumer] Opened a channel")

	// // Ensure that the queue exists and durable (should match producer's assetion)

	// _, err = jc.channel.QueueDeclare(
	// 	cfg.Factcheck_Queue_Name, // queue name
	// 	true,                     // durable
	// 	false,                    // delete when unused
	// 	false,                    // exclusive
	// 	false,                    // no-wait
	// 	nil,                      // arguments
	// )
	// if err != nil {
	// 	log.Printf("[JobConsumer] Failed to declare a queue: %v", err)
	// 	jc.channel.Close()
	// 	jc.conn.Close()
	// 	return nil, err
	// }

	// log.Printf("[JobConsumer] Declared a queue: %s", cfg.Factcheck_Queue_Name)

	// // Optional: Set Quality of Service (QoS) to process one message at a time initially
	// // This means the consumer will only receive the next message after it acknowledges the current one.
	// err = jc.channel.Qos(
	// 	1,     // prefetch count: How many messages the server will deliver, at most, at a time.
	// 	0,     // prefetch size: Server will try to keep at least this many bytes undelivered. (0 means no specific limit)
	// 	false, // global: false means QoS applies per new consumer (this channel)
	// )
	// if err != nil {
	// 	log.Printf("[JobConsumer] Failed to set QoS: %v", err)
	// } else {
	// 	log.Printf("[JobConsumer] Set QoS set to prefetch count: 1")
	// }

	// return jc, nil

}

func (jc *JobConsumer) connect() error {
	var err error
	log.Printf("[JobConsumer] Attempting to connect to RabbitMQ at %s", jc.cfg.RabbitMQ_URL)

	jc.conn, err = amqp.Dial(jc.cfg.RabbitMQ_URL)
	if err != nil {
		return fmt.Errorf("[JobConsumer] Failed to connect to RabbitMQ: %v", err)

	}
	log.Printf("[JobConsumer] Connected to RabbitMQ at %s", jc.cfg.RabbitMQ_URL)

	jc.channel, err = jc.conn.Channel()
	if err != nil {
		jc.conn.Close()
		return fmt.Errorf("[JobConsumer] Failed to open a channel: %v", err)

	}
	log.Printf("[JobConsumer] Opened a channel")

	// reinit notify channels for the new connection/channel
	jc.notifyConnClose = make(chan *amqp.Error) // reinitialize channel close notification connection
	jc.notifyChanClose = make(chan *amqp.Error) // reinitialize channel close notification channel
	jc.conn.NotifyClose(jc.notifyConnClose)
	jc.channel.NotifyClose(jc.notifyChanClose)

	// Ensure that the queue exists and durable (should match producer's assetion)
	_, err = jc.channel.QueueDeclare(
		jc.cfg.Factcheck_Queue_Name, // queue name
		true,                        // durable
		false,                       // delete when unused
		false,                       // exclusive
		false,                       // no-wait
		nil,                         // arguments
	)
	if err != nil {

		jc.channel.Close()
		jc.conn.Close()
		return fmt.Errorf("[JobConsumer] Failed to declare a queue: %v", err)
	}

	log.Printf("[JobConsumer] Declared a queue: %s", jc.cfg.Factcheck_Queue_Name)

	// Optional: Set Quality of Service (QoS) to process one message at a time initially
	// This means the consumer will only receive the next message after it acknowledges the current one.
	err = jc.channel.Qos(
		1,     // prefetch count: How many messages the server will deliver, at most, at a time.
		0,     // prefetch size: Server will try to keep at least this many bytes undelivered. (0 means no specific limit)
		false, // global: false means QoS applies per new consumer (this channel)
	)
	if err != nil {
		log.Printf("[JobConsumer] Failed to set QoS: %v", err)
	} else {
		log.Printf("[JobConsumer] Set QoS set to prefetch count: 1")
	}
	return nil // success
}

// connect method
// func (jc *JobConsumer) connect() error {
// 	var err error
// 	for i := 0; i < maxConnectRetries; i++ {
// 		log.Printf("[JobConsumer] Attempting to reconnect to RabbitMQ at attempt %d", i+1)
// 		jc.conn, err = amqp.Dial(jc.cfg.RabbitMQ_URL)
// 		if err == nil {
// 			log.Printf("[JobConsumer] Successfully reconnected to RabbitMQ")
// 			jc.channel, err = jc.conn.Channel()
// 			if err == nil {
// 				log.Printf("[JobConsumer] Successfully re-opened channel")
// 				jc.notifyConnClose = make(chan *amqp.Error) // reinitialize channel close notification connection
// 				jc.notifyChanClose = make(chan *amqp.Error) // reinitialize channel close notification channel
// 				jc.conn.NotifyClose(jc.notifyConnClose)
// 				jc.channel.NotifyClose(jc.notifyChanClose)
// 				return nil
// 			}
// 			log.Printf("[JobConsumer] Failed to re-open channel: %v", err)
// 			jc.conn.Close()
// 		}
// 		log.Printf("[JobConsumer] Failed to reconnect to RabbitMQ: %v", err)
// 		if i < maxConnectRetries-1 {
// 			log.Printf("[JobConsumer] Retrying in %v...", connectRetryDelay)
// 			time.Sleep(connectRetryDelay)
// 		}
// 	}
// 	return fmt.Errorf("failed to reconnect after %d attempts: %w", maxConnectRetries, err)
// }

// Consume starts consuming jobs from the queue
// this method will block until error occurs or Stop is called
func (jc *JobConsumer) StartConsuming(ctx context.Context) {

	if jc.channel == nil {
		log.Println("[JobConsumer] Channel is nil. Attempting to reconnect before consuming.")
		if err := jc.handleReconnect(ctx); err != nil {
			log.Printf("[JobConsumer] FATAL: Failed to reconnect during StartConsuming: %v. Consumer stopping.", err)
			// No need to signal jc.done here, as this goroutine will exit.
			// The main application might need a way to know this startup failed.
			return
		}
	}

	log.Printf("[JobConsumer] Starting to consume messages from queue: %s", jc.cfg.Factcheck_Queue_Name)

	// // handle connection errors in a separate goroutine to attempt to reconnect
	// go func() {
	// 	connErr := <-jc.conn.NotifyClose(make(chan *amqp.Error))
	// 	if connErr != nil {
	// 		log.Printf("[JobConsumer] Connection closed unexpectedly: %v", connErr)
	// 		jc.done <- connErr
	// 	}
	// }()

	// go func() {
	// 	chanErr := <-jc.channel.NotifyClose(make(chan *amqp.Error))
	// 	if chanErr != nil {
	// 		log.Printf("[JobConsumer] Channel closed unexpectedly: %v", chanErr)
	// 		jc.done <- chanErr
	// 	}
	// }()

	deliveries, err := jc.channel.Consume(
		jc.cfg.Factcheck_Queue_Name, // queue
		consumerTag,                 // consumer
		false,                       // auto-ack: false means we need to manually acknowledge the message
		false,                       // exclusive
		false,                       // no-local
		false,                       // no-wait
		nil,                         // args
	)

	if err != nil {
		log.Printf("[JobConsumer] Failed to register a consumer: %v", err)
		// this is critical error for this consuming instance. Try to reconnect
		if reconnErr := jc.handleReconnect(ctx); reconnErr != nil {
			log.Printf("[JobConsumer] FATAL: Failed to reconnect after failed to consume: %v. Consumer stopping.", reconnErr)
		}
		return
	}

	log.Printf("[JobConsumer] Registered a consumer. Waiting for messages...")

	// loop to process messages from msgs channel
	for {
		select {

		case <-ctx.Done():
			log.Printf("[JobConsumer] Context cancelled. Stopping consumer.")
			jc.Stop()
			return

		case d, ok := <-deliveries: // delivery from channel
			if !ok {
				log.Printf("[JobConsumer] Messages channel closed unexpectedly")
				if !jc.isStopping {
					if err := jc.handleReconnect(ctx); err != nil {
						log.Printf("[JobConsumer] FATAL: Failed to reconnect after failed to consume: %v. Consumer stopping.", err)
					}
				}
				return
			}
			log.Printf("[JobConsumer] Received a message. DeliveryTag: %d, MessageID: %s", d.DeliveryTag, d.MessageId)

			// unmarshal into models.DisasterReportData and pass to factCheckService
			var reportData models.DisasterReportData
			if errUnmarshal := json.Unmarshal(d.Body, &reportData); errUnmarshal != nil {
				log.Printf("[JobConsumer] Failed to unmarshal message into DisasterReportData: %v, Body: %s", errUnmarshal, string(d.Body))
				if nackErr := d.Nack(false, false); nackErr != nil {
					log.Printf("[JobConsumer] Error Nacking unparseable message %d: %v", d.DeliveryTag, nackErr)
				}
				continue
			}

			log.Printf("[JobConsumer] Successfully unmarshaled. PG_ID: %s, Title: '%s', Type: %s, Timestamp: %s",
				reportData.PostgresReportID, reportData.Title, reportData.IncidentType, reportData.IncidentTimestamp.Format(time.RFC3339))

			// * Actual Factchecking
			log.Printf("[JobConsumer] Processing report %s with FactcheckingService...", reportData.PostgresReportID)
			// create a new context with timeout for each report
			serviceCtx, serviceCancle := context.WithTimeout(context.Background(), factCheckTimeout)
			factCheckResult, processErr := jc.factCheckService.VerifyReportWithGDACS(serviceCtx, reportData)
			serviceCancle()

			if processErr != nil {
				log.Printf("[JobConsumer] FactcheckingService failed for report %s: %v", reportData.PostgresReportID, processErr)
				if nackErr := d.Nack(false, false); nackErr != nil {
					log.Printf("[JobConsumer] Error Nacking message %d: %v", d.DeliveryTag, nackErr)
				}
				continue
			}

			log.Printf("[JobConsumer] FactcheckingService completed for report %s. Result: %v", reportData.PostgresReportID, factCheckResult)
			if errAck := d.Ack(false); errAck != nil {
				log.Printf("[JobConsumer] Error Acknowledging message %d: %v", d.DeliveryTag, errAck)
			} else {
				log.Printf("[JobConsumer] Successfully Acknowledged message %d", d.DeliveryTag)
			}

			// var genericPayload map[string]interface{}
			// err := json.Unmarshal(d.Body, &genericPayload)
			// if err != nil {
			// 	log.Printf("[JobConsumer] Failed to unmarshal message: %v", err)
			// 	d.Nack(false, false) // Nack, no requeue
			// 	continue             // move to next message
			// }

			// log.Printf("[JobConsumer] Unmarshalled message: %v", genericPayload)

			// // example: check for specific fields from the Node.js test mesage
			// if msg, ok := genericPayload["message"].(string); ok {
			// 	log.Printf("[JobConsumer] Extracted Message: %s", msg)
			// }

			// // Acknowledge the message once processed
			// log.Printf("[JobConsumer] Processing completed. Acknowleging message (DeliveryTag: %d)", d.DeliveryTag)

			// // Acknowledge the message
			// if err := d.Ack(false); err != nil { // false for single message ack
			// 	log.Printf("[JobConsumer] Failed to acknowledge message: %v", err)
			// } else {
			// 	log.Printf("[JobConsumer] Message acknowledged (DeliveryTag: %d)", d.DeliveryTag)
			// }

		case err := <-jc.notifyConnClose:
			log.Printf("[JobConsumer] Connection closed event received: %v", err)
			if !jc.isStopping {
				if reconnErr := jc.handleReconnect(ctx); reconnErr != nil {
					log.Printf("[JobConsumer] FATAL: Failed to reconnect after connection closed: %v. Consumer stopping.", reconnErr)
				}
			}
			return

		case err := <-jc.notifyChanClose:
			log.Printf("[JobConsumer] Channel closed event received via notify: %v.", err)
			if !jc.isStopping {
				if jc.conn == nil || jc.conn.IsClosed() {
					log.Println("[JobConsumer] Connection also appears closed. Reconnect will handle.")
					// The notifyConnClose handler should ideally manage full reconnection.
					// If it doesn't, the next message delivery attempt will fail, triggering the !ok case.
				} else {
					log.Println("[JobConsumer] Channel closed, but connection seems alive. Attempting to re-establish channel and consumer.")
					if reconnErr := jc.handleReconnect(ctx); reconnErr != nil {
						log.Printf("[JobConsumer] FATAL: Reconnect failed after channel close: %v. Consumer stopping.", reconnErr)
					}
				}
			}
			return

		case <-jc.done: // if stop() was called or connetion/channel error
			log.Printf("[JobConsumer] Stop signal received. Exiting consumer.")
			return
		}
	}
}

func (jc *JobConsumer) handleReconnect(ctx context.Context) error {
	log.Printf("[JobConsumer] Attempting to reconnect...")

	if jc.channel != nil {
		jc.channel.Close()
		jc.channel = nil
	}
	if jc.conn != nil {
		jc.conn.Close()
		jc.conn = nil
	}

	// attempt to reconnect
	if err := jc.connect(); err != nil {
		log.Printf("[JobConsumer] Reconnect failed: %v", err)
		return fmt.Errorf("failed to reconnect: %w", err)
	}

	log.Println("[JobConsumer] Reconnected successfully.")
	go jc.StartConsuming(ctx) //! IMPORTANT: pass the original context to StartConsuming
	return nil

}

// stop gracefuly shutsdown the consumer
func (jc *JobConsumer) Stop() {
	log.Println("[JobConsumer] Stop method called. Signaling 'done' channel.")
	jc.isStopping = true // Indicate that shutdown is intentional
	// Non-blocking send to done channel
	select {
	case jc.done <- true:
		log.Println("[JobConsumer] 'done' signal sent successfully.")
	default:
		log.Println("[JobConsumer] 'done' channel already signaled or cannot send (consumer might be already stopped).")
	}
}

// Close cleanup the rabbitMQ resources.
func (jc *JobConsumer) Close() {
	log.Println("[JobConsumer] Close method called. Closing RabbitMQ channel and connection resources...")
	if jc.channel != nil {
		log.Println("[JobConsumer] Attempting to close channel...")
		err := jc.channel.Close()
		if err != nil {
			log.Printf("[JobConsumer] Error closing channel: %v", err)
		} else {
			log.Println("[JobConsumer] Channel closed.")
		}
		jc.channel = nil // Clear reference
	}
	if jc.conn != nil {
		log.Println("[JobConsumer] Attempting to close connection...")
		err := jc.conn.Close()
		if err != nil {
			log.Printf("[JobConsumer] Error closing connection: %v", err)
		} else {
			log.Println("[JobConsumer] Connection closed.")
		}
		jc.conn = nil // Clear reference
	}
	log.Println("[JobConsumer] RabbitMQ resources closed by Close().")
}
