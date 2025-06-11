package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/AungKyawPhyo1142/sentria-go/internal/config"
	"github.com/AungKyawPhyo1142/sentria-go/internal/core/service"
	"github.com/AungKyawPhyo1142/sentria-go/internal/models"
	amqp "github.com/rabbitmq/amqp091-go"
)

const (
	consumerTag             = "sentria-factchecker-consumer"
	connectRetryDelay       = 5 * time.Second
	maxConnectRetries       = 10
	factCheckTimeout        = 60 * time.Second
	factCheckPublishTimeout = 30 * time.Second
	fa                      = factCheckPublishTimeout
	defaultConcurrency      = 10 // number of concurrent workers
)

// JobConsumer is a struct that consumes jobs from a queue
type JobConsumer struct {
	conn                *amqp.Connection
	channel             *amqp.Channel
	cfg                 *config.Config
	factCheckService    *service.FactCheckService
	done                chan bool // channel to signal when the consumer is done
	isStopping          bool
	notifyConnClose     chan *amqp.Error
	notifyChanClose     chan *amqp.Error
	processingSemaphore chan struct{}  // semaphore to limit concurrent processing
	wg                  sync.WaitGroup // to wait for all workers to finish
	concurrency         int            // max concurrent go-routines/workers
}

func NewJobConsumer(cfg *config.Config, fcService *service.FactCheckService, concurrency int) (*JobConsumer, error) {

	if cfg == nil {
		return nil, fmt.Errorf("cfg cannot be nil")
	}

	if fcService == nil {
		return nil, fmt.Errorf("factCheckService cannot be nil")
	}

	if concurrency <= 0 {
		concurrency = defaultConcurrency
	}

	jc := &JobConsumer{
		cfg:                 cfg,
		factCheckService:    fcService, // pass the factcheck service here,
		done:                make(chan bool),
		concurrency:         concurrency,
		processingSemaphore: make(chan struct{}, concurrency), // initialize with concurrency workers
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
}

func (jc *JobConsumer) PublishFactCheckResult(ctx context.Context, result *models.FactCheckResult) error {
	if jc.channel == nil || jc.conn == nil || jc.conn.IsClosed() {
		log.Println("[JobConsumer-Result] Channel or connection is nil or closed. Attempting to reconnect before publishing.")
		if err := jc.handleReconnect(ctx); err != nil {
			log.Printf("[JobConsumer-Result] Failed to reconnect during PublishFactCheckResult: %v", err)
			return fmt.Errorf("failed to reconnect during PublishFactCheckResult: %w", err)
		}
	}

	_, err := jc.channel.QueueDeclare(
		jc.cfg.Factcheck_Result_Queue_Name, // queue name
		true,                               // durable
		false,                              // delete when unused
		false,                              // exclusive
		false,                              // no-wait
		nil,                                // arguments
	)

	if err != nil {
		log.Printf("[JobConsumer-Result] Failed to declare a queue: %v", err)
		return fmt.Errorf("[JobConsumer-Result] failed to declare a queue: %v", err)
	}

	body, err := json.Marshal(result)
	if err != nil {
		log.Printf("[JobConsumer-Result] Failed to marshal result: %v", err)
		return fmt.Errorf("[JobConsumer-Result] failed to marshal result: %v", err)
	}

	err = jc.channel.PublishWithContext(
		ctx,
		"",
		jc.cfg.Factcheck_Result_Queue_Name, // routing key
		false,                              // mandatory
		false,                              // immediate
		amqp.Publishing{
			ContentType:  "application/json",
			Body:         body,
			DeliveryMode: amqp.Persistent, // make message persistent
			Timestamp:    time.Now(),
			MessageId:    result.PostgresReportID,
		},
	)

	if err != nil {
		log.Printf("[JobConsumer-Result] Failed to publish a message: %v", err)
		return fmt.Errorf("[JobConsumer-Result] failed to publish a message: %v", err)
	}
	log.Printf("[JobConsumer-Result] Published a message with ID: %s", result.PostgresReportID)
	return nil
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
		jc.concurrency, // prefetch count: How many messages the server will deliver, at most, at a time.
		0,              // prefetch size: Server will try to keep at least this many bytes undelivered. (0 means no specific limit)
		false,          // global: false means QoS applies per new consumer (this channel)
	)
	if err != nil {
		log.Printf("[JobConsumer] Failed to set QoS: %v", err)
	} else {
		log.Printf("[JobConsumer] Set QoS set to prefetch count: 1")
	}
	return nil // success
}

