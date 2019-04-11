package deployd

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/navikt/deployment/common/pkg/deployment"
	"github.com/navikt/deployment/deployd/pkg/kubeclient"
	"github.com/navikt/deployment/deployd/pkg/metrics"
	log "github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

var (
	ErrNotMyCluster     = fmt.Errorf("your message belongs in another cluster")
	ErrDeadlineExceeded = fmt.Errorf("deadline exceeded")
)

func matchesCluster(req deployment.DeploymentRequest, cluster string) error {
	if req.GetCluster() != cluster {
		return ErrNotMyCluster
	}
	return nil
}

func meetsDeadline(req deployment.DeploymentRequest) error {
	deadline := time.Unix(req.GetDeadline(), 0)
	late := time.Since(deadline)
	if late > 0 {
		return ErrDeadlineExceeded
	}
	return nil
}

func deployKubernetes(teamClient kubeclient.TeamClient, logger *log.Entry, resources []json.RawMessage) error {
	for index, r := range resources {
		deployed, err := deployJSON(teamClient, r)

		if err != nil {
			return fmt.Errorf("resource %d: %s", index+1, err)
		}

		metrics.KubernetesResources.Inc()

		logger.Infof("resource %d: successfully deployed %s", index+1, deployed.GetSelfLink())
	}

	return nil
}

func deployJSON(teamClient kubeclient.TeamClient, data []byte) (*unstructured.Unstructured, error) {
	resource := unstructured.Unstructured{}
	err := resource.UnmarshalJSON(data)
	if err != nil {
		return nil, fmt.Errorf("while decoding payload: %s", err)
	}

	log.Tracef("Resource URL: %s", resource.GetSelfLink())

	return teamClient.DeployUnstructured(resource)
}

// Prepare decodes a string of bytes into a deployment request,
// and decides whether or not to allow a deployment.
//
// If everything is okay, returns a deployment request. Otherwise, an error.
func Prepare(msg []byte, key, cluster string) (*deployment.DeploymentRequest, error) {
	req := &deployment.DeploymentRequest{}

	if err := deployment.UnwrapMessage(msg, key, req); err != nil {
		return nil, err
	}

	// Check if we are the authoritative handler for this message
	if err := matchesCluster(*req, cluster); err != nil {
		return req, err
	}

	// Messages that are too old are discarded
	if err := meetsDeadline(*req); err != nil {
		return req, err
	}

	return req, nil
}

func Run(logger *log.Entry, msg []byte, key, cluster string, kube kubeclient.TeamClientProvider, deployStatus chan *deployment.DeploymentStatus) {
	// Check the validity and authenticity of the message.
	req, err := Prepare(msg, key, cluster)
	if req != nil {
		nl := logger.WithFields(req.LogFields())
		logger.Data = nl.Data // propagate changes down to caller
	}

	if err != nil {
		logger.Tracef("Discarding incoming message: %s", err)
		if err != ErrNotMyCluster {
			deployStatus <- deployment.NewFailureStatus(*req, err)
		}
		return
	}

	p := req.GetPayloadSpec()
	logger.Data["team"] = p.Team

	teamClient, err := kube.TeamClient(p.Team)
	if err != nil {
		deployStatus <- deployment.NewFailureStatus(*req, err)

	}

	resources, err := p.JSONResources()
	if err != nil {
		deployStatus <- deployment.NewErrorStatus(*req, fmt.Errorf("unserializing kubernetes resources: %s", err))
		return
	}

	if len(resources) == 0 {
		deployStatus <- deployment.NewErrorStatus(*req, fmt.Errorf("no resources to deploy"))
		return
	}

	logger.Infof("Accepting incoming deployment request")

	if err := deployKubernetes(teamClient, logger, resources); err != nil {
		deployStatus <- deployment.NewFailureStatus(*req, err)
		return
	}

	deployStatus <- deployment.NewInProgressStatus(*req)
}
