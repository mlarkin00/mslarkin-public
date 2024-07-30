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

var subscriptionId string = os.Getenv("SUBSCRIPTION_ID")               //"my-pull-subscription"
var projectId string = os.Getenv("PROJECT_ID")                         //"my-project-id"
var subscriberServiceName string = os.Getenv("SUBSCRIPTION_SERVICE")   //"my-subscriber-service"
var subscriberRegion string = os.Getenv("SUBSCRIPTION_SERVICE_REGION") //"us-central1"

// Flag to enable/disable autoscaling of Subscriber service
var enableAutoscaling bool = true
var maxScaleUpRate float64 = 1 // Limit scale-up per iteration (1 = 100%)

// Configure scaling metric targets and instance capacity
var targetAckLatencyMs float64 = 1200
var targetMaxAgeS float64 = 2
var instanceCapacity int32 = 1500

var checkDelayS = 60   // Frequency for metrics checks
var updateDelayMin = 5 // Time to wait, after a change, before making any other changes

func main() {
	// SIGINT handles Ctrl+C locally.
	// SIGTERM handles Cloud Run termination signal.
	signal.Notify(signalChan, syscall.SIGINT, syscall.SIGTERM)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	enableAutoscalingEnv := os.Getenv("ENABLE_AUTOSCALING")
	if len(enableAutoscalingEnv) > 0 {
		enableAutoscaling, _ = strconv.ParseBool(enableAutoscalingEnv)
	}
	maxScaleUpEnv := os.Getenv("MAX_SCALE_UP_RATE")
	if len(maxScaleUpEnv) > 0 {
		maxScaleUpRate, _ = strconv.ParseFloat(maxScaleUpEnv, 64)
	}
	targetAckLatencyEnv := os.Getenv("TARGET_ACK_LATENCY_MS")
	if len(targetAckLatencyEnv) > 0 {
		targetAckLatencyMs, _ = strconv.ParseFloat(targetAckLatencyEnv, 64)
	}
	maxAgeEnv := os.Getenv("MAX_AGE_S")
	if len(maxAgeEnv) > 0 {
		targetMaxAgeS, _ = strconv.ParseFloat(maxAgeEnv, 64)
	}
	instanceCapacityEnv := os.Getenv("INSTANCE_CAPACITY")
	if len(instanceCapacityEnv) > 0 {
		ic, _ := strconv.Atoi(instanceCapacityEnv)
		instanceCapacity = int32(ic)
	}

	updateDelayEnv := os.Getenv("UPDATE_DELAY_MIN")
	if len(updateDelayEnv) > 0 {
		updateDelayMin, _ = strconv.Atoi(updateDelayEnv)
	}
	checkDelayEnv := os.Getenv("CHECK_DELAY_S")
	if len(checkDelayEnv) > 0 {
		checkDelayS, _ = strconv.Atoi(checkDelayEnv)
	}

	go func() {
		for {
			// Autoscaling logic
			scalingCheck(ctx)
			time.Sleep(time.Duration(checkDelayS) * time.Second)
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
	timeSinceUpdate := time.Since(subscriberService.UpdateTime.AsTime())
	fmt.Printf("Service: %s | Instances: Configured: %v | Current: %v | Last Updated: %v ago\n",
		subscriberServiceName, configuredInstances, currentInstances, timeSinceUpdate)

	// Get metrics
	ackLatencyMs := getAckLatencyMs(ctx, subscriptionId, projectId)
	messageBacklog := getMessageBacklog(ctx, subscriptionId, projectId)
	// maxMessageAge := getMaxMessageAgeS(ctx, subscriptionId, projectId)

	// Get recommended instances for each metric
	ackLatencyRecommendation := averageValueRecommendation(ackLatencyMs, targetAckLatencyMs, currentInstances)
	capacityRecommendation := instanceCapacityRecommendation(messageBacklog, instanceCapacity)
	// maxAgeRecommendation := averageValueRecommendation(maxMessageAge, targetMaxAgeS, currentInstances)

	if ackLatencyRecommendation > configuredInstances {
		// recommendedInstances = max(ackLatencyRecommendation, maxAgeRecommendation)
		recommendedInstances = ackLatencyRecommendation
		// } else if maxAgeRecommendation < configuredInstances {
		// 	recommendedInstances = maxAgeRecommendation
	} else if capacityRecommendation < configuredInstances {
		recommendedInstances = capacityRecommendation
	} else {
		recommendedInstances = configuredInstances
	}

	// If the current instance count has caught up with configured instances
	// AND
	// If the recommendation is greater than the current configured instances AND the change delay has expired
	// OR the recommendation is lower than the current configuration
	if configuredInstances == currentInstances &&
		(recommendedInstances > configuredInstances &&
			time.Since(subscriberService.UpdateTime.AsTime()) > (time.Duration(updateDelayMin)*time.Minute) ||
			configuredInstances > recommendedInstances) {
		fmt.Printf("Time since last change: %v\n", time.Since(subscriberService.UpdateTime.AsTime()))
		// Only effect the change if autoscaling is enabled
		if enableAutoscaling {
			fmt.Printf("ACTUAL: Recommended Instance change: %v --> %v\n", configuredInstances, recommendedInstances)
			if float64((recommendedInstances-configuredInstances)/configuredInstances) > maxScaleUpRate {
				recommendedInstances = int32(math.Ceil((1 + maxScaleUpRate) * float64(configuredInstances)))
				fmt.Printf("Limiting scale-up to %v instances\n", recommendedInstances)
			}
			fmt.Println("Updating Instances...")
			updateInstanceCount(ctx, subscriberService, recommendedInstances)
			fmt.Println("Instance count updated")
		} else {
			fmt.Printf("DRY_RUN: Recommended Instance change: %v --> %v\n",
				configuredInstances, recommendedInstances)
		}
	}
}

func averageValueRecommendation(metricValue float64, metricTarget float64, currentInstanceCount int32) int32 {
	recommendedInstances := max(math.Ceil(float64(currentInstanceCount)*(metricValue/metricTarget)), 1)
	return int32(recommendedInstances)
}

func instanceCapacityRecommendation(metricValue int32, instanceCapacity int32) int32 {
	recommendedInstances := max(math.Ceil(float64(metricValue)/float64(instanceCapacity)), 1)
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

func getMaxMessageAgeS(ctx context.Context, subscriptionId string, projectId string) float64 {
	monitoringMetric := "pubsub.googleapis.com/subscription/oldest_unacked_message_age"
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

	return metricData[0].GetValue().GetDoubleValue()
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
