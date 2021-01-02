package mfsexec

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"net"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"k8s.io/apimachinery/pkg/api/errors"
)

type Runner struct {
	mfsdir string
	host   string
	port   string
}

// New mfs executor for operating on directories.
func New(dir, mfsServer string) (*Runner, error) {
	if !filepath.IsAbs(dir) {
		return nil, fmt.Errorf("invalid execution directory (not absolute) %q", dir)
	}
	if !strings.Contains(mfsServer, ":") {
		mfsServer = net.JoinHostPort(mfsServer, "9421")
	}
	host, port, err := net.SplitHostPort(mfsServer)
	if err != nil {
		return nil, err
	}
	return &Runner{
		mfsdir: dir,
		host:   host,
		port:   port,
	}, nil
}

func (r *Runner) exec(ctx context.Context, command string, args ...string) (out []byte, err error) {
	cmd := exec.CommandContext(ctx, command, args...)
	cmd.Dir = r.mfsdir
	return cmd.CombinedOutput()
}

// SetQuota for the volume at path, size in bytes.
func (r *Runner) SetQuota(ctx context.Context, path string, size int64) error {
	_, err := r.exec(ctx, "mfssetquota", "-l", strconv.FormatInt(size, 10), path)
	if err != nil {
		return err
	}
	// TODO: Debug log
	return nil
}

func (r *Runner) GetQuota(ctx context.Context, path string) (used, avail int64, err error) {
	out, err := r.exec(ctx, "mfsgetquota", path)
	if err != nil {
		return 0, 0, err
	}
	lines := bytes.Split(out, []byte{'\n'})
	if len(lines) < 3 {
		return 0, 0, errors.NewInternalError(fmt.Errorf("invalid output from mfsgetquota"))
	}
	fld := bytes.Split(lines[2], []byte{'|'})
	if len(fld) < 4 {
		return 0, 0, errors.NewInternalError(fmt.Errorf("invalid row output %q from mfsgetquota", lines[2]))
	}
	if !bytes.Equal(bytes.TrimSpace(fld[0]), []byte("length")) {
		return 0, 0, errors.NewInternalError(fmt.Errorf("invalid output type %q from mfsgetquota", fld[0]))
	}
	sz, err := parseSize(fld[1])
	if err != nil {
		return -1, -1, err
	}
	av, err := parseSize(fld[2])
	if err != nil {
		return sz, -1, nil
	}
	return sz, av, nil
}

func parseSize(sz []byte) (int64, error) {
	sz = bytes.TrimSpace(sz)
	if bytes.Equal(sz, []byte{'-'}) {
		return -1, nil
	}
	return strconv.ParseInt(string(sz), 10, 64)
}

// GetAvailableCap returns available capacity of the moosefs cluster.
func (r *Runner) GetAvailableCap(ctx context.Context) (int64, error) {
	sep := "|"
	args := []string{"-H", r.host}
	if r.port != "" {
		args = append(args, "-P", r.port)
	}
	out, err := r.exec(ctx, "mfscli", append(args, "-SIN", "-s", sep)...)
	if err != nil {
		fmt.Println(string(out))
		return 0, err
	}
	scan := bufio.NewScanner(bytes.NewBuffer(out))
	for scan.Scan() {
		fld := bytes.Split(scan.Bytes(), []byte(sep))
		if len(fld) != 3 {
			continue
		}
		if bytes.EqualFold(fld[0], []byte("master info:")) &&
			bytes.EqualFold(fld[1], []byte("avail space")) {
			return strconv.ParseInt(string(bytes.TrimSpace(fld[2])), 10, 64)
		}
	}
	return 0, fmt.Errorf("invalid mfscli output %q", string(out))
}
