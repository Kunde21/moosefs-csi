package mfs

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path"
	"path/filepath"
	"time"

	"github.com/Kunde21/moosefs-csi/driver/mfsexec"
	"github.com/container-storage-interface/spec/lib/go/csi"

	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/informers"
	v1 "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/utils/mount"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Keys set within the volume context to hold volume details
const (
	// Server Key
	sk = "server"
	// Path Key
	pk = "path"
	// Root Key
	rk = "root"
)

var _ csi.ControllerServer = (*ControllerServer)(nil)

type ControllerServer struct {
	d        *mfsDriver
	mountDir string
	root     string

	mfscli moosefsCLI

	stop chan struct{}
	vols v1.PersistentVolumeInformer
}

type moosefsCLI interface {
	SetQuota(context.Context, string, int64) error
	GetQuota(context.Context, string) (int64, int64, error)
	GetAvailableCap(context.Context) (int64, error)
}

func NewControllerServer(d *mfsDriver, root, mountDir string) (*ControllerServer, error) {
	root, mountDir = path.Clean(root), path.Clean(mountDir)
	if root == "" {
		root = "/"
	}
	switch mountDir {
	case "", ".", "/":
		return nil, fmt.Errorf("invalid mount directory %v", mountDir)
	}
	// Multiple clusters on the same nodes could cause a collision where clusters are sharing
	// the same default mountDir.  Avoid collisions unless they are the same mfs root directory.
	mountDir = filepath.Join(mountDir, root)

	m := mount.New("")
	if err := m.Unmount(mountDir); err != nil {
		log.Printf("cleanup error: %v", err)
	}
	if err := os.MkdirAll(mountDir, 0744); err != nil {
		return nil, err
	}
	srv := path.Join(d.mfsServer+":", root)
	if err := m.Mount(srv, mountDir, "moosefs", nil); err != nil {
		return nil, fmt.Errorf("failed to mount root directory: %w", err)
	}

	conf, err := rest.InClusterConfig()
	if err != nil {
		return nil, err
	}
	k8cl, err := kubernetes.NewForConfig(conf)
	if err != nil {
		return nil, err
	}

	inf := informers.NewSharedInformerFactory(k8cl, 10*time.Second)
	vols := inf.Core().V1().PersistentVolumes()
	stop := make(chan struct{}, 1)
	go vols.Informer().Run(stop)
	mfscli, err := mfsexec.New(mountDir, srv)
	if err != nil {
		return nil, err
	}
	return &ControllerServer{
		d:        d,
		mountDir: mountDir,
		root:     root,

		mfscli: mfscli,

		stop: stop,
		vols: vols,
	}, nil
}

// Register node server to the grpc server
func (cs *ControllerServer) Register(srv *grpc.Server) {
	csi.RegisterControllerServer(srv, cs)
}

