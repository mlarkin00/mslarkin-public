package main

import (
	monitoring "cloud.google.com/go/monitoring/apiv3/v2"
	monitoringpb "cloud.google.com/go/monitoring/apiv3/v2/monitoringpb"
	"context"
	"fmt"
	"math"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	run "cloud.google.com/go/run/apiv2"
	runpb "cloud.google.com/go/run/apiv2/runpb"
	"google.golang.org/api/iterator"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Create channel to listen for signals.
var signalChan chan (os.Signal) = make(chan os.Signal, 1)
var lastUpdate time.Time

var topicId string = "my-topic"
var subscriptionId string = "my-pull-subscription"
var projectId string = "my-project-id"

var subscriberServiceName string = "my-subscriber-service"
var subscriberRegion string = "us-central1"

// Configure scaling metric targets and instance capacity
var rateUtilizationTarget float64 = 1
var targetRange float64 = .1
var targetAckLatencyMs float64 = 2000
var instanceCapacity int32 = 1000

// Time to wait, after a change, before making any other changes
var updateDelayMin = 5

func main() {
	// SIGINT handles Ctrl+C locally.
	// SIGTERM handles Cloud Run termination signal.
	signal.Notify(signalChan, syscall.SIGINT, syscall.SIGTERM)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	rateTargetEnv := os.Getenv("UTILIZATION_TARGET")
	if len(rateTargetEnv) > 0 {
		rateUtilizationTarget, _ = strconv.ParseFloat(rateTargetEnv, 64)
	}
	targetRangeEnv := os.Getenv("TARGET_RANGE")
	if len(targetRangeEnv) > 0 {
		targetRange, _ = strconv.ParseFloat(rateTargetEnv, 64)
	}
	targetAckLatencyEnv := os.Getenv("TARGET_ACK_LATENCY_MS")
	if len(targetAckLatencyEnv) > 0 {
		targetAckLatencyMs, _ = strconv.ParseFloat(rateTargetEnv, 64)
	}
	instanceCapacityEnv := os.Getenv("INSTANCE_CAPACITY")
	if len(instanceCapacityEnv) > 0 {
		ic, _ := strconv.Atoi(rateTargetEnv)
		instanceCapacity = int32(ic)
	}

	go func() {
		for {
			// Autoscaling logic
			scalingCheck(ctx)
			time.Sleep(30 * time.Second)
		}
	}()

	// Receive output from signalChan.
	sig := <-signalChan
	fmt.Printf("%s signal caught\n", sig)
}

//////////////////
// Autoscaling //
/////////////////

func scalingCheck(ctx context.Context) {
	var recommendedInstances int32

	subscriberService, _ := getRunService(ctx, subscriberServiceName, projectId, subscriberRegion)
	configuredInstances := subscriberService.Scaling.MinInstanceCount
	currentInstances := getInstanceCount(ctx, subscriberServiceName, projectId, subscriberRegion)
	lastUpdate = subscriberService.UpdateTime.AsTime()
	fmt.Printf("Service: %s\nInstances: Configured: %v | Current: %v\nLast Updated: %v\n",
		subscriberServiceName, configuredInstances, currentInstances, lastUpdate)

	// Get metrics
	publishRate := getPublishRate(ctx, topicId, projectId)
	ackRate := getAckRate(ctx, subscriptionId, projectId)
	rateUtilization := publishRate / ackRate
	ackLatencyMs := getAckLatencyMs(ctx, subscriptionId, projectId)
	messageBacklog := getMessageBacklog(ctx, subscriptionId, projectId)

	fmt.Printf("CHECK: Pub Rate: %v, Ack Rate: %v, Utilization: %v\n", publishRate, ackRate, rateUtilization)
	pubAckRecommendation := utilizationValueRecommendation(rateUtilization, rateUtilizationTarget, targetRange, currentInstances)
	if pubAckRecommendation != configuredInstances {
		fmt.Printf("CHECK: Recommended Instance change (Pub/Ack Delta): %v --> %v\n", configuredInstances, pubAckRecommendation)
	}

	fmt.Printf("\nCHECK: Ack Latency (ms): %v, Target (ms): %v\n", ackLatencyMs, targetAckLatencyMs)
	ackLatencyRecommendation := averageValueRecommendation(ackLatencyMs, targetAckLatencyMs, currentInstances)
	if ackLatencyRecommendation != configuredInstances {
		fmt.Printf("CHECK: Recommended Instance change (Ack Latency): %v --> %v\n", configuredInstances, ackLatencyRecommendation)
	}

	fmt.Printf("\nCHECK: Backlog (# messages): %v, Instance Capacity: %v\n", messageBacklog, instanceCapacity)
	capacityRecommendation := instanceCapacityRecommendation(messageBacklog, instanceCapacity)
	if capacityRecommendation != configuredInstances {
		fmt.Printf("CHECK: Recommended Instance change (Backlog): %v --> %v\n", configuredInstances, capacityRecommendation)
	}

	if pubAckRecommendation > configuredInstances {
		recommendedInstances = max(pubAckRecommendation, ackLatencyRecommendation, capacityRecommendation)
	} else {
		recommendedInstances = max(ackLatencyRecommendation, capacityRecommendation)
	}

	if recommendedInstances != configuredInstances {
		fmt.Printf("--------\nCHECK: Recommended Instance change (Final): %v --> %v\n------\n", configuredInstances, recommendedInstances)
	}

	// If the recommendation is greater than the current configuration, and the change delay has expired
	// OR if the recommendation is lower than the current configuration
	if recommendedInstances > configuredInstances &&
		time.Since(subscriberService.UpdateTime.AsTime()) > (time.Duration(updateDelayMin)*time.Minute) ||
		configuredInstances > recommendedInstances {
		fmt.Printf("Time since last change: %v\n", time.Since(subscriberService.UpdateTime.AsTime()))
		fmt.Printf("Recommended Instance change: %v --> %v\n", configuredInstances, recommendedInstances)
		fmt.Println("Updating Instances...")
		updateInstanceCount(ctx, subscriberService, recommendedInstances)
		fmt.Println("Instance count updated")
	}

}

func utilizationValueRecommendation(utilizationValue float64, utilizationTarget float64, targetRange float64, currentInstanceCount int32) int32 {
	var recommendedInstances int32
	if utilizationValue > (utilizationTarget+targetRange) || utilizationValue < (utilizationTarget-targetRange) {
		recommendedInstances = int32(math.Round(float64(currentInstanceCount)*utilizationValue) * utilizationTarget)
	} else {
		recommendedInstances = currentInstanceCount
	}
	return int32(recommendedInstances)
}

func averageValueRecommendation(metricValue float64, metricTarget float64, currentInstanceCount int32) int32 {
	recommendedInstances := math.Ceil(float64(currentInstanceCount) * (metricValue / metricTarget))
	return int32(recommendedInstances)
}

func instanceCapacityRecommendation(metricValue int32, instanceCapacity int32) int32 {
	recommendedInstances := math.Ceil(float64(metricValue) / float64(instanceCapacity))
	return int32(recommendedInstances)
}

//////////////////////////
// Cloud Run management //
//////////////////////////

func getInstanceCount(ctx context.Context, service string, projectId string, region string) int32 {
	monitoringMetric := "run.googleapis.com/container/instance_count"
	aggregationSeconds := 60
	metricDelaySeconds := 180
	groupBy := []string{"resource.labels.service_name"}
	metricFilter := fmt.Sprintf("metric.type=\"%s\""+
		" AND resource.labels.service_name =\"%s\""+
		" AND resource.labels.location =\"%s\"",
		monitoringMetric, service, region)

	metricData := getMetricMean(ctx,
		metricFilter,
		metricDelaySeconds,
		aggregationSeconds,
		groupBy,
		projectId)

	return int32(metricData[0].GetValue().GetDoubleValue())
}

func getRunService(ctx context.Context,
	service string,
	projectId string,
	region string) (*runpb.Service, error) {
	client, err := run.NewServicesClient(ctx)
	if err != nil {
		fmt.Printf("Error getting client:\n")
		panic(err)
	}
	defer client.Close()

	serviceId := "projects/" + projectId + "/locations/" + region + "/services/" + service

	req := &runpb.GetServiceRequest{Name: serviceId}
	resp, err := client.GetService(ctx, req)
	if err != nil {
		fmt.Printf("Error getting service: %s in %s (Project ID: %s)\n", service, region, projectId)
		panic(err)
	}
	return resp, err

}

func updateInstanceCount(ctx context.Context, service *runpb.Service, desiredMinInstances int32) {
	// Return is the current min-instances setting is the same as the desired min-instances
	if service.Scaling.MinInstanceCount == desiredMinInstances {
		return
	}

	client, err := run.NewServicesClient(ctx)
	if err != nil {
		fmt.Printf("Error getting client:\n")
		panic(err)
	}
	defer client.Close()

	service.Scaling.MinInstanceCount = int32(desiredMinInstances)

	var messageType *runpb.Service
	updateMask, err := fieldmaskpb.New(messageType, "scaling.min_instance_count")
	if err != nil {
		fmt.Printf("Error in update service request:\n")
		panic(err)
	}

	req := &runpb.UpdateServiceRequest{
		Service:    service,
		UpdateMask: updateMask,
	}
	op, err := client.UpdateService(ctx, req)
	if err != nil {
		fmt.Printf("Error in update service request:\n")
		panic(err)
	}

	resp, err := op.Wait(ctx)
	if err != nil {
		fmt.Printf("Error updating service:\n")
		panic(err)
	}
	_ = resp
}

//////////////////////////////
// Cloud Monitoring metrics //
//////////////////////////////

func getMessageBacklog(ctx context.Context, subscriptionId string, projectId string) int32 {
	monitoringMetric := "pubsub.googleapis.com/subscription/num_undelivered_messages"
	aggregationSeconds := 60
	metricDelaySeconds := 120
	groupBy := []string{"resource.labels.subscription_id"}
	metricFilter := fmt.Sprintf("metric.type=\"%s\""+
		" AND resource.labels.subscription_id =\"%s\"",
		monitoringMetric, subscriptionId)

	metricData := getMetricMean(ctx,
		metricFilter,
		metricDelaySeconds,
		aggregationSeconds,
		groupBy,
		projectId)

	return int32(metricData[0].GetValue().GetDoubleValue())
}

func getPublishRate(ctx context.Context, topicId string, projectId string) float64 {
	monitoringMetric := "pubsub.googleapis.com/topic/message_sizes"
	aggregationSeconds := 60
	metricDelaySeconds := 240
	groupBy := []string{"resource.labels.topic_id"}
	metricFilter := fmt.Sprintf("metric.type=\"%s\""+
		" AND resource.labels.topic_id =\"%s\"",
		monitoringMetric, topicId)

	metricData := getMetricDelta(ctx,
		metricFilter,
		metricDelaySeconds,
		aggregationSeconds,
		groupBy,
		projectId)

	messagesPerMinute := float64(metricData[0].Value.GetDistributionValue().Count)

	//Return the latest rate datapoint (converted to seconds)
	return messagesPerMinute / 60
}

func getAckRate(ctx context.Context, subscriptionId string, projectId string) float64 {
	monitoringMetric := "pubsub.googleapis.com/subscription/ack_message_count"
	aggregationSeconds := 60
	metricDelaySeconds := 240
	groupBy := []string{"resource.labels.subscription_id"}
	metricFilter := fmt.Sprintf("metric.type=\"%s\""+
		" AND resource.labels.subscription_id =\"%s\"",
		monitoringMetric, subscriptionId)

	metricData := getMetricRate(ctx,
		metricFilter,
		metricDelaySeconds,
		aggregationSeconds,
		groupBy,
		projectId)

	//Return the latest rate datapoint
	return float64(metricData[0].GetValue().GetDoubleValue())
}

func getAckLatencyMs(ctx context.Context, subscriptionId string, projectId string) float64 {
	monitoringMetric := "pubsub.googleapis.com/subscription/ack_latencies"
	aggregationSeconds := 60
	metricDelaySeconds := 240
	groupBy := []string{"resource.labels.subscription_id"}
	metricFilter := fmt.Sprintf("metric.type=\"%s\""+
		" AND resource.labels.subscription_id =\"%s\"",
		monitoringMetric, subscriptionId)

	//TODO: Implement Distribution metric mean function
	metricData := getMetricDelta(ctx,
		metricFilter,
		metricDelaySeconds,
		aggregationSeconds,
		groupBy,
		projectId)

	//Return the latest rate datapoint
	return float64(metricData[0].GetValue().GetDistributionValue().Mean)
}

func getMetricRate(ctx context.Context,
	resourceFilter string,
	metricDelaySeconds int,
	aggregationSeconds int,
	groupBy []string,
	projectId string) []monitoringpb.Point {
	client, err := monitoring.NewMetricClient(ctx)
	if err != nil {
		panic(err)
	}
	defer client.Close()

	//Configure metric aggregation
	aggregationStruct := &monitoringpb.Aggregation{
		CrossSeriesReducer: monitoringpb.Aggregation_REDUCE_SUM,
		PerSeriesAligner:   monitoringpb.Aggregation_ALIGN_RATE,
		GroupByFields:      groupBy,
		AlignmentPeriod: &durationpb.Duration{
			Seconds: int64(aggregationSeconds),
		},
	}

	//Configure metric interval
	startTime := time.Now().UTC().Add(time.Second * -time.Duration(metricDelaySeconds+aggregationSeconds))
	endTime := time.Now().UTC()

	interval := &monitoringpb.TimeInterval{
		StartTime: &timestamppb.Timestamp{
			Seconds: startTime.Unix(),
		},
		EndTime: &timestamppb.Timestamp{
			Seconds: endTime.Unix(),
		},
	}

	req := &monitoringpb.ListTimeSeriesRequest{
		Name:        "projects/" + projectId,
		Filter:      resourceFilter,
		Interval:    interval,
		Aggregation: aggregationStruct,
	}

	// Get the time series data.
	it := client.ListTimeSeries(ctx, req)
	var data []monitoringpb.Point
	for {
		resp, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			// Handle error.
			panic(err)
		}
		// Use resp.
		for _, point := range resp.GetPoints() {
			data = append(data, *point)
		}
	}
	return data
}

