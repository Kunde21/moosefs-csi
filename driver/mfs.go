package mfs

import (
	"github.com/container-storage-interface/spec/lib/go/csi"
)

type mfsDriver struct {
	name    string
	version string

	endpoint  string
	mfsServer string

	nodeID string

	volcap map[csi.VolumeCapability_AccessMode_Mode]bool
}

const driverName = "csi.kunde21.moosefs"

var version = "0.0.1"

func NewMFSdriver(nodeID, endpoint, mfsServer string) *mfsDriver {
	vcam := []csi.VolumeCapability_AccessMode_Mode{
		csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
		csi.VolumeCapability_AccessMode_SINGLE_NODE_READER_ONLY,
		csi.VolumeCapability_AccessMode_MULTI_NODE_READER_ONLY,
		csi.VolumeCapability_AccessMode_MULTI_NODE_SINGLE_WRITER,
		csi.VolumeCapability_AccessMode_MULTI_NODE_MULTI_WRITER,
	}
	n := &mfsDriver{
		name:      driverName,
		version:   version,
		nodeID:    nodeID,
		endpoint:  endpoint,
		mfsServer: mfsServer,
		volcap:    map[csi.VolumeCapability_AccessMode_Mode]bool{},
	}
	for _, v := range vcam {
		n.volcap[v] = true
	}
	return n
}
