package main

import (
	"strings"

	"github.com/joshsymonds/savecraft-client/plugins/satisfactory/sav"
)

// connEdge is one logistics link between two actors, captured from a
// connection component's mConnectedComponent reference. from/to are actor
// instance paths (the owning machine/belt/splitter/etc.), transport is "belt"
// or "pipe". Both ends of a physical link emit a mirror edge, so line
// connectivity treats from/to symmetrically (union-find unions both).
//
// directed is set when the owning connector names its flow side — an "Output"
// connector means flow leaves the owner (from=owner→to=connected), an "Input"
// connector means flow enters it (from=connected→to=owner). Belt-side
// "ConveyorAny" connectors and pipe connectors carry no side, so directed is
// false (connectivity still holds). The flow side comes from the connector
// instance-name suffix, not the mDirection property — that property is
// inconsistently serialized across saves.
type connEdge struct {
	from      string
	to        string
	transport string
	directed  bool
}

// isConnectionComponent reports whether o is a belt/pipe connection component,
// and which transport it carries.
func isConnectionComponent(classPath string) (transport string, ok bool) {
	switch {
	case strings.Contains(classPath, "FGFactoryConnectionComponent"):
		return "belt", true
	case strings.Contains(classPath, "FGPipeConnection"):
		return "pipe", true
	}
	return "", false
}

// actorOf strips the trailing ".<connName>" from a connection component's
// instance path, yielding its owning actor's instance path.
func actorOf(connInstancePath string) string {
	if i := strings.LastIndex(connInstancePath, "."); i >= 0 {
		return connInstancePath[:i]
	}
	return connInstancePath
}

// connectorName returns a connection component's local name — the suffix after
// the owning actor's path, e.g. "Output0", "Input3", "ConveyorAny1".
func connectorName(connInstancePath string) string {
	if i := strings.LastIndex(connInstancePath, "."); i >= 0 {
		return connInstancePath[i+1:]
	}
	return connInstancePath
}

// connEdgeFrom builds the edge for a connection component, or returns ok=false
// when the component is unconnected (no mConnectedComponent, e.g. a pole's
// snap-only point). Direction is taken from the owning connector's name: an
// "Output" connector orients flow owner→connected, an "Input" connector
// orients connected→owner; any other connector (belt "ConveyorAny", pipe) is
// left undirected but still contributes connectivity.
func connEdgeFrom(instanceName string, od *sav.ObjectData, transport string) (connEdge, bool) {
	ref, ok := prop[sav.ObjectRef](od, "mConnectedComponent")
	if !ok || ref.Path == "" {
		return connEdge{}, false
	}
	owner, connected := actorOf(instanceName), actorOf(ref.Path)
	switch conn := connectorName(instanceName); {
	case strings.HasPrefix(conn, "Output"):
		return connEdge{from: owner, to: connected, transport: transport, directed: true}, true
	case strings.HasPrefix(conn, "Input"):
		return connEdge{from: connected, to: owner, transport: transport, directed: true}, true
	default:
		return connEdge{from: owner, to: connected, transport: transport}, true
	}
}
