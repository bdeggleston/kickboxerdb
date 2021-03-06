package topology

import (
	"fmt"
	"sync"
)

import (
	"partitioner"
)

/**
 What happens when a new datacenter joins?
 * both clusters collect information about each other
 * streaming needs to be reworked to reconcile incoming data

 Cases to handle:
 	* both dcs have data, data needs to be reconciled between both datacenters
 */

/**
 How do datacenter's communicate?
 * All nodes talk to all nodes
 * datacenters pick a random coordinator on each request (why?)
 */

type DatacenterID string

type DatacenterContainer struct {
	rings map[DatacenterID] *Ring
	lock sync.RWMutex
}

func NewDatacenterContainer() *DatacenterContainer {
	dc := &DatacenterContainer{
		rings: make(map[DatacenterID]*Ring),
	}
	return dc
}

func (dc *DatacenterContainer) AddNode(node Node) error {
	dc.lock.Lock()
	defer dc.lock.Unlock()

	dcId := node.GetDatacenterId()
	if _, exists := dc.rings[dcId]; !exists {
		dc.rings[dcId] = NewRing()
	}
	return dc.rings[dcId].AddNode(node)
}

func (dc *DatacenterContainer) Size() int {
	num := 0
	for _, ring := range dc.rings {
		num += ring.Size()
	}
	return num
}

func (dc *DatacenterContainer) AllNodes() []Node {
	dc.lock.RLock()
	defer dc.lock.RUnlock()

	nodes := make([]Node, 0, dc.Size())
	for _, ring := range dc.rings {
		nodes = append(nodes, ring.AllNodes()...)
	}
	return nodes
}

func (dc *DatacenterContainer) GetRing(dcId DatacenterID) (*Ring, error) {
	dc.lock.RLock()
	defer dc.lock.RUnlock()

	ring, exists := dc.rings[dcId]
	if !exists {
		return nil, fmt.Errorf("Unknown datacenter [%v]", dcId)
	}
	return ring, nil
}

// returns a map of datacenter ids -> replica nodes
func (dc *DatacenterContainer) GetNodesForToken(t partitioner.Token, replicationFactor uint32) map[DatacenterID][]Node {
	dc.lock.RLock()
	defer dc.lock.RUnlock()

	// allocate an additional space for the local node when this is used in queries
	nodes := make(map[DatacenterID][]Node, len(dc.rings) + 1)
	for dcid, ring := range dc.rings {
		nodes[dcid] = ring.GetNodesForToken(t, replicationFactor)
	}

	return nodes
}
