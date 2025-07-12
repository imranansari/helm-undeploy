package activities

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/cli"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type HelmUndeployRequest struct {
	GitHubOrg    string
	RepoName     string
	BranchName   string
	PRNumber     *int // nil for branch deployments, set for PR deployments
	Wait         bool
	Timeout      time.Duration
}

type Activities struct {
	logger     zerolog.Logger
	kubeconfig string
	namespace  string
}

func NewActivities(logger zerolog.Logger) *Activities {
	namespace := os.Getenv("KUBERNETES_NAMESPACE")
	if namespace == "" {
		namespace = "default"
	}
	
	return &Activities{
		logger:     logger,
		kubeconfig: os.Getenv("KUBECONFIG"),
		namespace:  namespace,
	}
}

// generateReleaseName creates a Helm release name based on GitHub context
func (a *Activities) generateReleaseName(request HelmUndeployRequest) string {
	// Sanitize inputs to be valid for Helm release names
	sanitize := func(s string) string {
		// Replace non-alphanumeric characters with hyphens
		reg := regexp.MustCompile(`[^a-zA-Z0-9]+`)
		sanitized := reg.ReplaceAllString(s, "-")
		// Remove leading/trailing hyphens and convert to lowercase
		sanitized = strings.Trim(sanitized, "-")
		return strings.ToLower(sanitized)
	}
	
	repoName := sanitize(request.RepoName)
	branchName := sanitize(request.BranchName)
	
	// For PR deployments: {repo}-pr-{number}
	if request.PRNumber != nil {
		return fmt.Sprintf("%s-pr-%d", repoName, *request.PRNumber)
	}
	
	// For branch deployments: {repo}-{branch}
	// Special handling for main/master branches
	if branchName == "main" || branchName == "master" {
		return repoName
	}
	
	return fmt.Sprintf("%s-%s", repoName, branchName)
}

type ValidateReleaseResponse struct {
	Exists      bool
	Status      string
	Version     int
	UpdatedTime time.Time
}

func (a *Activities) ValidateReleaseActivity(ctx context.Context, request HelmUndeployRequest) (*ValidateReleaseResponse, error) {
	releaseName := a.generateReleaseName(request)
	
	a.logger.Info().
		Str("releaseName", releaseName).
		Str("namespace", a.namespace).
		Str("githubOrg", request.GitHubOrg).
		Str("repoName", request.RepoName).
		Str("branchName", request.BranchName).
		Interface("prNumber", request.PRNumber).
		Msg("Validating helm release")

	actionConfig, err := a.getActionConfig(a.namespace)
	if err != nil {
		return nil, fmt.Errorf("failed to get action config: %w", err)
	}

	listAction := action.NewList(actionConfig)
	listAction.Filter = releaseName
	listAction.Deployed = true

	releases, err := listAction.Run()
	if err != nil {
		return nil, fmt.Errorf("failed to list releases: %w", err)
	}

	for _, release := range releases {
		if release.Name == releaseName {
			return &ValidateReleaseResponse{
				Exists:      true,
				Status:      release.Info.Status.String(),
				Version:     release.Version,
				UpdatedTime: release.Info.LastDeployed.Time,
			}, nil
		}
	}

	return &ValidateReleaseResponse{
		Exists: false,
	}, nil
}

type UndeployReleaseResponse struct {
	Success bool
	Message string
}

func (a *Activities) UndeployReleaseActivity(ctx context.Context, request HelmUndeployRequest) (*UndeployReleaseResponse, error) {
	releaseName := a.generateReleaseName(request)
	
	a.logger.Info().
		Str("releaseName", releaseName).
		Str("namespace", a.namespace).
		Str("githubOrg", request.GitHubOrg).
		Str("repoName", request.RepoName).
		Str("branchName", request.BranchName).
		Interface("prNumber", request.PRNumber).
		Msg("Undeploying helm release")

	actionConfig, err := a.getActionConfig(a.namespace)
	if err != nil {
		return nil, fmt.Errorf("failed to get action config: %w", err)
	}

	uninstallAction := action.NewUninstall(actionConfig)
	uninstallAction.Wait = request.Wait
	if request.Timeout > 0 {
		uninstallAction.Timeout = request.Timeout
	}

	resp, err := uninstallAction.Run(releaseName)
	if err != nil {
		return &UndeployReleaseResponse{
			Success: false,
			Message: fmt.Sprintf("Failed to uninstall release: %v", err),
		}, err
	}

	return &UndeployReleaseResponse{
		Success: true,
		Message: fmt.Sprintf("Release %s uninstalled. Status: %s", releaseName, resp.Info),
	}, nil
}

type VerifyUndeployResponse struct {
	Verified         bool
	ResourcesRemoved bool
	Message          string
}

func (a *Activities) VerifyUndeployActivity(ctx context.Context, request HelmUndeployRequest) (*VerifyUndeployResponse, error) {
	releaseName := a.generateReleaseName(request)
	
	a.logger.Info().
		Str("releaseName", releaseName).
		Str("namespace", a.namespace).
		Str("githubOrg", request.GitHubOrg).
		Str("repoName", request.RepoName).
		Str("branchName", request.BranchName).
		Interface("prNumber", request.PRNumber).
		Msg("Verifying helm release undeploy")

	config, err := a.getKubeConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to get kube config: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes client: %w", err)
	}

	deployments, err := clientset.AppsV1().Deployments(a.namespace).List(ctx, v1.ListOptions{
		LabelSelector: fmt.Sprintf("app.kubernetes.io/instance=%s", releaseName),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list deployments: %w", err)
	}

	services, err := clientset.CoreV1().Services(a.namespace).List(ctx, v1.ListOptions{
		LabelSelector: fmt.Sprintf("app.kubernetes.io/instance=%s", releaseName),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list services: %w", err)
	}

	resourcesRemoved := len(deployments.Items) == 0 && len(services.Items) == 0

	return &VerifyUndeployResponse{
		Verified:         resourcesRemoved,
		ResourcesRemoved: resourcesRemoved,
		Message:          fmt.Sprintf("Deployments: %d, Services: %d", len(deployments.Items), len(services.Items)),
	}, nil
}

func (a *Activities) getActionConfig(namespace string) (*action.Configuration, error) {
	kubeconfigPath := a.kubeconfig
	if kubeconfigPath == "" {
		kubeconfigPath = filepath.Join(os.Getenv("HOME"), ".kube", "config")
	}

	settings := cli.New()
	settings.KubeConfig = kubeconfigPath

	actionConfig := new(action.Configuration)
	err := actionConfig.Init(settings.RESTClientGetter(), namespace, os.Getenv("HELM_DRIVER"), a.logFunc)
	if err != nil {
		return nil, err
	}

	return actionConfig, nil
}

func (a *Activities) getKubeConfig() (*rest.Config, error) {
	kubeconfigPath := a.kubeconfig
	if kubeconfigPath == "" {
		kubeconfigPath = filepath.Join(os.Getenv("HOME"), ".kube", "config")
	}

	if inClusterConfig := os.Getenv("KUBERNETES_SERVICE_HOST"); inClusterConfig != "" {
		config, err := rest.InClusterConfig()
		if err == nil {
			return config, nil
		}
		a.logger.Warn().Err(err).Msg("Failed to use in-cluster config, falling back to kubeconfig")
	}

	config, err := clientcmd.BuildConfigFromFlags("", kubeconfigPath)
	if err != nil {
		return nil, fmt.Errorf("failed to build kube config: %w", err)
	}

	return config, nil
}

func (a *Activities) logFunc(format string, args ...interface{}) {
	a.logger.Debug().Msgf(format, args...)
}