// processMessage handles a single message in its own go routine
func (jc *JobConsumer) processMessage(ctx context.Context, d amqp.Delivery) {
	defer func() {
		<-jc.processingSemaphore // release the semaphore when the worker is done
		jc.wg.Done()             // decrement the wait group counter
		if r := recover(); r != nil {
			log.Printf("[JobConsumer] CRITICAL: Panic recovered in processMessage for delivery %d: %v. Nacking message.", d.DeliveryTag, r)
			// Nack the message if a panic occurs during processing.
			if nackErr := d.Nack(false, false); nackErr != nil {
				log.Printf("[JobConsumer] Error Nacking message %d after panic: %v", d.DeliveryTag, nackErr)
			}
		}
	}()
	log.Printf("[JobConsumer] Received a message with delivery tag: %d, message id: %s", d.DeliveryTag, d.MessageId)

	// unmarshal into models.DisasterReportData and pass to factCheckService
	var reportData models.DisasterReportData
	if errUnmarshal := json.Unmarshal(d.Body, &reportData); errUnmarshal != nil {
		log.Printf("[JobConsumer] Failed to unmarshal message into DisasterReportData: %v, Body: %s", errUnmarshal, string(d.Body))
		if nackErr := d.Nack(false, false); nackErr != nil {
			log.Printf("[JobConsumer] Error Nacking unparseable message %d: %v", d.DeliveryTag, nackErr)
		}
		return
	}

	log.Printf("[JobConsumer] Successfully unmarshaled. PG_ID: %s, MongoDocID: %s, ReportName: %s, Type: %s, Timestamp: %s",
		reportData.PostgresReportID, reportData.MongoDocID, reportData.ReportName, reportData.IncidentType, reportData.IncidentTimestamp.Format(time.RFC3339))

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
		return
	}

	log.Printf("[JobConsumer] FactcheckingService completed for report %s. Result: %v", reportData.PostgresReportID, factCheckResult)

	// * Publish FactCheckResult
	log.Printf("[JobConsumer] Publishing fact check result for report %s...", reportData.PostgresReportID)
	publishCtx, publishCancel := context.WithTimeout(context.Background(), factCheckPublishTimeout)
	defer publishCancel()
	if err := jc.PublishFactCheckResult(publishCtx, factCheckResult); err != nil {
		log.Printf("[JobConsumer] Failed to publish fact check result for report %s: %v", reportData.PostgresReportID, err)
		// Nack the message if publishing fails
		if nackErr := d.Nack(false, true); nackErr != nil {
			log.Printf("[JobConsumer] Error Nacking message %d: %v", d.DeliveryTag, nackErr)
		} else {
			log.Printf("[JobConsumer] Successfully Nacked message %d", d.DeliveryTag)
		}
		return
	}

	if errAck := d.Ack(false); errAck != nil {
		log.Printf("[JobConsumer] Error Acknowledging message %d: %v", d.DeliveryTag, errAck)
	} else {
		log.Printf("[JobConsumer] Successfully Acknowledged message %d", d.DeliveryTag)
	}
}

// Consume starts consuming jobs from the queue
// this method will block until error occurs or Stop is called
func (jc *JobConsumer) StartConsuming(ctx context.Context) {

	if jc.channel == nil || jc.conn == nil || jc.conn.IsClosed() {
		log.Println("[JobConsumer] Channel or connection is nil or closed. Attempting to reconnect before consuming.")
		if err := jc.handleReconnect(ctx); err != nil {
			log.Printf("[JobConsumer] FATAL: Failed to reconnect during StartConsuming: %v. Consumer stopping.", err)
			// No need to signal jc.done here, as this goroutine will exit.
			// The main application might need a way to know this startup failed.
			return
		}
	}

	log.Printf("[JobConsumer] Starting to consume messages from queue: %s", jc.cfg.Factcheck_Queue_Name)

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

		case <-ctx.Done(): // application is shutting down
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
			jc.wg.Add(1)                         // increment the wait group counter
			jc.processingSemaphore <- struct{}{} // acquire the semaphore slot (blocks if full)
			go jc.processMessage(ctx, d)         // process the message in a new goroutine

		case err := <-jc.notifyConnClose:
			log.Printf("[JobConsumer] Connection closed event received: %v", err)
			jc.clearNotifications()
			if !jc.isStopping {
				if reconnErr := jc.handleReconnect(ctx); reconnErr != nil {
					log.Printf("[JobConsumer] FATAL: Failed to reconnect after connection closed: %v. Consumer stopping.", reconnErr)
					return
				}
			}
			return

		case <-jc.done: // if stop() was called or connetion/channel error
			log.Printf("[JobConsumer] Stop signal received. Exiting consumer.")
			return
		}
	}
}

func (jc *JobConsumer) clearNotifications() {
	if jc.notifyConnClose != nil {
		close(jc.notifyConnClose) // close to stop go-routines potentially reading from old ones
		jc.notifyConnClose = nil
	}
	if jc.notifyChanClose != nil {
		close(jc.notifyChanClose) // close to stop go-routines potentially reading from old ones
		jc.notifyChanClose = nil
	}
}

func (jc *JobConsumer) handleReconnect(ctx context.Context) error {
	log.Printf("[JobConsumer] Attempting to reconnect...")
	jc.clearNotifications()
	if jc.channel != nil {
		jc.channel.Close()
		jc.channel = nil
	}
	if jc.conn != nil {
		jc.conn.Close()
		jc.conn = nil
	}

	for i := 0; i < maxConnectRetries; i++ {
		if jc.isStopping || ctx.Err() != nil {
			return fmt.Errorf("reconnect aborted: %w", ctx.Err())
		}
		log.Printf("[JobConsumer] Reconnect attempt %d...", i+1)
		// attempt to reconnect
		if err := jc.connect(); err == nil {
			log.Printf("[JobConsumer] Reconnect successful.")
			go jc.StartConsuming(ctx) //! IMPORTANT: pass the original context to StartConsuming
			return nil
		}
		log.Printf("[JobConsumer] Reconnect failed. Retrying in %v...", connectRetryDelay)
		select {
		case <-time.After(connectRetryDelay):
		case <-ctx.Done():
			return ctx.Err()
		case <-jc.done:
			return fmt.Errorf("reconnect aborted: %w", ctx.Err())
		}
	}

	return fmt.Errorf("failed to reconnect after %d attempts", maxConnectRetries) //! IMPORTAN

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
	log.Println("[JobConsumer] Close method called. Waiting for all processing goroutines to finish...")
	jc.wg.Wait() // Wait for all processing goroutines to finish
	log.Printf("[JobConsumer] All processing goroutines finished.")

	log.Println("[JobConsumer] Closing RabbitMQ channel and connection resources. ")
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