func getMetricMean(ctx context.Context,
	resourceFilter string,
	metricDelaySeconds int,
	aggregationSeconds int,
	groupBy []string,
	projectId string) []monitoringpb.Point {
	client, err := monitoring.NewMetricClient(ctx)
	if err != nil {
		panic(err)
	}
	defer client.Close()

	//Configure metric aggregation
	aggregationStruct := &monitoringpb.Aggregation{
		CrossSeriesReducer: monitoringpb.Aggregation_REDUCE_SUM,
		PerSeriesAligner:   monitoringpb.Aggregation_ALIGN_MEAN,
		GroupByFields:      groupBy,
		AlignmentPeriod: &durationpb.Duration{
			Seconds: int64(aggregationSeconds),
		},
	}

	///Configure metric interval
	startTime := time.Now().UTC().Add(time.Second * -time.Duration(metricDelaySeconds+aggregationSeconds))
	endTime := time.Now().UTC()

	interval := &monitoringpb.TimeInterval{
		StartTime: &timestamppb.Timestamp{
			Seconds: startTime.Unix(),
		},
		EndTime: &timestamppb.Timestamp{
			Seconds: endTime.Unix(),
		},
	}

	req := &monitoringpb.ListTimeSeriesRequest{
		Name:        "projects/" + projectId,
		Filter:      resourceFilter,
		Interval:    interval,
		Aggregation: aggregationStruct,
	}

	// Get the time series data.
	it := client.ListTimeSeries(ctx, req)
	var data []monitoringpb.Point
	for {
		resp, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			// Handle error.
			panic(err)
		}
		// Use resp.
		for _, point := range resp.GetPoints() {
			data = append(data, *point)
		}
	}
	return data
}

