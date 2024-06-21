package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"cloud.google.com/go/pubsub"
)

// /////////////////////////
// Configure environment //
// /////////////////////////
var topicId = "my-topic"
var projectId = "my-project-id"
var messagesPerSecond = 20 //Number of messages per second to publish
///////////////////////////

// Create channel to listen for signals.
var signalChan chan (os.Signal) = make(chan os.Signal, 1)

func main() {
	// SIGINT handles Ctrl+C locally.
	// SIGTERM handles Cloud Run termination signal.
	signal.Notify(signalChan, syscall.SIGINT, syscall.SIGTERM)

	http.HandleFunc("/", publishHandler)

	// Determine port for HTTP service.
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
		log.Printf("defaulting to port %s", port)
	}

	go func() {
		// Start HTTP server.
		if err := http.ListenAndServe(":"+port, nil); err != nil {
			log.Fatal(err)
		}
	}()

	mps := os.Getenv("MESSAGES_PER_SECOND")
	if len(mps) > 0 {
		messagesPerSecond, _ = strconv.Atoi(mps)
	}

	//Publish messages based on configured messagesPerSecond
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	for i := 0; i < messagesPerSecond; i++ {
		go func() {
			for {
				msg := "Hello World (generated)"
				err := publishMessage(ctx, projectId, topicId, msg)
				if err != nil {
					log.Fatal(err)

				}
				time.Sleep(1 * time.Second)
				select {
				case <-ctx.Done():
					return
				default:
				}
			}
		}()
	}

	// Receive output from signalChan.
	sig := <-signalChan
	fmt.Printf("%s signal caught\n", sig)
}

func publishHandler(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	err := publishMessage(ctx, projectId, topicId, "Hello World (handler)")
	if err != nil {
		log.Fatal(err)
	}

	fmt.Fprintf(w, "Message published")
}

func publishMessage(ctx context.Context, projectID, topicId string, message string) error {
	client, err := pubsub.NewClient(ctx, projectID)
	if err != nil {
		return fmt.Errorf("pubsub.NewClient: %w", err)
	}
	defer client.Close()

	topic := client.Topic(topicId)
	result := topic.Publish(ctx, &pubsub.Message{
		Data: []byte(message),
	})
	// Block until the result is returned and a server-generated
	// ID is returned for the published message.
	id, err := result.Get(ctx)
	if err != nil {
		return fmt.Errorf("Get: %w", err)
	}
	fmt.Printf("Published message ID: %v\n", id)
	return nil
}
