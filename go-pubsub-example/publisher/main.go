package main

import (
	"context"
	"fmt"
	"log"
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
var messagesPerSecond = 200 //Number of messages per second to publish
var messagesPerBatch = 1    //Set to 1 to disable batch publishing
///////////////////////////

// Create channel to listen for signals.
var signalChan chan (os.Signal) = make(chan os.Signal, 1)

func main() {
	// SIGINT handles Ctrl+C locally.
	// SIGTERM handles Cloud Run termination signal.
	signal.Notify(signalChan, syscall.SIGINT, syscall.SIGTERM)
	ctx := context.Background()

	//Get configured messagesPerSecond
	mps := os.Getenv("MESSAGES_PER_SECOND")
	if len(mps) > 0 {
		messagesPerSecond, _ = strconv.Atoi(mps)
	}

	client, err := pubsub.NewClient(ctx, projectId)
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()

	msg := "Hello World (generated)"
	topic := client.Topic(topicId)
	topic.PublishSettings.CountThreshold = messagesPerBatch

	go func() {
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

func publishMessage(ctx context.Context, topic *pubsub.Topic, message string) {

	result := topic.Publish(ctx, &pubsub.Message{
		Data: []byte(message),
	})
	// Block until the result is returned and a server-generated
	// ID is returned for the published message.
	id, err := result.Get(ctx)
	if err != nil {
		fmt.Println(fmt.Errorf("Get: %w", err))
	}
	fmt.Printf("Published message ID: %v\n", id)
}
