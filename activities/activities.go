package activities

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
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
	ReleaseName string
	Namespace   string
	Wait        bool
	Timeout     time.Duration
}

type Activities struct {
	logger     zerolog.Logger
	kubeconfig string
}

func NewActivities(logger zerolog.Logger) *Activities {
	return &Activities{
		logger:     logger,
		kubeconfig: os.Getenv("KUBECONFIG"),
	}
}

type ValidateReleaseResponse struct {
	Exists      bool
	Status      string
	Version     int
	UpdatedTime time.Time
}

func (a *Activities) ValidateReleaseActivity(ctx context.Context, request HelmUndeployRequest) (*ValidateReleaseResponse, error) {
	a.logger.Info().
		Str("release", request.ReleaseName).
		Str("namespace", request.Namespace).
		Msg("Validating helm release")

	actionConfig, err := a.getActionConfig(request.Namespace)
	if err != nil {
		return nil, fmt.Errorf("failed to get action config: %w", err)
	}

	listAction := action.NewList(actionConfig)
	listAction.Filter = request.ReleaseName
	listAction.Deployed = true

	releases, err := listAction.Run()
	if err != nil {
		return nil, fmt.Errorf("failed to list releases: %w", err)
	}

	for _, release := range releases {
		if release.Name == request.ReleaseName {
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
	a.logger.Info().
		Str("release", request.ReleaseName).
		Str("namespace", request.Namespace).
		Msg("Undeploying helm release")

	actionConfig, err := a.getActionConfig(request.Namespace)
	if err != nil {
		return nil, fmt.Errorf("failed to get action config: %w", err)
	}

	uninstallAction := action.NewUninstall(actionConfig)
	uninstallAction.Wait = request.Wait
	if request.Timeout > 0 {
		uninstallAction.Timeout = request.Timeout
	}

	resp, err := uninstallAction.Run(request.ReleaseName)
	if err != nil {
		return &UndeployReleaseResponse{
			Success: false,
			Message: fmt.Sprintf("Failed to uninstall release: %v", err),
		}, err
	}

	return &UndeployReleaseResponse{
		Success: true,
		Message: fmt.Sprintf("Release %s uninstalled. Status: %s", request.ReleaseName, resp.Info),
	}, nil
}

type VerifyUndeployResponse struct {
	Verified         bool
	ResourcesRemoved bool
	Message          string
}

func (a *Activities) VerifyUndeployActivity(ctx context.Context, request HelmUndeployRequest) (*VerifyUndeployResponse, error) {
	a.logger.Info().
		Str("release", request.ReleaseName).
		Str("namespace", request.Namespace).
		Msg("Verifying helm release undeploy")

	config, err := a.getKubeConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to get kube config: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes client: %w", err)
	}

	deployments, err := clientset.AppsV1().Deployments(request.Namespace).List(ctx, v1.ListOptions{
		LabelSelector: fmt.Sprintf("app.kubernetes.io/instance=%s", request.ReleaseName),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list deployments: %w", err)
	}

	services, err := clientset.CoreV1().Services(request.Namespace).List(ctx, v1.ListOptions{
		LabelSelector: fmt.Sprintf("app.kubernetes.io/instance=%s", request.ReleaseName),
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