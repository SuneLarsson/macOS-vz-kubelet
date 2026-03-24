package provider

import (
	"context"
	"fmt"
	"time"

	"github.com/virtual-kubelet/virtual-kubelet/log"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// DiscoverNFSEndpoint waits for the NFS service and discovers its raw internal node IP and active NodePort.
// It allows the VM to mount the file share directly by injecting this information into its environment variables.
func DiscoverNFSEndpoint(ctx context.Context, k8sClient kubernetes.Interface, namespace, serviceName string) (targetIP string, nodePort int32, err error) {
	logger := log.G(ctx).WithFields(log.Fields{
		"service": serviceName,
	})

	if k8sClient == nil {
		return "", 0, fmt.Errorf("kubernetes client is required for NFS endpoint discovery")
	}

	logger.Infof("Waiting for endpoints to be ready for Service %s", serviceName)
	targetNodeName, err := waitForEndpoints(ctx, k8sClient, namespace, serviceName)
	if err != nil {
		return "", 0, fmt.Errorf("timeout waiting for NFS endpoints: %w", err)
	}

	// Look up the physical InternalIP of that specific Node
	node, err := k8sClient.CoreV1().Nodes().Get(ctx, targetNodeName, metav1.GetOptions{})
	if err != nil {
		return "", 0, fmt.Errorf("failed to get Node %s: %w", targetNodeName, err)
	}

	for _, addr := range node.Status.Addresses {
		if addr.Type == "InternalIP" {
			targetIP = addr.Address
			break
		}
	}

	if targetIP == "" {
		return "", 0, fmt.Errorf("could not find InternalIP for Node %s", targetNodeName)
	}

	// Look up the Service to find the dynamically assigned NodePort
	svc, err := k8sClient.CoreV1().Services(namespace).Get(ctx, serviceName, metav1.GetOptions{})
	if err != nil {
		return "", 0, fmt.Errorf("failed to get Service %s: %w", serviceName, err)
	}

	for _, port := range svc.Spec.Ports {
		if port.NodePort != 0 {
			nodePort = port.NodePort
			break
		}
	}

	if nodePort == 0 {
		return "", 0, fmt.Errorf("service %s does not have a NodePort configured", serviceName)
	}

	logger.Infof("Successfully discovered NFS Endpoint: IP=%s NodePort=%d", targetIP, nodePort)
	return targetIP, nodePort, nil
}

// waitForEndpoints blocks until the service has at least one ready endpoint
func waitForEndpoints(ctx context.Context, k8sClient kubernetes.Interface, namespace, serviceName string) (string, error) {
	timeout := time.After(2 * time.Minute)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-timeout:
			return "", fmt.Errorf("timed out waiting for endpoints for %s", serviceName)
		case <-ticker.C:
			// check endpoints
			epList, err := k8sClient.DiscoveryV1().EndpointSlices(namespace).List(ctx, metav1.ListOptions{
				LabelSelector: fmt.Sprintf("kubernetes.io/service-name=%s", serviceName),
			})

			if err != nil {
				continue
			}

			// Check if we have at least one ready endpoint address
			for _, slice := range epList.Items {
				for _, ep := range slice.Endpoints {
					if ep.Conditions.Ready != nil && *ep.Conditions.Ready {
						if ep.NodeName != nil {
							return *ep.NodeName, nil
						}
					}
				}
			}
		}
	}
}
