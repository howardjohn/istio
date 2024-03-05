package match

import (
	xds "github.com/cncf/xds/go/xds/core/v3"
	matcher "github.com/cncf/xds/go/xds/type/matcher/v3"
	network "github.com/envoyproxy/go-control-plane/envoy/extensions/matching/common_inputs/network/v3"
	wrappers "google.golang.org/protobuf/types/known/wrapperspb"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pilot/pkg/xds/filters"
)

var (
	DestinationPort = &xds.TypedExtensionConfig{
		Name:        "port",
		TypedConfig: util.MessageToAny(&network.DestinationPortInput{}),
	}
	DestinationIP = &xds.TypedExtensionConfig{
		Name:        "ip",
		TypedConfig: util.MessageToAny(&network.DestinationIPInput{}),
	}
	SNI = &xds.TypedExtensionConfig{
		Name:        "sni",
		TypedConfig: util.MessageToAny(&network.ServerNameInput{}),
	}
	ApplicationProtocolInput = &xds.TypedExtensionConfig{
		Name:        "application-protocol",
		TypedConfig: util.MessageToAny(&network.ApplicationProtocolInput{}),
	}
	TransportProtocolInput = &xds.TypedExtensionConfig{
		Name:        "transport-protocol",
		TypedConfig: util.MessageToAny(&network.TransportProtocolInput{}),
	}
)

type Mapper struct {
	*matcher.Matcher
	Map map[string]*matcher.Matcher_OnMatch
}

type ProtocolMatch struct {
	TCP, TLS, HTTP *matcher.Matcher_OnMatch
}

// NewAppProtocol defines a matcher that performs an action depending on if traffic is HTTP or TCP.
func NewAppProtocol(pm ProtocolMatch) *matcher.Matcher {
	appMap := map[string]*matcher.Matcher_OnMatch{
		"'h2c'":      pm.HTTP,
		"'http/1.1'": pm.HTTP,
	}
	if features.HTTP10 {
		appMap["'http/1.0'"] = pm.HTTP
	}
	return &matcher.Matcher{
		MatcherType: &matcher.Matcher_MatcherTree_{
			MatcherTree: &matcher.Matcher_MatcherTree{
				Input: ApplicationProtocolInput,
				TreeType: &matcher.Matcher_MatcherTree_ExactMatchMap{
					ExactMatchMap: &matcher.Matcher_MatcherTree_MatchMap{
						Map: appMap,
					},
				},
			},
		},
		OnNoMatch: pm.TCP,
	}
}

// NewProtocol defines a matcher that performs an action depending on if traffic is HTTP, TLS, or TCP.
func NewProtocol(pm ProtocolMatch) *matcher.Matcher {
	appMap := map[string]*matcher.Matcher_OnMatch{
		"h2c":      pm.HTTP,
		"http/1.1": pm.HTTP,
	}
	if features.HTTP10 {
		appMap["http/1.0"] = pm.HTTP
	}
	application := &matcher.Matcher{
		MatcherType: &matcher.Matcher_MatcherTree_{
			MatcherTree: &matcher.Matcher_MatcherTree{
				Input: ApplicationProtocolInput,
				TreeType: &matcher.Matcher_MatcherTree_ExactMatchMap{
					ExactMatchMap: &matcher.Matcher_MatcherTree_MatchMap{
						Map: appMap,
					},
				},
			},
		},
		OnNoMatch: pm.TCP,
	}
	transport := &matcher.Matcher{
		MatcherType: &matcher.Matcher_MatcherTree_{
			MatcherTree: &matcher.Matcher_MatcherTree{
				Input: TransportProtocolInput,
				TreeType: &matcher.Matcher_MatcherTree_ExactMatchMap{
					ExactMatchMap: &matcher.Matcher_MatcherTree_MatchMap{
						Map: map[string]*matcher.Matcher_OnMatch{
							filters.RawBufferTransportProtocol: ToMatcher(application),
							filters.TLSTransportProtocol:       pm.TLS,
						},
					},
				},
			},
		},
	}
	return transport
}

func NewDestinationIP() Mapper {
	m := map[string]*matcher.Matcher_OnMatch{}
	match := &matcher.Matcher{
		MatcherType: &matcher.Matcher_MatcherTree_{
			MatcherTree: &matcher.Matcher_MatcherTree{
				Input: DestinationIP,
				TreeType: &matcher.Matcher_MatcherTree_ExactMatchMap{
					ExactMatchMap: &matcher.Matcher_MatcherTree_MatchMap{
						Map: m,
					},
				},
			},
		},
		OnNoMatch: nil,
	}
	return Mapper{Matcher: match, Map: m}
}

func NewSNI() Mapper {
	m := map[string]*matcher.Matcher_OnMatch{}
	match := &matcher.Matcher{
		MatcherType: &matcher.Matcher_MatcherTree_{
			MatcherTree: &matcher.Matcher_MatcherTree{
				Input: SNI,
				// TODO(https://github.com/cncf/xds/pull/24/files) this should be exact+prefix matcher
				TreeType: &matcher.Matcher_MatcherTree_ExactMatchMap{
					ExactMatchMap: &matcher.Matcher_MatcherTree_MatchMap{
						Map: m,
					},
				},
			},
		},
		OnNoMatch: nil,
	}
	return Mapper{Matcher: match, Map: m}
}

func NewDestinationPort() Mapper {
	m := map[string]*matcher.Matcher_OnMatch{}
	match := &matcher.Matcher{
		MatcherType: &matcher.Matcher_MatcherTree_{
			MatcherTree: &matcher.Matcher_MatcherTree{
				Input: DestinationPort,
				TreeType: &matcher.Matcher_MatcherTree_ExactMatchMap{
					ExactMatchMap: &matcher.Matcher_MatcherTree_MatchMap{
						Map: m,
					},
				},
			},
		},
		OnNoMatch: nil,
	}
	return Mapper{Matcher: match, Map: m}
}

func ToChain(name string) *matcher.Matcher_OnMatch {
	return &matcher.Matcher_OnMatch{
		OnMatch: &matcher.Matcher_OnMatch_Action{
			Action: &xds.TypedExtensionConfig{
				Name:        name,
				TypedConfig: util.MessageToAny(&wrappers.StringValue{Value: name}),
			},
		},
	}
}

func ToMatcher(match *matcher.Matcher) *matcher.Matcher_OnMatch {
	return &matcher.Matcher_OnMatch{
		OnMatch: &matcher.Matcher_OnMatch_Matcher{
			Matcher: match,
		},
	}
}
