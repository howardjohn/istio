package sds

import (
	"context"

	xdsapiv2 "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	discoveryv2 "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v2"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	v2 "istio.io/istio/pilot/pkg/proxy/envoy/v2"
)

// sdsServiceAdapter is a sdsservice that converts v3 Discovery messages to v2 messages.
// See notes in DiscoveryStreamV2Adapter for more info.
type sdsServiceAdapter struct {
	s *sdsservice
}

func (d *sdsServiceAdapter) FetchSecrets(ctx context.Context, request *xdsapiv2.DiscoveryRequest) (*xdsapiv2.DiscoveryResponse, error) {
	v3Resp, err := d.s.FetchSecrets(ctx, v2.UpgradeV2Request(request))
	return v2.DowngradeV3Response(v3Resp), err
}

// We implement the v2 ADS API
var _ discoveryv2.SecretDiscoveryServiceServer = &sdsServiceAdapter{}

func (d *sdsServiceAdapter) StreamSecrets(stream discoveryv2.SecretDiscoveryService_StreamSecretsServer) error {
	v3Stream := &v2.DiscoveryStreamV2Adapter{stream}
	sdsServiceLog.Infof("SDS: starting legacy v2 discovery stream")
	return d.s.StreamSecrets(v3Stream)
}

func (d sdsServiceAdapter) DeltaSecrets(stream discoveryv2.SecretDiscoveryService_DeltaSecretsServer) error {
	return status.Error(codes.Unimplemented, "DeltaSecrets not implemented")
}

func (s *sdsservice) createV2Adapter() discoveryv2.SecretDiscoveryServiceServer {
	return &sdsServiceAdapter{s}
}
