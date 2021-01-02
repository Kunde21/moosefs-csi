package mfs

import (
	"context"

	"github.com/container-storage-interface/spec/lib/go/csi"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type IdentityServer struct {
	Driver *mfsDriver
}

func NewIdentityServer(d *mfsDriver) (*IdentityServer, error) {
	switch "" {
	case d.name:
		return nil, status.Error(codes.Unavailable, "Driver name not configured")
	case d.version:
		return nil, status.Error(codes.Unavailable, "Driver is missing version")
	}
	return &IdentityServer{Driver: d}, nil
}

// Register identity server to the grpc server
func (ids *IdentityServer) Register(srv *grpc.Server) {
	csi.RegisterIdentityServer(srv, ids)
}

func (ids *IdentityServer) GetPluginInfo(ctx context.Context, req *csi.GetPluginInfoRequest) (*csi.GetPluginInfoResponse, error) {
	return &csi.GetPluginInfoResponse{
		Name:          ids.Driver.name,
		VendorVersion: ids.Driver.version,
	}, nil
}

func (ids *IdentityServer) Probe(ctx context.Context, req *csi.ProbeRequest) (*csi.ProbeResponse, error) {
	return &csi.ProbeResponse{}, nil
}

func (ids *IdentityServer) GetPluginCapabilities(ctx context.Context, req *csi.GetPluginCapabilitiesRequest) (*csi.GetPluginCapabilitiesResponse, error) {
	return &csi.GetPluginCapabilitiesResponse{
		Capabilities: []*csi.PluginCapability{
			{Type: &csi.PluginCapability_Service_{
				Service: &csi.PluginCapability_Service{
					Type: csi.PluginCapability_Service_CONTROLLER_SERVICE,
				},
			}},
			{Type: &csi.PluginCapability_VolumeExpansion_{
				VolumeExpansion: &csi.PluginCapability_VolumeExpansion{
					Type: csi.PluginCapability_VolumeExpansion_ONLINE,
				},
			}},
		},
	}, nil
}
