package main

import (
	"context"
	"fmt"
	"k8s.io/kubectl/pkg/util"
	"net"
	"net/http"
	"net/http/pprof"
	"os"
	"os/signal"
	"runtime"
	"syscall"

	"google.golang.org/grpc"

	"k8s.io/klog/v2"
	registerapi "k8s.io/kubelet/pkg/apis/pluginregistration/v1"
)

func nodeRegister(csiDriverName, httpEndpoint string) {
	// When kubeletRegistrationPath is specified then driver-registrar ONLY acts
	// as gRPC server which replies to registration requests initiated by kubelet's
	// plugins watcher infrastructure. Node labeling is done by kubelet's csi code.
	registrar := newRegistrationServer(csiDriverName, *kubeletRegistrationPath, supportedVersions)
	socketPath := buildSocketPath(csiDriverName)
	if err := util.CleanupSocketFile(socketPath); err != nil {
		klog.Errorf("%+v", err)
		os.Exit(1)
	}

	var oldmask int
	if runtime.GOOS == "linux" {
		// Default to only user accessible socket, caller can open up later if desired
		oldmask, _ = util.Umask(0077)
	}

	klog.Infof("Starting Registration Server at: %s\n", socketPath)
	lis, err := net.Listen("unix", socketPath)
	if err != nil {
		klog.Errorf("failed to listen on socket: %s with error: %+v", socketPath, err)
		os.Exit(1)
	}
	if runtime.GOOS == "linux" {
		util.Umask(oldmask)
	}
	klog.Infof("Registration Server started at: %s\n", socketPath)
	grpcServer := grpc.NewServer()

	// Registers kubelet plugin watcher api.
	registerapi.RegisterRegistrationServer(grpcServer, registrar)

	go httpServer(socketPath, httpEndpoint, csiDriverName)
	go removeRegSocket(csiDriverName)
	// Starts service
	if err := grpcServer.Serve(lis); err != nil {
		klog.Errorf("Registration Server stopped serving: %v", err)
		os.Exit(1)
	}

	// If gRPC server is gracefully shutdown, cleanup and exit
	os.Exit(0)
}

func buildSocketPath(csiDriverName string) string {
	return fmt.Sprintf("%s/%s-reg.sock", *pluginRegistrationPath, csiDriverName)
}

func httpServer(socketPath string, httpEndpoint string, csiDriverName string) {
	if httpEndpoint == "" {
		klog.Infof("Skipping HTTP server because endpoint is set to: %q", httpEndpoint)
		return
	}
	klog.Infof("Starting HTTP server at endpoint: %v\n", httpEndpoint)

	// Prepare http endpoint for healthz + profiling (if enabled)
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, req *http.Request) {
		socketExists, err := util.DoesSocketExist(socketPath)
		if err == nil && socketExists {
			grpcSocketCheckError := checkLiveRegistrationSocket(socketPath, csiDriverName)
			if grpcSocketCheckError != nil {
				w.WriteHeader(http.StatusInternalServerError)
				w.Write([]byte(grpcSocketCheckError.Error()))
				klog.Errorf("health check failed: %+v", grpcSocketCheckError)
			} else {
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(`ok`))
				klog.V(5).Infof("health check succeeded")
			}
		} else if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(err.Error()))
			klog.Errorf("health check failed: %+v", err)
		} else if !socketExists {
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte("registration socket does not exist"))
			klog.Errorf("health check failed, registration socket does not exist")
		}
	})

	if *enableProfile {
		klog.InfoS("Starting profiling", "endpoint", httpEndpoint)

		mux.HandleFunc("/debug/pprof/", pprof.Index)
		mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
		mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
		mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
		mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
	}

	klog.Fatal(http.ListenAndServe(httpEndpoint, mux))
}

func checkLiveRegistrationSocket(socketFile, csiDriverName string) error {
	klog.V(2).Infof("Attempting to open a gRPC connection with: %q", socketFile)
	grpcConn, err := connection.ConnectWithoutMetrics(socketFile)
	if err != nil {
		return fmt.Errorf("error connecting to node-registrar socket %s: %v", socketFile, err)
	}

	defer closeGrpcConnection(socketFile, grpcConn)

	klog.V(2).Infof("Calling node registrar to check if it still responds")
	ctx, cancel := context.WithTimeout(context.Background(), *operationTimeout)
	defer cancel()

	client := registerapi.NewRegistrationClient(grpcConn)

	infoRequest := &registerapi.InfoRequest{}

	info, err := client.GetInfo(ctx, infoRequest)
	if err != nil {
		return fmt.Errorf("error getting info from node-registrar socket: %v", err)
	}

	if info.Name == csiDriverName {
		return nil
	}
	return fmt.Errorf("invalid driver name %s", info.Name)
}

func closeGrpcConnection(socketFile string, conn *grpc.ClientConn) {
	err := conn.Close()
	if err != nil {
		klog.Errorf("error closing socket %s: %v", socketFile, err)
		os.Exit(1)
	}
}

func removeRegSocket(csiDriverName string) {
	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, syscall.SIGTERM)
	<-sigc
	socketPath := buildSocketPath(csiDriverName)
	err := os.Remove(socketPath)
	if err != nil && !os.IsNotExist(err) {
		klog.Errorf("failed to remove socket: %s with error: %+v", socketPath, err)
		os.Exit(1)
	}
	os.Exit(0)
}
