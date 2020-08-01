package bolt

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"time"

	"go.etcd.io/bbolt"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const bucketName = "csi.kunde21.moosefs"

type VolStore struct {
	bdb    *bbolt.DB
	bucket []byte
}

type Volume struct {
	ID       string
	Capacity int64
	Params   map[string]string
}

// New volume storage
func New(fname string) (*VolStore, error) {
	bdb, err := bbolt.Open(fname, 0744, &bbolt.Options{Timeout: 5 * time.Second})
	if err != nil {
		return nil, fmt.Errorf("failed to create tracking store: %w", err)
	}

	if err := bdb.Update(func(tx *bbolt.Tx) error {
		b, err := tx.CreateBucketIfNotExists([]byte(bucketName))
		if err != nil {
			return err
		}
		stats := b.Stats()
		fmt.Println("tracking store:", stats)
		return nil
	}); err != nil {
		return nil, fmt.Errorf("failed to initialize tracking store: %w", err)
	}
	return &VolStore{
		bdb:    bdb,
		bucket: []byte(bucketName),
	}, nil
}

// Close the store and release resources.
func (vs *VolStore) Close() error {
	return vs.bdb.Close()
}

// GetVolume fetches the volume, if it exists.
func (vs *VolStore) GetVolume(ctx context.Context, id string) (*Volume, error) {
	var v *Volume
	return v, vs.bdb.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket(vs.bucket)
		vol := b.Get([]byte(id))
		if vol == nil {
			return status.Errorf(codes.NotFound, "volume %q does not exist", id)
		}
		return json.Unmarshal(vol, &v)
	})
}

// InsertVolume registers a new volume.
// Noop if the same volume is already registered.
func (vs *VolStore) InsertVolume(ctx context.Context, vol Volume) error {
	if vol.ID == "" {
		return fmt.Errorf("misssing volume ID")
	}
	v, err := json.Marshal(vol)
	if err != nil {
		return fmt.Errorf("invalid json: %w", err)
	}
	return vs.bdb.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket(vs.bucket)
		svol := b.Get([]byte(vol.ID))
		if svol != nil && !bytes.Equal(v, svol) {
			return status.Error(codes.AlreadyExists, "volume exists")
		}
		return b.Put([]byte(vol.ID), v)
	})
}

// DeleteVolume unregisters the volume, if it exists (noop if it doesn't).
func (vs *VolStore) DeleteVolume(ctx context.Context, id string) error {
	if id == "" {
		return fmt.Errorf("misssing volume ID")
	}
	return vs.bdb.Update(func(tx *bbolt.Tx) error {
		return tx.Bucket(vs.bucket).Delete([]byte(id))
	})
}
