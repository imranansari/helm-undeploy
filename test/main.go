package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/joho/godotenv"
	"github.com/rs/zerolog"
	"github.com/helm-undeploy/activities"
	"github.com/helm-undeploy/workflows"
	"go.temporal.io/sdk/client"
)

func main() {
	var (
		releaseName   = flag.String("release", "", "Helm release name to undeploy")
		namespace     = flag.String("namespace", "default", "Kubernetes namespace")
		wait          = flag.Bool("wait", true, "Wait for resources to be deleted")
		timeout       = flag.Duration("timeout", 5*time.Minute, "Timeout for undeploy operation")
		temporalHost  = flag.String("temporal-host", "localhost:7233", "Temporal server host:port")
		taskQueue     = flag.String("task-queue", "helm-undeploy-queue", "Temporal task queue")
		workflowID    = flag.String("workflow-id", "", "Workflow ID (generated if not provided)")
	)
	flag.Parse()

	logger := zerolog.New(os.Stdout).With().Timestamp().Caller().Logger()

	if err := godotenv.Load(); err != nil {
		logger.Debug().Err(err).Msg("No .env file found")
	}

	if *releaseName == "" {
		logger.Fatal().Msg("Release name is required")
	}

	temporalClient, err := client.Dial(client.Options{
		HostPort: *temporalHost,
		Logger:   &temporalLogger{logger: logger},
	})
	if err != nil {
		logger.Fatal().Err(err).Msg("Unable to create Temporal client")
	}
	defer temporalClient.Close()

	request := activities.HelmUndeployRequest{
		ReleaseName: *releaseName,
		Namespace:   *namespace,
		Wait:        *wait,
		Timeout:     *timeout,
	}

	workflowOptions := client.StartWorkflowOptions{
		ID:        *workflowID,
		TaskQueue: *taskQueue,
	}
	if workflowOptions.ID == "" {
		workflowOptions.ID = fmt.Sprintf("helm-undeploy-%s-%s-%d", 
			request.ReleaseName, 
			request.Namespace, 
			time.Now().Unix())
	}

	logger.Info().
		Str("workflowID", workflowOptions.ID).
		Str("release", request.ReleaseName).
		Str("namespace", request.Namespace).
		Bool("wait", request.Wait).
		Dur("timeout", request.Timeout).
		Msg("Starting helm undeploy workflow")

	we, err := temporalClient.ExecuteWorkflow(context.Background(), workflowOptions, 
		workflows.HelmUndeployWorkflow, request)
	if err != nil {
		logger.Fatal().Err(err).Msg("Unable to execute workflow")
	}

	logger.Info().
		Str("workflowID", we.GetID()).
		Str("runID", we.GetRunID()).
		Msg("Workflow started")

	var result workflows.HelmUndeployResponse
	err = we.Get(context.Background(), &result)
	if err != nil {
		logger.Fatal().Err(err).Msg("Unable to get workflow result")
	}

	if result.Success {
		logger.Info().
			Str("message", result.Message).
			Time("timestamp", result.Timestamp).
			Msg("Helm undeploy completed successfully")
	} else {
		logger.Error().
			Str("message", result.Message).
			Time("timestamp", result.Timestamp).
			Msg("Helm undeploy failed")
		os.Exit(1)
	}
}

type temporalLogger struct {
	logger zerolog.Logger
}

func (l *temporalLogger) Debug(msg string, keyvals ...interface{}) {
	l.logger.Debug().Fields(keyvals).Msg(msg)
}

func (l *temporalLogger) Info(msg string, keyvals ...interface{}) {
	l.logger.Info().Fields(keyvals).Msg(msg)
}

func (l *temporalLogger) Warn(msg string, keyvals ...interface{}) {
	l.logger.Warn().Fields(keyvals).Msg(msg)
}

func (l *temporalLogger) Error(msg string, keyvals ...interface{}) {
	l.logger.Error().Fields(keyvals).Msg(msg)
}