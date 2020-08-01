package main

import (
	"os"
	"testing"

	mfscsi "github.com/Kunde21/moosefs-csi"
	mfs "github.com/Kunde21/moosefs-csi/driver"
	"k8s.io/utils/mount"

	"github.com/kubernetes-csi/csi-test/v3/pkg/sanity"
)

func TestSanity(t *testing.T) {
	const testdir = "/tmp/csitesting"
	const root = "/csitest"
	const conDir = "/tmp/controller"
	mfsEP := os.Getenv("MOOSEFS_ENDPOINT")
	if mfsEP == "" {
		t.Skipf("missing %q environment variable, skipping csi-sanity test", "MOOSEFS_ENDPOINT")
	}
	st, err := os.Stat(testdir)
	if err != nil {
		if err := os.MkdirAll(testdir, os.ModeDir); err != nil {
			t.Fatal(err)
		}
	} else if !st.IsDir() {
		t.Fatalf("Test directory %q is not accessible", testdir)
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	ep := "unix://" + wd + "/csi.sock"
	t.Cleanup(func() {
		if err := os.Remove(ep); err != nil {
			t.Log(err)
		}
	})
	nodeID, endpoint, server := "testing", ep, mfsEP
	driver := mfs.NewMFSdriver(nodeID, endpoint, server)
	m, err := mfs.NewMounter()
	if err != nil {
		t.Error(err)
	}
	ns := mfs.NewNodeServer(driver, m, root)
	cs, err := mfs.NewControllerServer(driver, root, conDir)
	if err != nil {
		t.Fatal(err)
	}
	ids, err := mfs.NewIdentityServer(driver)
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		if err := mfscsi.Serve(endpoint, ns, cs, ids); err != nil {
			t.Log(err)
		}
	}()
	t.Cleanup(func() { cs.Close() })

	conf := sanity.NewTestConfig()
	conf.Address = ep
	conf.StagingPath = "/tmp/csitesting/staging"
	conf.RemoveStagingPathCmd = "rmdir"
	conf.RemoveStagingPath = remove(t, m, true)
	conf.TargetPath = "/tmp/csitesting/target"
	conf.RemoveTargetPath = remove(t, m, false)
	sanity.Test(t, conf)
}

func remove(t *testing.T, m mount.Interface, strict bool) func(string) error {
	return func(path string) error {
		t.Log("remove staging", path)
		if err := m.Unmount(path); err != nil && !os.IsNotExist(err) {
			t.Log("unmount stg error", err)
			if strict {
				return err
			}
		}
		if err := os.RemoveAll(path); err != nil {
			t.Log("remove stg error", err)
			if strict {
				return err
			}
		}
		return nil
	}
}
