package mfs

import (
	"context"
	"fmt"
	"path"
	"path/filepath"

	"github.com/Kunde21/moosefs-csi/driver/store/bolt"
	"github.com/container-storage-interface/spec/lib/go/csi"
	"k8s.io/utils/mount"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const boltFile = "csi.kunde21.moosefs.bdb"

var _ csi.ControllerServer = (*ControllerServer)(nil)

type ControllerServer struct {
	d        *mfsDriver
	vol      *bolt.VolStore
	mountDir string
	root     string
}

func NewControllerServer(d *mfsDriver, root, mountDir string) (*ControllerServer, error) {
	root, mountDir = path.Clean(root), path.Clean(mountDir)
	if root == "" {
		root = "/"
	}
	switch mountDir {
	case "", "/":
		return nil, fmt.Errorf("invalid mount directory %v", mountDir)
	}
	m := mount.New("")

	if err := m.Mount(path.Join(d.mfsServer+":", root), mountDir, "moosefs", nil); err != nil {
		return nil, fmt.Errorf("failed to mount root directory: %w", err)
	}
	vs, err := bolt.New(filepath.Join(mountDir, boltFile))
	if err != nil {
		return nil, err
	}
	return &ControllerServer{
		d:        d,
		vol:      vs,
		mountDir: mountDir,
		root:     root,
	}, nil
}

// Register node server to the grpc server
func (cs *ControllerServer) Register(srv *grpc.Server) {
	csi.RegisterControllerServer(srv, cs)
}

// Close the server and release resources.
func (cs *ControllerServer) Close() error {
	verr := cs.vol.Close()
	merr := mount.New("").Unmount(cs.mountDir)
	if verr != nil || merr != nil {
		return fmt.Errorf("errors encountered store %q mount %q", verr, merr)
	}
	return nil
}

func (cs *ControllerServer) CreateVolume(ctx context.Context, req *csi.CreateVolumeRequest) (*csi.CreateVolumeResponse, error) {
	switch {
	case req.GetName() == "":
		return nil, status.Error(codes.InvalidArgument, "name not provided")
	case len(req.GetVolumeCapabilities()) == 0:
		return nil, status.Error(codes.InvalidArgument, "capabilities not provided")
	}
	if err := cs.vol.InsertVolume(ctx, bolt.Volume{
		ID:       req.GetName(),
		Capacity: req.CapacityRange.GetRequiredBytes(),
		Params:   req.GetParameters(),
	}); err != nil {
		if status.Code(err) == codes.AlreadyExists {
			return nil, err
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &csi.CreateVolumeResponse{
		Volume: &csi.Volume{
			CapacityBytes: req.GetCapacityRange().GetRequiredBytes(),
			VolumeId:      req.Name,
			VolumeContext: req.GetParameters(),
		},
	}, nil
}

func (cs *ControllerServer) DeleteVolume(ctx context.Context, req *csi.DeleteVolumeRequest) (*csi.DeleteVolumeResponse, error) {
	switch "" {
	case req.GetVolumeId():
		return nil, status.Error(codes.InvalidArgument, "volume id not provided")
	}
	if err := cs.vol.DeleteVolume(ctx, req.GetVolumeId()); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &csi.DeleteVolumeResponse{}, nil
}

func (cs *ControllerServer) ControllerGetVolume(_ context.Context, _ *csi.ControllerGetVolumeRequest) (*csi.ControllerGetVolumeResponse, error) {
	return nil, status.Error(codes.Unimplemented, "")
}

func (cs *ControllerServer) ControllerPublishVolume(_ context.Context, _ *csi.ControllerPublishVolumeRequest) (*csi.ControllerPublishVolumeResponse, error) {
	return nil, status.Error(codes.Unimplemented, "")
}

func (cs *ControllerServer) ControllerUnpublishVolume(_ context.Context, _ *csi.ControllerUnpublishVolumeRequest) (*csi.ControllerUnpublishVolumeResponse, error) {
	return nil, status.Error(codes.Unimplemented, "")
}

func (cs *ControllerServer) ValidateVolumeCapabilities(ctx context.Context, req *csi.ValidateVolumeCapabilitiesRequest) (*csi.ValidateVolumeCapabilitiesResponse, error) {
	switch "" {
	case req.GetVolumeId():
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	_, err := cs.vol.GetVolume(ctx, req.GetVolumeId())
	if status.Code(err) == codes.NotFound {
		return nil, err
	}
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	switch 0 {
	case len(req.GetVolumeCapabilities()):
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	valid := true
	for _, cap := range req.GetVolumeCapabilities() {
		valid = valid && cs.d.volcap[cap.GetAccessMode().GetMode()]
	}
	if !valid {
		return nil, status.Error(codes.InvalidArgument, "capabilities don't exist")
	}
	return &csi.ValidateVolumeCapabilitiesResponse{
		Confirmed: &csi.ValidateVolumeCapabilitiesResponse_Confirmed{
			VolumeContext:      req.VolumeContext,
			VolumeCapabilities: req.VolumeCapabilities,
			Parameters:         req.Parameters,
		},
	}, nil
}

func (cs *ControllerServer) ListVolumes(_ context.Context, _ *csi.ListVolumesRequest) (*csi.ListVolumesResponse, error) {
	return nil, status.Error(codes.Unimplemented, "")
}

func (cs *ControllerServer) GetCapacity(_ context.Context, _ *csi.GetCapacityRequest) (*csi.GetCapacityResponse, error) {
	return nil, status.Error(codes.Unimplemented, "")
}

// ControllerGetCapabilities implements the default GRPC callout.
// Default supports all capabilities
func (cs *ControllerServer) ControllerGetCapabilities(ctx context.Context, req *csi.ControllerGetCapabilitiesRequest) (*csi.ControllerGetCapabilitiesResponse, error) {
	return &csi.ControllerGetCapabilitiesResponse{
		Capabilities: []*csi.ControllerServiceCapability{
			{Type: &csi.ControllerServiceCapability_Rpc{
				Rpc: &csi.ControllerServiceCapability_RPC{
					Type: csi.ControllerServiceCapability_RPC_CREATE_DELETE_VOLUME,
				},
			}},
		},
	}, nil
}

func (cs *ControllerServer) CreateSnapshot(_ context.Context, _ *csi.CreateSnapshotRequest) (*csi.CreateSnapshotResponse, error) {
	return nil, status.Error(codes.Unimplemented, "")
}

func (cs *ControllerServer) DeleteSnapshot(_ context.Context, _ *csi.DeleteSnapshotRequest) (*csi.DeleteSnapshotResponse, error) {
	return nil, status.Error(codes.Unimplemented, "")
}

func (cs *ControllerServer) ListSnapshots(_ context.Context, _ *csi.ListSnapshotsRequest) (*csi.ListSnapshotsResponse, error) {
	return nil, status.Error(codes.Unimplemented, "")
}

func (cs *ControllerServer) ControllerExpandVolume(_ context.Context, _ *csi.ControllerExpandVolumeRequest) (*csi.ControllerExpandVolumeResponse, error) {
	return nil, status.Error(codes.Unimplemented, "")
}
