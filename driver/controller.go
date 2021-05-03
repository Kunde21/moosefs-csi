package mfs

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Kunde21/moosefs-csi/driver/metastore"
	"github.com/Kunde21/moosefs-csi/driver/mfsexec"
	"github.com/container-storage-interface/spec/lib/go/csi"

	nomad "github.com/hashicorp/nomad/api"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/utils/mount"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	// Directory root for persistent volumes
	volumePath = "volumes"
	// Directory root for cluster metadata
	metaPath = "k8smeta"
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
	orch   Orchestration
	mfsvs  *metastore.Store
}

type moosefsCLI interface {
	SetQuota(context.Context, string, int64) error
	GetQuota(context.Context, string) (int64, int64, error)
	GetAvailableCap(context.Context) (int64, error)
}

type Orchestration interface {
	VolumeExists(string) error
	NodeExists(string) error
	GetVolumeCapacity(string) (int64, error)
}

func InitK8sIntegration() (*k8sIntegration, error) {
	conf, err := rest.InClusterConfig()
	if err != nil {
		return nil, err
	}
	k8cl, err := kubernetes.NewForConfig(conf)
	if err != nil {
		return nil, err
	}
	inf := informers.NewSharedInformerFactory(k8cl, 10*time.Second)
	nodes := inf.Core().V1().Nodes()
	vols := inf.Core().V1().PersistentVolumes()
	stop := make(chan struct{}, 1)
	go vols.Informer().Run(stop)
	go nodes.Informer().Run(stop)
	return &k8sIntegration{
		stop: stop,
		vs:   vols,
		nds:  nodes,
	}, nil
}

func InitNomadIntegration() (*nomadIntegration, error) {
	stop := make(chan struct{}, 1)
	config := &nomad.Config{
		Address:  os.Getenv("NOMAD_ADDR"),
		SecretID: os.Getenv("NOMAD_TOKEN"),
		TLSConfig: &nomad.TLSConfig{
			CACert:     os.Getenv("NOMAD_CAPATH"),
			ClientCert: os.Getenv("NOMAD_CLIENT_CERT"),
			ClientKey:  os.Getenv("NOMAD_CLIENT_KEY"),
		},
	}

	client, err := nomad.NewClient(config)

	if err != nil {
		return nil, err
	}

	return &nomadIntegration{
		client: client,
		stop:   stop,
	}, nil
}

