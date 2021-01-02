package mfs

import (
	"github.com/Kunde21/moosefs-csi/driver/mfsexec"
	"github.com/container-storage-interface/spec/lib/go/csi"
)

type mfsDriver struct {
	name    string
	version string

	endpoint  string
	mfsServer string

	moosefsCLI

	nodeID string

	volcap map[csi.VolumeCapability_AccessMode_Mode]bool
}

const driverName = "csi.kunde21.moosefs"

var version = "0.0.2"

func NewMFSdriver(nodeID, endpoint, mfsServer, mfsdir string) (*mfsDriver, error) {
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
	var err error
	n.moosefsCLI, err = mfsexec.New(mfsdir, mfsServer)
	if err != nil {
		return nil, err
	}
	return n, nil
}
