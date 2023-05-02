package main

import (
	"context"
	orgs "github.com/appuio/control-api/apis/organization/v1"
	controller "github.com/appuio/grafana-organizations-operator/pkg"
	grafana "github.com/grafana/grafana-api-golang-client"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"
)

var (
	ControlApiToken string
	ControlApiUrl   string
	GrafanaUrl      string
	GrafanaUsername string
	GrafanaPassword string
)

func main() {
	ControlApiUrl = os.Getenv("CONTROL_API_URL")
	ControlApiToken = os.Getenv("CONTROL_API_TOKEN")
	GrafanaUrl = os.Getenv("GRAFANA_URL")
	GrafanaUsername = os.Getenv("GRAFANA_USERNAME")
	if GrafanaUsername == "" {
		GrafanaUsername = os.Getenv("admin-user") // env variable name used by Grafana Helm chart. And yes using '-' is stupid because of compatibility issues with some shells.
	}
	GrafanaPassword = os.Getenv("GRAFANA_PASSWORD")
	if GrafanaPassword == "" {
		GrafanaPassword = os.Getenv("admin-password") // env variable name used by Grafana Helm chart. And yes using '-' is stupid because of compatibility issues with some shells.
	}

	klog.Infof("CONTROL_API_URL: %s\n", ControlApiUrl)
	klog.Infof("GRAFANA_URL: %s\n", GrafanaUrl)
	klog.Infof("GRAFANA_USERNAME: %s\n", GrafanaUsername)

	controlApiConfig := &rest.Config{}
	controlApiConfig.BearerToken = ControlApiToken
	controlApiConfig.Host = ControlApiUrl
	controlApiConfig.GroupVersion = &orgs.GroupVersion
	controlApiConfig.APIPath = "/apis"
	controlApiConfig.NegotiatedSerializer = serializer.NewCodecFactory(scheme.Scheme)

	grafanaConfig := grafana.Config{Client: http.DefaultClient, BasicAuth: url.UserPassword(GrafanaUsername, GrafanaPassword)}

	// ctx will be passed to lock and controller to signal termination
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Listen for OS signals
	done := make(chan os.Signal, 1)
	signal.Notify(done, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-done
		klog.Info("Exiting...")
		cancel()
	}()

	controlApiClient, err := rest.RESTClientFor(controlApiConfig)
	if err != nil {
		klog.Errorf("Could not connect to Control API: %v\n", err)
		os.Exit(1)
	}

	klog.Info("Starting initial sync...")
	err = controller.ReconcileAllOrgs(ctx, controlApiClient, grafanaConfig, GrafanaUrl)
	if err != nil {
		klog.Errorf("Could not do initial reconciliation: %v\n", err)
		os.Exit(1)
	}
	klog.Info("Initial sync done")

	for {
		select {
		case <-time.After(10 * time.Second):
		case <-ctx.Done():
			os.Exit(0)
		}
		err = controller.ReconcileAllOrgs(ctx, controlApiClient, grafanaConfig, GrafanaUrl)
		if err != nil {
			klog.Errorf("Could not reconcile (will retry): %v\n", err)
		}
	}
}