func NewControllerServer(orch Orchestration, d *mfsDriver, root, mountDir string) (*ControllerServer, error) {

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

	mfscli, err := mfsexec.New(mountDir, srv)
	if err != nil {
		return nil, err
	}
	md := filepath.Join(mountDir, metaPath)
	if err := os.MkdirAll(md, 0744); err != nil {
		return nil, err
	}
	mvols := metastore.New(md)
	return &ControllerServer{
		d:        d,
		mountDir: mountDir,
		root:     root,

		mfscli: mfscli,
		mfsvs:  mvols,
		orch:   orch,
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

	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	pm, vCtx := req.GetParameters(), map[string]string{}
	vCtx[sk] = cs.d.mfsServer
	if pm[sk] != "" {
		vCtx[sk] = pm[sk]
	}
	vol := metastore.Volume{
		Name:     req.GetName(),
		Capacity: req.GetCapacityRange().GetRequiredBytes(),
		Dynamic:  true,
		MFSPath:  cs.root,
	}
	// clear out any relative paths to protect cluster root
	vCtx[pk] = filepath.Join(volumePath, req.GetName())
	vCtx[rk] = cs.root
	if cp := path.Clean(pm[pk]); cp != "." {
		if !path.IsAbs(cp) {
			// relative paths can escape into the host filesystem
			// join them to root to force an absolute path starting in mfs root directory
			cp = path.Join("/", cp)
		}
		if pm[rk] == "true" {
			vol.MFSPath = ""
			vCtx[rk] = ""
		}
		vCtx[pk] = cp
	}
	vol.MFSPath = filepath.Join(vCtx[rk], vCtx[pk])
	if isSubDir(cs.root, vol.MFSPath) {
		vol.Path = strings.TrimPrefix(vCtx[pk], cs.root)
	}
	csivol := &csi.Volume{
		VolumeId:      req.GetName(),
		CapacityBytes: req.GetCapacityRange().GetRequiredBytes(),
		VolumeContext: vCtx,
	}
	v, err := cs.mfsvs.ReadVol(ctx, req.GetName())
	if status.Code(err) == codes.OK {
		switch {
		case v.Capacity != vol.Capacity:
			return nil, status.Error(codes.AlreadyExists, "capacity cannot be changed")
		case v.MFSPath != vol.MFSPath:
			return nil, status.Error(codes.AlreadyExists, "volume path cannot change")
		}
		return &csi.CreateVolumeResponse{Volume: csivol}, nil
	}
	if err := cs.mfsvs.CreateVol(ctx, vol); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &csi.CreateVolumeResponse{Volume: csivol}, nil
}

// mkDir creates the volume directory.
func (cs *ControllerServer) mkDir(path string) error {
	if !isSubDir(cs.mountDir, path) {
		return nil
	}
	if ok, err := dirExist("/", path); err != nil {
		return status.Errorf(codes.Internal, "directory exist failed: %v", err)
	} else if ok {
		return nil
	}
	if err := os.MkdirAll(path, 0755); err != nil {
		return status.Errorf(codes.Internal, "create directory failed: %v", err)
	}
	return nil
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

func isSubDir(root, path string) bool {
	dir := filepath.Dir(path)
	for ; ; dir = filepath.Dir(dir) {
		switch {
		case root == dir:
			return true
		case len(dir) <= len(root):
			return false
		}
	}
}

func (cs *ControllerServer) DeleteVolume(ctx context.Context, req *csi.DeleteVolumeRequest) (*csi.DeleteVolumeResponse, error) {
	switch "" {
	case req.GetVolumeId():
		return nil, status.Error(codes.InvalidArgument, "volume id not provided")
	}
	v, err := cs.mfsvs.ReadVol(ctx, req.GetVolumeId())
	if status.Code(err) != codes.OK || v.Path == "" {
		return &csi.DeleteVolumeResponse{}, nil
	}
	if err := os.RemoveAll(filepath.Join(cs.mountDir, v.Path)); err != nil {
		return nil, status.Errorf(codes.Internal, "remove directory failed: %v", err)
	}
	if err := cs.mfsvs.DeleteVol(ctx, v.Name); err != nil {
		return nil, status.Errorf(codes.Internal, "remove volume failed: %v", err)
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
	vCtx := req.GetVolumeContext()
	v, err := cs.mfsvs.ReadVol(ctx, req.GetVolumeId())
	switch status.Code(err) {
	default:
		return nil, status.Error(codes.Internal, err.Error())
	case codes.NotFound:
		// Pre-allocated volume
		v = &metastore.Volume{
			Name:    req.GetVolumeId(),
			Dynamic: false,
			MFSPath: filepath.Join(vCtx[rk], vCtx[pk]),
		}
		if isSubDir(cs.root, v.MFSPath) {
			v.Path = strings.TrimPrefix(vCtx[pk], cs.root)
		}
		cap, err := cs.getVolumeCapacity(v.Name)
		if err != nil {
			return nil, err
		}
		v.Capacity = cap
		if err := cs.mfsvs.CreateVol(ctx, *v); err != nil {
			return nil, status.Error(codes.Internal, err.Error())
		}
	case codes.OK:
		// Success
	}
	if v.Capacity == 0 {
		// Set quota when missing
		cap, err := cs.getVolumeCapacity(v.Name)
		if err == nil {
			v.Capacity = cap
			if err := cs.mfsvs.UpdateVol(ctx, *v); err != nil {
				return nil, status.Error(codes.Internal, err.Error())
			}
		}
	}

	if err := cs.orch.NodeExists(req.GetNodeId()); err != nil {
		if k8serr.IsNotFound(err) {
			return nil, status.Error(codes.NotFound, "node does not exist")
		}
	}
	if v.Path == "" {
		// Directory is outside of managed space.
		v.Node = req.GetNodeId()
		if err := cs.mfsvs.UpdateVol(ctx, *v); err != nil {
			return nil, status.Error(codes.Internal, err.Error())
		}
		return &csi.ControllerPublishVolumeResponse{
			PublishContext: map[string]string{"quota": strconv.FormatInt(v.Capacity, 10)},
		}, nil
	}
	md := filepath.Join(cs.mountDir, v.Path)

	// Create directory if it does not exist.
	// Missing directory will cause the mount to fail on the node.
	if err := cs.mkDir(md); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	if v.Capacity == 0 {
		return &csi.ControllerPublishVolumeResponse{}, nil
	}
	if err := cs.mfscli.SetQuota(ctx, md, v.Capacity); err != nil {
		return nil, status.Errorf(codes.Internal, "set quota failed: %v", err)
	}

	v.Node = req.GetNodeId()
	if err := cs.mfsvs.UpdateVol(ctx, *v); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &csi.ControllerPublishVolumeResponse{}, nil
}

// getVolumeCapacity as defined in k8s pv.
func (cs *ControllerServer) getVolumeCapacity(id string) (int64, error) {
	return cs.orch.GetVolumeCapacity(id)
}

func (cs *ControllerServer) ControllerUnpublishVolume(ctx context.Context, req *csi.ControllerUnpublishVolumeRequest) (*csi.ControllerUnpublishVolumeResponse, error) {
	switch "" {
	case req.GetVolumeId():
		return nil, status.Error(codes.InvalidArgument, "missing volume id")
	}
	v, err := cs.mfsvs.ReadVol(ctx, req.GetVolumeId())
	switch status.Code(err) {
	default:
		return nil, status.Error(codes.Internal, err.Error())
	case codes.NotFound:
		return &csi.ControllerUnpublishVolumeResponse{}, nil
	case codes.OK:
	}
	if !v.Dynamic {
		if err := cs.mfsvs.DeleteVol(ctx, req.GetVolumeId()); err != nil {
			return nil, status.Error(codes.Internal, err.Error())
		}
		return &csi.ControllerUnpublishVolumeResponse{}, nil
	}
	v.Node = ""
	if err := cs.mfsvs.UpdateVol(ctx, *v); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &csi.ControllerUnpublishVolumeResponse{}, nil
}

func (cs *ControllerServer) ValidateVolumeCapabilities(ctx context.Context, req *csi.ValidateVolumeCapabilitiesRequest) (*csi.ValidateVolumeCapabilitiesResponse, error) {
	switch "" {
	case req.GetVolumeId():
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	if len(req.GetVolumeCapabilities()) == 0 {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	_, err := cs.mfsvs.ReadVol(ctx, req.GetVolumeId())
	if status.Code(err) == codes.NotFound {
		if err := cs.orch.VolumeExists(req.GetVolumeId()); status.Code(err) != codes.OK {
			return nil, status.Error(codes.NotFound, "volume does not exist")
		}
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
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	cap, err := cs.mfscli.GetAvailableCap(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to read capacity %v", err.Error())
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
	switch "" {
	case req.GetVolumeId():
		return nil, status.Error(codes.InvalidArgument, "missing volume id")
	}
	v, err := cs.mfsvs.ReadVol(ctx, req.GetVolumeId())
	if status.Code(err) == codes.NotFound {
		return nil, err
	}
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	cap := req.GetCapacityRange().GetRequiredBytes()
	if v.Capacity > cap {
		return &csi.ControllerExpandVolumeResponse{CapacityBytes: cap}, nil
	}

	// Update quota on managed paths only
	dir := filepath.Join(cs.mountDir, v.Path)
	ex, err := dirExist("/", dir)
	if err != nil {
		return nil, err
	}
	node := v.Path != "" && ex
	if node {
		if err := cs.mfscli.SetQuota(ctx, dir, cap); err != nil {
			return nil, status.Error(codes.Internal, err.Error())
		}
	}
	v.Capacity = cap
	if err := cs.mfsvs.UpdateVol(ctx, *v); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &csi.ControllerExpandVolumeResponse{
		CapacityBytes:         cap,
		NodeExpansionRequired: !node,
	}, nil
}
