package main

import (
	"cloud.google.com/go/pubsub"
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"
)

// /////////////////////////
// Configure environment //
// /////////////////////////
var subscriptionId string = os.Getenv("SUBSCRIPTION_ID") //"my-pull-subscription"
var projectId string = os.Getenv("PROJECT_ID")           //"my-project-id"
var processingDelayMs = 1000                             //Delay to simulate message processing time
var maxOutstanding = 1000                                //Maximum number of concurrent messages
///////////////////////////

// Create channel to listen for signals.
var signalChan chan (os.Signal) = make(chan os.Signal, 1)

func main() {
	// SIGINT handles Ctrl+C locally.
	// SIGTERM handles Cloud Run termination signal.
	signal.Notify(signalChan, syscall.SIGINT, syscall.SIGTERM)
	ctx := context.Background()

	// Listen for default TCP startup probe on PORT, and then close connection
	go func() {
		// Determine port for HTTP service.
		port := os.Getenv("PORT")
		if port == "" {
			port = "8080"
			fmt.Printf("defaulting to port %s\n", port)
		}
		// Listen on PORT for TCP startup probe
		startupListener, err := net.Listen("tcp", ":"+port)
		fmt.Printf("Listening for TCP startup probe on %s\n", startupListener.Addr().String())
		if err != nil {
			log.Fatal(err)
		}
		defer startupListener.Close()
		// Accept incoming probe connection, then close
		conn, err := startupListener.Accept()
		if err != nil {
			log.Fatal(err)
		}
		conn.Close()
		fmt.Printf("Closing TCP connection and shutting down listener\n")
	}()

	maxOutstandingEnv := os.Getenv("MAX_CONCURRENT_MESSAGES")
	if len(maxOutstandingEnv) > 0 {
		maxOutstanding, _ = strconv.Atoi(maxOutstandingEnv)
	}
	processDelayEnv := os.Getenv("PROCESS_DELAY_MS")
	if len(processDelayEnv) > 0 {
		processingDelayMs, _ = strconv.Atoi(processDelayEnv)
	}

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

	// MaxOutstandingMessages limits the number of concurrent handlers of messages.
	// In this case, up to [maxOutstanding] unacked messages can be handled concurrently.
	// Note, even in synchronous mode, messages pulled in a batch can still be handled
	// concurrently.
	fmt.Printf("Configuring for %v concurrent messages\n", maxOutstanding)
	sub.ReceiveSettings.MaxOutstandingMessages = maxOutstanding

	err = sub.Receive(ctx, handleMessage)
	if err != nil {
		return fmt.Errorf("sub.Receive: %w", err)
	}

	return nil
}

func handleMessage(ctx context.Context, m *pubsub.Message) {
	// fmt.Println("Got message:", string(m.Data))

	// Sleep to emulate processing time
	time.Sleep(time.Duration(processingDelayMs) * time.Millisecond)
	m.Ack()
}
