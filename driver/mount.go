package mfs

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"k8s.io/utils/mount"
)

var _ mount.Interface

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

// mount the mooseFs filesystem (possibly deprecated for k8s/mount)
func (m *mounter) mount(sourcePath, destPath, mountType string, opts []string) error {
	switch errMsg := "%s is not specified for mounting the volume"; "" {
	case sourcePath:
		return fmt.Errorf(errMsg, "source")
	case destPath:
		return fmt.Errorf(errMsg, "destination path")
	}

	mountCmd := "mount"
	mountArgs := append(make([]string, 0, 6), "-t", mountType)
	if len(opts) > 0 {
		mountArgs = append(mountArgs, "-o", strings.Join(opts, ","))
	}
	mountArgs = append(mountArgs, sourcePath)
	mountArgs = append(mountArgs, destPath)

	// create target, os.Mkdirall is noop if it exists
	err := os.MkdirAll(destPath, 0750)
	if err != nil {
		return err
	}

	out, err := exec.Command(mountCmd, mountArgs...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("mounting failed: %v cmd: '%s %s' output: %q",
			err, mountCmd, strings.Join(mountArgs, " "), string(out))
	}

	return nil
}

// uMount (un-mount) the mooseFs filesystem (possibly deprecated for k8s/mount)
func (m *mounter) uMount(destPath string) error {
	umountCmd := "umount"
	umountArgs := []string{}
	if destPath == "" {
		return errors.New("Destination path not specified for unmounting volume")
	}
	umountArgs = append(umountArgs, destPath)
	out, err := exec.Command(umountCmd, umountArgs...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("mounting failed: %v cmd: '%s %s' output: %q",
			err, umountCmd, strings.Join(umountArgs, " "), string(out))
	}
	return nil
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

func (m *mounter) isLikelyNotMountPoint(mountPoint string) (bool, error) {
	switch errMsg := "%s is not specified for checking the mount"; "" {
	case mountPoint:
		return false, fmt.Errorf(errMsg, "path")
	}

	fsys, err := listMountsAtPoint(mountPoint)
	if err != nil {
		return false, err
	}
	return len(fsys) == 0, nil
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
