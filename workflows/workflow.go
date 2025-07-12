package workflows

import (
	"time"

	"github.com/helm-undeploy/activities"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

type HelmUndeployResponse struct {
	Success   bool
	Message   string
	Timestamp time.Time
}

func HelmUndeployWorkflow(ctx workflow.Context, request activities.HelmUndeployRequest) (*HelmUndeployResponse, error) {
	logger := workflow.GetLogger(ctx)
	logger.Info("Starting helm undeploy workflow",
		"githubOrg", request.GitHubOrg,
		"repoName", request.RepoName,
		"branchName", request.BranchName,
		"prNumber", request.PRNumber,
		"dryRun", request.DryRun)

	ao := workflow.ActivityOptions{
		StartToCloseTimeout: 10 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    time.Second,
			BackoffCoefficient: 2.0,
			MaximumInterval:    time.Minute,
			MaximumAttempts:    3,
		},
	}
	ctx = workflow.WithActivityOptions(ctx, ao)

	var validateResp activities.ValidateReleaseResponse
	err := workflow.ExecuteActivity(ctx, "ValidateReleaseActivity", request).Get(ctx, &validateResp)
	if err != nil {
		logger.Error("Failed to validate release", "error", err)
		return &HelmUndeployResponse{
			Success:   false,
			Message:   "Failed to validate release: " + err.Error(),
			Timestamp: time.Now(),
		}, err
	}

	if !validateResp.Exists {
		message := "Release not found"
		if request.DryRun {
			message = "DRY-RUN: Release not found (would fail if executed)"
		}
		return &HelmUndeployResponse{
			Success:   false,
			Message:   message,
			Timestamp: time.Now(),
		}, nil
	}

	if request.DryRun {
		logger.Info("DRY-RUN: Would undeploy release", "releaseName", validateResp.ReleaseName)
		return &HelmUndeployResponse{
			Success:   true,
			Message:   "DRY-RUN: Would undeploy release " + validateResp.ReleaseName,
			Timestamp: time.Now(),
		}, nil
	}

	var undeployResp activities.UndeployReleaseResponse
	err = workflow.ExecuteActivity(ctx, "UndeployReleaseActivity", request).Get(ctx, &undeployResp)
	if err != nil {
		logger.Error("Failed to undeploy release", "error", err)
		return &HelmUndeployResponse{
			Success:   false,
			Message:   "Failed to undeploy release: " + err.Error(),
			Timestamp: time.Now(),
		}, err
	}

	if request.Wait && undeployResp.Success {
		var verifyResp activities.VerifyUndeployResponse
		err = workflow.ExecuteActivity(ctx, "VerifyUndeployActivity", request).Get(ctx, &verifyResp)
		if err != nil {
			logger.Warn("Failed to verify undeploy", "error", err)
		}
	}

	logger.Info("Helm undeploy workflow completed",
		"success", undeployResp.Success,
		"message", undeployResp.Message)

	return &HelmUndeployResponse{
		Success:   undeployResp.Success,
		Message:   undeployResp.Message,
		Timestamp: time.Now(),
	}, nil
}