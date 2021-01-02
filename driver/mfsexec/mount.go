package mfsexec

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"

	"k8s.io/utils/mount"
)

var _ mount.Interface = (*mounter)(nil)

type mounter struct {
	mount.Interface
}

// NewMounter ...
func NewMounter() (mount.Interface, error) {
	const findmntCmd = "findmnt"
	_, err := exec.LookPath(findmntCmd)
	if err != nil {
		if err == exec.ErrNotFound {
			return nil, fmt.Errorf("%q executable not found in $PATH", findmntCmd)
		}
		return nil, err
	}
	return &mounter{mount.New("")}, nil
}

// Checks if the given src and dst path are mounted
//
// courtesy: https://github.com/digitalocean/csi-digitalocean/blob/master/driver/mounter.go
func (m *mounter) IsMounted(sourcePath, destPath string) (bool, error) {
	switch errMsg := "%s is not specified for checking the mount"; "" {
	case sourcePath:
		return false, fmt.Errorf(errMsg, "source")
	case destPath:
		return false, fmt.Errorf(errMsg, "target")
	}

	fsys, err := listMountsAtPoint(sourcePath)
	if err != nil {
		return false, err
	}
	for _, fs := range fsys {
		// check if the mount is propagated correctly. It should be set to shared.
		if fs.Propagation != "shared" {
			return true, fmt.Errorf("mount propagation for target %q is not enabled or the block device %q does not exist anymore", destPath, sourcePath)
		}
		// the mountpoint should match as well
		if fs.Target == destPath {
			return true, nil
		}
	}
	return false, nil
}

type fileSystem struct {
	Target      string `json:"target"`
	Propagation string `json:"propagation"`
	FsType      string `json:"fstype"`
	Options     string `json:"options"`
}

// listMountsAtPoint ...
func listMountsAtPoint(path string) ([]fileSystem, error) {
	const findmntCmd = "findmnt"
	findmntArgs := []string{"-o", "TARGET,PROPAGATION,FSTYPE,OPTIONS", path, "-J"}
	out, err := exec.Command(findmntCmd, findmntArgs...).CombinedOutput()
	if err != nil {
		out = bytes.TrimSpace(out)
		if len(out) == 0 {
			// findmnt exits with non zero exit status if it couldn't find anything
			return nil, os.ErrNotExist
		}
		return nil, fmt.Errorf("checking mounted failed: %v cmd: %q output: %q",
			err, findmntCmd, string(out))
	}
	var fsys []fileSystem
	err = json.Unmarshal(out, &struct {
		FS []fileSystem `json:"filesystems"`
	}{FS: fsys})
	if err != nil {
		return nil, fmt.Errorf("couldn't unmarshal data: %q: %s", string(out), err)
	}
	return fsys, nil
}
