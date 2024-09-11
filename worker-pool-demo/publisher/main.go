package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"cloud.google.com/go/pubsub"
)

// /////////////////////////
// Configure environment //
// /////////////////////////
var topicId string = os.Getenv("TOPIC_ID")     //"my-pubsub-topic"
var projectId string = os.Getenv("PROJECT_ID") //"my-project-id"
var messagesPerSecond = 1000                   //Number of messages per second to publish
///////////////////////////

// Create channel to listen for signals.
var signalChan chan (os.Signal) = make(chan os.Signal, 1)

func main() {
	// SIGINT handles Ctrl+C locally.
	// SIGTERM handles Cloud Run termination signal.
	signal.Notify(signalChan, syscall.SIGINT, syscall.SIGTERM)
	ctx := context.Background()

	// Set up HTTP listener for manually publishing messages
	http.HandleFunc("/publish-message", publishHandler)

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

	client, err := pubsub.NewClient(ctx, projectId)
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()

	msg := "Hello World (auto-generated)"
	topic := client.Topic(topicId)

	// Continually publish messages in the background
	go func() {
		fmt.Printf("Publishing %v messages per second\n", messagesPerSecond)
		for {
			start := time.Now()
			//Publish messages based on configured messagesPerSecond
			for b := 0; b < messagesPerSecond; b++ {
				go publishMessage(ctx, topic, msg)
			}
			sendTime := time.Since(start)
			//Sleep for time remaining until end of 1s period, or continue immediately
			sleepTime := max((1*time.Second)-sendTime, 0*time.Second)
			time.Sleep(sleepTime)
			select {
			case <-ctx.Done():
				return
			default:
			}
		}
	}()

	// Receive output from signalChan.
	sig := <-signalChan
	fmt.Printf("%s signal caught\n", sig)
}

func publishMessage(ctx context.Context, topic *pubsub.Topic, message string) string {

	result := topic.Publish(ctx, &pubsub.Message{
		Data: []byte(message),
	})
	// Block until the result is returned and a server-generated
	// ID is returned for the published message (discarded).
	id, err := result.Get(ctx)
	if err != nil {
		fmt.Println(fmt.Errorf("error publishing message: %w", err))
	}
	return id
}

// Manually publish a message with HTTP request
func publishHandler(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	client, err := pubsub.NewClient(ctx, projectId)
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()

	msg := "Hello World (manually published)"
	topic := client.Topic(topicId)
	msgId := publishMessage(ctx, topic, msg)

	fmt.Fprintf(w, "Message ID %v published", msgId)
}
