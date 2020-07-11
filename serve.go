package mfscsi

import (
	"context"
	"fmt"
	"net"
	"os"
	"strings"

	"github.com/sirupsen/logrus"
	"google.golang.org/grpc"
)

type Registrar interface {
	Register(*grpc.Server)
}

func Serve(endpoint string, services ...Registrar) error {
	var proto, addr string
	if strings.HasPrefix(strings.ToLower(endpoint), "unix://") ||
		strings.HasPrefix(strings.ToLower(endpoint), "tcp://") {
		s := strings.SplitN(endpoint, "://", 2)
		if s[1] != "" {
			proto, addr = s[0], s[1]
		}
	}
	if proto == "" || addr == "" {
		return fmt.Errorf("Invalid endpoint: %v", endpoint)
	}
	if proto == "unix" {
		addr = "/" + addr
		if err := os.Remove(addr); err != nil && !os.IsNotExist(err) {
			return err
		}
	}

	listener, err := net.Listen(proto, addr)
	if err != nil {
		return err
	}
	server := grpc.NewServer(grpc.ChainUnaryInterceptor(logCmd))
	for _, svc := range services {
		svc.Register(server)
	}
	return server.Serve(listener)
}

func logCmd(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp interface{}, err error) {
	resp, err = handler(ctx, req)
	logrus.New().WithField("mthd", info.FullMethod).WithField("err", err).
		WithField("req", fmt.Sprintf("%v", req)).WithField("resp", fmt.Sprintf("%v", resp)).
		Info("handled")
	return resp, err
}
