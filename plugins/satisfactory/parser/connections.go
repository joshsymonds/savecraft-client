package main

import (
	"strings"

	"github.com/joshsymonds/savecraft.gg/plugins/satisfactory/sav"
)

// connEdge is one logistics link between two actors, captured from a
// connection component's mConnectedComponent reference. from/to are actor
// instance paths (the owning machine/belt/splitter/etc.), transport is "belt"
// or "pipe". Edges are undirected for line-connectivity purposes; both ends
// of a physical link emit a mirror edge.
type connEdge struct {
	from      string
	to        string
	transport string
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

// connEdgeFrom builds the edge for a connection component, or returns ok=false
// when the component is unconnected (no mConnectedComponent, e.g. a pole's
// snap-only point).
func connEdgeFrom(instanceName string, od *sav.ObjectData, transport string) (connEdge, bool) {
	ref, ok := prop[sav.ObjectRef](od, "mConnectedComponent")
	if !ok || ref.Path == "" {
		return connEdge{}, false
	}
	return connEdge{from: actorOf(instanceName), to: actorOf(ref.Path), transport: transport}, true
}