// Close the server and release resources.
func (cs *ControllerServer) Close() error {
	err := mount.New("").Unmount(cs.mountDir)
	if err != nil {
		return fmt.Errorf("errors encountered mount %q", err)
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
	prms, vCtx := req.GetParameters(), map[string]string{}
	vCtx[sk] = cs.d.mfsServer
	if prms[sk] != "" {
		vCtx[sk] = prms[sk]
	}
	// clear out any relative paths to protect cluster root
	vCtx[pk] = filepath.Join("volumes", req.GetName())
	vCtx[rk] = cs.root
	if cp := path.Clean(prms[pk]); cp != "." {
		if !path.IsAbs(cp) {
			// relative paths can escape into the host filesystem
			// join them to root to force an absolute path starting in mfs root directory
			cp = path.Join("/", cp)
		}
		if prms[rk] == "true" {
			vCtx[rk] = ""
		}
		vCtx[pk] = cp
	}
	return &csi.CreateVolumeResponse{
		Volume: &csi.Volume{
			VolumeId:      req.GetName(),
			CapacityBytes: req.GetCapacityRange().GetRequiredBytes(),
			VolumeContext: vCtx,
		},
	}, nil
}

func dirExist(mount, path string) (bool, error) {
	dirPath := filepath.Join(mount, path)
	fi, err := os.Stat(dirPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, status.Errorf(codes.Internal, "file system error: %v", err)
	}
	if fi.IsDir() {
		return true, nil
	}
	return true, status.Errorf(codes.FailedPrecondition, "path %q is not a directory", path)
}

func (cs *ControllerServer) DeleteVolume(ctx context.Context, req *csi.DeleteVolumeRequest) (*csi.DeleteVolumeResponse, error) {
	switch "" {
	case req.GetVolumeId():
		return nil, status.Error(codes.InvalidArgument, "volume id not provided")
	}
	vk8, err := cs.vols.Lister().Get(req.GetVolumeId())
	if err != nil {
		if k8serr.IsNotFound(err) {
			return &csi.DeleteVolumeResponse{}, nil
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	if vk8.Spec.CSI == nil || vk8.Spec.CSI.VolumeAttributes == nil {
		return nil, status.Error(codes.InvalidArgument, "volume not provisioned by CSI driver")
	}
	params := vk8.Spec.CSI.VolumeAttributes
	if err := os.RemoveAll(filepath.Join(cs.mountDir, params[pk])); err != nil {
		return nil, status.Errorf(codes.Internal, "remove directory failed: %v", err)
	}
	return &csi.DeleteVolumeResponse{}, nil
}

func (cs *ControllerServer) ControllerGetVolume(_ context.Context, _ *csi.ControllerGetVolumeRequest) (*csi.ControllerGetVolumeResponse, error) {
	return nil, status.Error(codes.Unimplemented, "")
}

func (cs *ControllerServer) ControllerPublishVolume(ctx context.Context, req *csi.ControllerPublishVolumeRequest) (*csi.ControllerPublishVolumeResponse, error) {
	log.Printf("publish request %+v", req)
	switch "" {
	case req.GetNodeId(), req.GetVolumeId():
		return nil, status.Error(codes.InvalidArgument, "node and volume ids are required")
	}
	if req.GetVolumeCapability() == nil {
		return nil, status.Error(codes.InvalidArgument, "volume capability required")
	}
	vk8, err := cs.vols.Lister().Get(req.GetVolumeId())
	if err != nil {
		log.Println(err)
		return nil, status.Error(codes.NotFound, "volume does not exist")
	}
	c := vk8.Spec.Capacity[corev1.ResourceStorage]
	cap, ok := c.AsInt64()
	if !ok {
		return nil, status.Error(codes.Internal, "failed to get capacity")
	}
	vCtx := req.GetVolumeContext()
	if vCtx[rk] == "" {
		return &csi.ControllerPublishVolumeResponse{}, nil
	}
	// Create directory if it does not exist.
	// Missing directory will cause the mount to fail on the node.
	ex, err := dirExist(cs.mountDir, vCtx[pk])
	if err != nil {
		return nil, err
	}
	log.Printf("mountdir: %q, path: %q", cs.mountDir, vCtx[pk])
	md := filepath.Join(cs.mountDir, vCtx[pk])
	if !ex {
		log.Printf("dirpath: %q", md)
		if err := os.MkdirAll(md, 0755); err != nil {
			return nil, status.Errorf(codes.Internal, "create directory failed: %v", err)
		}
	}

	if err := cs.mfscli.SetQuota(ctx, md, cap); err != nil {
		return nil, status.Errorf(codes.Internal, "set quota failed: %v", err)
	}

	return &csi.ControllerPublishVolumeResponse{}, nil
}

func (cs *ControllerServer) ControllerUnpublishVolume(ctx context.Context, req *csi.ControllerUnpublishVolumeRequest) (*csi.ControllerUnpublishVolumeResponse, error) {
	switch "" {
	case req.GetVolumeId():
		return nil, status.Error(codes.InvalidArgument, "missing volume id")
	}
	return &csi.ControllerUnpublishVolumeResponse{}, nil
}

func (cs *ControllerServer) ValidateVolumeCapabilities(ctx context.Context, req *csi.ValidateVolumeCapabilitiesRequest) (*csi.ValidateVolumeCapabilitiesResponse, error) {
	switch "" {
	case req.GetVolumeId():
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	_, err := cs.vols.Lister().Get(req.GetVolumeId())
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

func (cs *ControllerServer) GetCapacity(ctx context.Context, _ *csi.GetCapacityRequest) (*csi.GetCapacityResponse, error) {
	cap, err := cs.mfscli.GetAvailableCap(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to read capacity %q", err)
	}
	return &csi.GetCapacityResponse{AvailableCapacity: cap}, nil
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
			{Type: &csi.ControllerServiceCapability_Rpc{
				Rpc: &csi.ControllerServiceCapability_RPC{
					Type: csi.ControllerServiceCapability_RPC_PUBLISH_UNPUBLISH_VOLUME,
				},
			}},
			{Type: &csi.ControllerServiceCapability_Rpc{
				Rpc: &csi.ControllerServiceCapability_RPC{
					Type: csi.ControllerServiceCapability_RPC_GET_CAPACITY,
				},
			}},
			{Type: &csi.ControllerServiceCapability_Rpc{
				Rpc: &csi.ControllerServiceCapability_RPC{
					Type: csi.ControllerServiceCapability_RPC_EXPAND_VOLUME,
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

func (cs *ControllerServer) ControllerExpandVolume(ctx context.Context, req *csi.ControllerExpandVolumeRequest) (*csi.ControllerExpandVolumeResponse, error) {
	vol, err := cs.vols.Lister().Get(req.GetVolumeId())
	if err != nil {
		if k8serr.IsNotFound(err) {
			return nil, status.Error(codes.NotFound, err.Error())
		}
		return nil, err
	}
	sz := vol.Spec.Capacity["storage"]
	cap := req.GetCapacityRange().GetRequiredBytes()
	if s, _ := sz.AsInt64(); s > cap {
		return nil, status.Error(codes.InvalidArgument, "resize must be larger than current size")
	}
	path := vol.Spec.CSI.VolumeAttributes[pk]
	if vol.Spec.CSI.VolumeAttributes[rk] == "" {
		return &csi.ControllerExpandVolumeResponse{CapacityBytes: cap}, nil
	}
	if err := cs.mfscli.SetQuota(ctx, path, cap); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &csi.ControllerExpandVolumeResponse{CapacityBytes: cap}, nil
}
