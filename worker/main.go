package main

import (
	"fmt"
	"os"

	"github.com/joho/godotenv"
	"github.com/rs/zerolog"
	"github.com/helm-undeploy/activities"
	"github.com/helm-undeploy/workflows"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
)

func main() {
	logger := zerolog.New(os.Stdout).With().Timestamp().Caller().Logger()
	
	if err := godotenv.Load(); err != nil {
		logger.Debug().Err(err).Msg("No .env file found")
	}

	config := LoadConfig(logger)

	temporalClient, err := client.Dial(client.Options{
		HostPort:  config.TemporalHost,
		Namespace: config.TemporalNamespace,
	})
	if err != nil {
		logger.Fatal().Err(err).Msg("Unable to create Temporal client")
	}
	defer temporalClient.Close()

	activities := activities.NewActivities(logger)

	w := worker.New(temporalClient, config.TaskQueue, worker.Options{
		MaxConcurrentActivityExecutionSize: 10,
	})

	w.RegisterWorkflow(workflows.HelmUndeployWorkflow)
	w.RegisterActivity(activities.ValidateReleaseActivity)
	w.RegisterActivity(activities.UndeployReleaseActivity)  
	w.RegisterActivity(activities.VerifyUndeployActivity)

	logger.Info().
		Str("taskQueue", config.TaskQueue).
		Str("temporalHost", config.TemporalHost).
		Msg("Starting Temporal worker")

	err = w.Run(worker.InterruptCh())
	if err != nil {
		logger.Fatal().Err(err).Msg("Unable to start worker")
	}
}

type Config struct {
	TemporalHost      string
	TemporalNamespace string
	TaskQueue         string
}

func LoadConfig(logger zerolog.Logger) *Config {
	config := &Config{
		TemporalHost:      getEnvOrDefault("TEMPORAL_HOST", "localhost:7233"),
		TemporalNamespace: getEnvOrDefault("TEMPORAL_NAMESPACE", "default"),
		TaskQueue:         getEnvOrDefault("TASK_QUEUE", "helm-undeploy-queue"),
	}

	if kubeconfigSecret := os.Getenv("KUBECONFIG_SECRET_PATH"); kubeconfigSecret != "" {
		kubeconfig, err := readSecretFile(kubeconfigSecret)
		if err != nil {
			logger.Error().Err(err).Str("path", kubeconfigSecret).Msg("Failed to read kubeconfig secret")
		} else {
			os.Setenv("KUBECONFIG", kubeconfig)
		}
	}

	return config
}

func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func readSecretFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("failed to read secret file: %w", err)
	}
	return string(data), nil
}