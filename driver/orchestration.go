package mfs

import (
	"log"

	nomad "github.com/hashicorp/nomad/api"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	v1 "k8s.io/client-go/informers/core/v1"
)

type k8sIntegration struct {
	vs   v1.PersistentVolumeInformer
	nds  v1.NodeInformer
	stop chan struct{}
}

type nomadIntegration struct {
	client *nomad.Client
	stop   chan struct{}
}

// Implement k8s interface
func (k8s *k8sIntegration) VolumeExists(id string) error {
	_, err := k8s.vs.Lister().Get(id)
	return err
}

func (k8s *k8sIntegration) NodeExists(id string) error {
	_, err := k8s.nds.Lister().Get(id)
	return err
}

func (k8s *k8sIntegration) GetVolumeCapacity(id string) (int64, error) {
	vk8, err := k8s.vs.Lister().Get(id)
	if err != nil {
		log.Println(err)
		return 0, status.Error(codes.NotFound, "k8s volume does not exist")
	}
	c := vk8.Spec.Capacity.Storage()
	cap, ok := c.AsInt64()
	if !ok {
		return 0, status.Error(codes.Internal, "failed to get capacity")
	}
	return cap, nil
}

// Implement Nomad interface
func (n *nomadIntegration) VolumeExists(id string) error {
	q := &nomad.QueryOptions{}
	_, _, err := n.client.CSIVolumes().Info(id, q)
	return err
}

func (n *nomadIntegration) NodeExists(id string) error {
	q := &nomad.QueryOptions{}
	_, _, err := n.client.Nodes().Info(id, q)
	return err
}

func (n *nomadIntegration) GetVolumeCapacity(id string) (int64, error) {
	q := &nomad.QueryOptions{}
	vol, _, err := n.client.CSIVolumes().Info(id, q)
	if err != nil {
		log.Println(err)
		return 0, status.Error(codes.NotFound, "k8s volume does not exist")
	}
	return vol.Capacity, nil
}