func getMetricDelta(ctx context.Context,
	resourceFilter string,
	metricDelaySeconds int,
	aggregationSeconds int,
	groupBy []string,
	projectId string) []monitoringpb.Point {
	client, err := monitoring.NewMetricClient(ctx)
	if err != nil {
		panic(err)
	}
	defer client.Close()

	//Configure metric aggregation
	aggregationStruct := &monitoringpb.Aggregation{
		PerSeriesAligner: monitoringpb.Aggregation_ALIGN_DELTA,
		GroupByFields:    groupBy,
		AlignmentPeriod: &durationpb.Duration{
			Seconds: int64(aggregationSeconds),
		},
	}

	//Configure metric interval
	startTime := time.Now().UTC().Add(time.Second * -time.Duration(metricDelaySeconds+aggregationSeconds))
	endTime := time.Now().UTC()

	interval := &monitoringpb.TimeInterval{
		StartTime: &timestamppb.Timestamp{
			Seconds: startTime.Unix(),
		},
		EndTime: &timestamppb.Timestamp{
			Seconds: endTime.Unix(),
		},
	}

	req := &monitoringpb.ListTimeSeriesRequest{
		Name:        "projects/" + projectId,
		Filter:      resourceFilter,
		Interval:    interval,
		Aggregation: aggregationStruct,
	}

	// Get the time series data.
	it := client.ListTimeSeries(ctx, req)
	var data []monitoringpb.Point
	for {
		resp, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			// Handle error.
			panic(err)
		}
		// Use resp.
		for _, point := range resp.GetPoints() {
			data = append(data, *point)
		}
	}
	return data
}
