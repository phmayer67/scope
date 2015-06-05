package main

import (
	"fmt"
	"log"
	"net"
	"strconv"
	"time"

	"github.com/weaveworks/procspy"
	"github.com/weaveworks/scope/report"
)

// spy invokes procspy.Connections to generate a report.Report that contains
// every discovered (spied) connection on the host machine, at the granularity
// of host and port. It optionally enriches that topology with process (PID)
// information.
func spy(
	hostID, hostName string,
	includeProcesses bool,
	pms []processMapper,
) report.Report {
	defer func(begin time.Time) {
		spyDuration.WithLabelValues().Observe(float64(time.Since(begin)))
	}(time.Now())

	r := report.MakeReport()

	conns, err := procspy.Connections(includeProcesses)
	if err != nil {
		log.Printf("spy connections: %v", err)
		return r
	}

	for conn := conns.Next(); conn != nil; conn = conns.Next() {
		addConnection(&r, conn, hostID, hostName, pms)
	}

	return r
}

func addConnection(
	r *report.Report,
	c *procspy.Connection,
	hostID, hostName string,
	pms []processMapper,
) {
	var (
		scopedLocal  = scopedIP(hostID, c.LocalAddress)
		scopedRemote = scopedIP(hostID, c.RemoteAddress)
		key          = hostID + report.IDDelim + scopedLocal
		edgeKey      = scopedLocal + report.IDDelim + scopedRemote
	)

	r.Network.Adjacency[key] = r.Network.Adjacency[key].Add(scopedRemote)

	if _, ok := r.Network.NodeMetadatas[scopedLocal]; !ok {
		r.Network.NodeMetadatas[scopedLocal] = report.NodeMetadata{
			"name": hostName,
		}
	}

	// Count the TCP connection.
	edgeMeta := r.Network.EdgeMetadatas[edgeKey]
	edgeMeta.WithConnCountTCP = true
	edgeMeta.MaxConnCountTCP++
	r.Network.EdgeMetadatas[edgeKey] = edgeMeta

	if c.Proc.PID > 0 {
		var (
			scopedLocal  = scopedIPPort(hostID, c.LocalAddress, c.LocalPort)
			scopedRemote = scopedIPPort(hostID, c.RemoteAddress, c.RemotePort)
			key          = hostID + report.IDDelim + scopedLocal
			edgeKey      = scopedLocal + report.IDDelim + scopedRemote
		)

		r.Process.Adjacency[key] = r.Process.Adjacency[key].Add(scopedRemote)

		if _, ok := r.Process.NodeMetadatas[scopedLocal]; !ok {
			// First hit establishes NodeMetadata for scoped local address + port
			md := report.NodeMetadata{
				"pid":    fmt.Sprintf("%d", c.Proc.PID),
				"name":   c.Proc.Name,
				"domain": hostID,
			}

			for _, pm := range pms {
				v, err := pm.Map(c.PID)
				if err != nil {
					continue
				}
				md[pm.Key()] = v
			}

			r.Process.NodeMetadatas[scopedLocal] = md
		}
		// Count the TCP connection.
		edgeMeta := r.Process.EdgeMetadatas[edgeKey]
		edgeMeta.WithConnCountTCP = true
		edgeMeta.MaxConnCountTCP++
		r.Process.EdgeMetadatas[edgeKey] = edgeMeta
	}
}

// scopedIP makes an IP unique over multiple networks.
func scopedIP(scope string, ip net.IP) string {
	if ip.IsLoopback() {
		return scope + report.ScopeDelim + ip.String()
	}
	return report.ScopeDelim + ip.String()
}

// scopedIPPort makes an IP+port tuple unique over multiple networks.
func scopedIPPort(scope string, ip net.IP, port uint16) string {
	return scopedIP(scope, ip) + report.ScopeDelim + strconv.FormatUint(uint64(port), 10)
}
