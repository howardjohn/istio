package match

import (
	xds "github.com/cncf/xds/go/xds/core/v3"
	matcher "github.com/cncf/xds/go/xds/type/matcher/v3"
	network "github.com/envoyproxy/go-control-plane/envoy/extensions/matching/common_inputs/network/v3"
	wrappers "google.golang.org/protobuf/types/known/wrapperspb"
	"istio.io/istio/pkg/log"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/util/protoconv"
	"istio.io/istio/pilot/pkg/xds/filters"
)

var (
	DestinationPort = &xds.TypedExtensionConfig{
		Name:        "port",
		TypedConfig: protoconv.MessageToAny(&network.DestinationPortInput{}),
	}
	DestinationIP = &xds.TypedExtensionConfig{
		Name:        "ip",
		TypedConfig: protoconv.MessageToAny(&network.DestinationIPInput{}),
	}
	SourceIP = &xds.TypedExtensionConfig{
		Name:        "source-ip",
		TypedConfig: protoconv.MessageToAny(&network.SourceIPInput{}),
	}
	SNI = &xds.TypedExtensionConfig{
		Name:        "sni",
		TypedConfig: protoconv.MessageToAny(&network.ServerNameInput{}),
	}
	ApplicationProtocolInput = &xds.TypedExtensionConfig{
		Name:        "application-protocol",
		TypedConfig: protoconv.MessageToAny(&network.ApplicationProtocolInput{}),
	}
	TransportProtocolInput = &xds.TypedExtensionConfig{
		Name:        "transport-protocol",
		TypedConfig: protoconv.MessageToAny(&network.TransportProtocolInput{}),
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

func newMapper(input *xds.TypedExtensionConfig) Mapper {
	m := map[string]*matcher.Matcher_OnMatch{}
	match := &matcher.Matcher{
		MatcherType: &matcher.Matcher_MatcherTree_{
			MatcherTree: &matcher.Matcher_MatcherTree{
				Input: input,
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

func NewSourceIP() Mapper {
	return newMapper(SourceIP)
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
				TypedConfig: protoconv.MessageToAny(&wrappers.StringValue{Value: name}),
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

// BuildMatcher cleans the entire match tree to avoid empty maps and returns a viable top-level matcher.
// Note: this mutates the internal mappers/matchers that make up the tree.
func (m Mapper) BuildMatcher() *matcher.Matcher {
	root := m
	for len(root.Map) == 0 {
		// the top level matcher is empty; if its fallback goes to a matcher, return that
		// TODO is there a way we can just say "always go to action"?
		if fallback := root.GetOnNoMatch(); fallback != nil {
			if replacement, ok := mapperFromMatch(fallback.GetMatcher()); ok {
				root = replacement
				continue
			}
		}
		// no fallback or fallback isn't a mapper
		log.Warnf("could not repair invalid matcher; empty map at root matcher does not have a map fallback")
		return nil
	}
	q := []*matcher.Matcher_OnMatch{m.OnNoMatch}
	for _, onMatch := range root.Map {
		q = append(q, onMatch)
	}

	// fix the matchers, add child mappers OnMatch to the queue
	for len(q) > 0 {
		head := q[0]
		q = q[1:]
		q = append(q, fixEmptyOnMatchMap(head)...)
	}
	return root.Matcher
}

// if the onMatch sends to an empty mapper, make the onMatch send directly to the onNoMatch of that empty mapper
// returns mapper if it doesn't need to be fixed, or can't be fixed
func fixEmptyOnMatchMap(onMatch *matcher.Matcher_OnMatch) []*matcher.Matcher_OnMatch {
	if onMatch == nil {
		return nil
	}
	innerMatcher := onMatch.GetMatcher()
	if innerMatcher == nil {
		// this already just performs an Action
		return nil
	}
	innerMapper, ok := mapperFromMatch(innerMatcher)
	if !ok {
		// this isn't a mapper or action, not supported by this func
		return nil
	}
	if len(innerMapper.Map) > 0 {
		return innerMapper.allOnMatches()
	}

	if fallback := innerMapper.GetOnNoMatch(); fallback != nil {
		// change from: onMatch -> map (empty with fallback) to onMatch -> fallback
		// that fallback may be an empty map, so we re-queue onMatch in case it still needs fixing
		onMatch.OnMatch = fallback.OnMatch
		return []*matcher.Matcher_OnMatch{onMatch} // the inner mapper is gone
	}

	// envoy will nack this eventually
	log.Warnf("empty mapper %v with no fallback", innerMapper.Matcher)
	return innerMapper.allOnMatches()
}

func mapperFromMatch(mmatcher *matcher.Matcher) (Mapper, bool) {
	if mmatcher == nil {
		return Mapper{}, false
	}
	switch m := mmatcher.MatcherType.(type) {
	case *matcher.Matcher_MatcherTree_:
		var mmap *matcher.Matcher_MatcherTree_MatchMap
		switch t := m.MatcherTree.TreeType.(type) {
		case *matcher.Matcher_MatcherTree_PrefixMatchMap:
			mmap = t.PrefixMatchMap
		case *matcher.Matcher_MatcherTree_ExactMatchMap:
			mmap = t.ExactMatchMap
		default:
			return Mapper{}, false
		}
		return Mapper{Matcher: mmatcher, Map: mmap.Map}, true
	}
	return Mapper{}, false
}

func (m Mapper) allOnMatches() []*matcher.Matcher_OnMatch {
	var out []*matcher.Matcher_OnMatch
	out = append(out, m.OnNoMatch)
	if m.Map == nil {
		return out
	}
	for _, match := range m.Map {
		out = append(out, match)
	}
	return out
}
