package main

import (
	"context"
	"fmt"
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
var subscriptionId = "my-pull-subscription"
var projectId = "my-project-id"
var processingDelayMs = 500 //Delay to simulate message processing time
var maxOutstanding = 1000   //Maximum number of concurrent messages
///////////////////////////

var maxMessages = os.Getenv("MAX_CONCURRENT_MESSAGES")

// Create channel to listen for signals.
var signalChan chan (os.Signal) = make(chan os.Signal, 1)

func main() {
	// SIGINT handles Ctrl+C locally.
	// SIGTERM handles Cloud Run termination signal.
	signal.Notify(signalChan, syscall.SIGINT, syscall.SIGTERM)
	ctx := context.Background()

	go func() {
		for {
			fmt.Println("Waiting for messages...")
			err := subscribeToPullQueue(ctx, projectId, subscriptionId)
			if err != nil {
				fmt.Printf("sub.Receive: %v", err)
			}
		}
	}()

	// Receive output from signalChan.
	sig := <-signalChan
	fmt.Printf("%s signal caught\n", sig)

}

func subscribeToPullQueue(ctx context.Context, projectId, subscriptionId string) error {
	client, err := pubsub.NewClient(ctx, projectId)
	if err != nil {
		return fmt.Errorf("pubsub.NewClient: %w", err)
	}
	defer client.Close()

	sub := client.Subscription(subscriptionId)
	cctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// MaxOutstandingMessages limits the number of concurrent handlers of messages.
	// In this case, up to [maxOutstanding] unacked messages can be handled concurrently.
	// Note, even in synchronous mode, messages pulled in a batch can still be handled
	// concurrently.
	if len(maxMessages) > 0 {
		maxOutstanding, _ = strconv.Atoi(maxMessages)
	}
	fmt.Printf("Configuring for %v concurrent messages\n", maxOutstanding)
	sub.ReceiveSettings.MaxOutstandingMessages = maxOutstanding

	err = sub.Receive(cctx, func(ctx context.Context, m *pubsub.Message) {
		// fmt.Println("Got message:", string(m.Data))

		// Sleep to emulate processing time
		time.Sleep(time.Duration(processingDelayMs) * time.Millisecond)
		m.Ack()
	})
	if err != nil {
		return fmt.Errorf("sub.Receive: %w", err)
	}

	return nil
}
