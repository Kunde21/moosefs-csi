package metastore

import (
	"context"
	"encoding/json"
	"errors"
	"io/ioutil"
	"os"
	"path/filepath"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Store struct {
	// path to the root of the metadata storage.
	path string
}

type Volume struct {
	Name     string `json:"name"`
	Capacity int64  `json:"capacity"`
	// Dynamic provisioned volume.
	Dynamic bool   `json:"dynamic"`
	Node    string `json:"node"`
	// Path of the directory on the controller.
	Path string `json:"path"`
	// Path of the directory within the moosefs cluster.
	MFSPath string `json:"mfs_path"`
}

// New metadata storage.
func New(path string) *Store {
	return &Store{
		path: path,
	}
}

// CreateVol within the moosefs cluster.
func (o *Store) CreateVol(ctx context.Context, v Volume) error {
	// check if vol exists
	vp := filepath.Join(o.path, v.Name)
	ex, err := fileExist(vp)
	if err != nil {
		return err
	}
	if ex {
		return status.Error(codes.AlreadyExists, "volume already exists")
	}
	// write metadata
	buf, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return ioutil.WriteFile(vp, buf, 0744)
}

// ReadVol within the moosefs cluster.
func (o *Store) ReadVol(ctx context.Context, name string) (*Volume, error) {
	vp := filepath.Join(o.path, name)
	buf, err := ioutil.ReadFile(vp)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, status.Error(codes.NotFound, "volume does not exist")
		}
		return nil, err
	}
	var v Volume
	if err := json.Unmarshal(buf, &v); err != nil {
		return nil, err
	}
	return &v, nil
}

// UpdateVol within the moosefs cluster.
func (o *Store) UpdateVol(ctx context.Context, v Volume) error {
	// check if vol exists
	vp := filepath.Join(o.path, v.Name)
	ex, err := fileExist(vp)
	if err != nil {
		return err
	}
	if !ex {
		return status.Error(codes.NotFound, "volume does not exist")
	}
	// write metadata
	buf, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return ioutil.WriteFile(vp, buf, 0744)
}

// DeleteVol within the moosefs cluster.
func (o *Store) DeleteVol(ctx context.Context, name string) error {
	return os.Remove(filepath.Join(o.path, name))
}

func fileExist(p string) (bool, error) {
	fi, err := os.Stat(p)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, status.Errorf(codes.Internal, "file system error: %v", err)
	}
	if fi.IsDir() {
		return false, status.Errorf(codes.FailedPrecondition, "path %q is not a meta file", p)
	}
	return true, nil
}
