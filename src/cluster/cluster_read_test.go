package cluster

import (
	"fmt"
	"testing"
	"time"
)

import (
	"store"
	"testing_helpers"
)

// sets up 4 dcs with 10 nodes each
func setupReadTestCluster(t *testing.T, s store.Store) *Cluster {
	partitioner := NewMD5Partitioner()
	c, err := NewCluster(
		s,
		"127.0.0.1:9999",
		"Test Cluster",
		partitioner.GetToken("0"),
		NewNodeId(),
		DatacenterId("DC0000"),
		3,
		partitioner,
		nil,
	)
	if err != nil {
		panic(fmt.Sprintf("Unexpected error instantiating cluster: %v", err))
	}

	// remove local node
	c.ring = NewRing()
	c.localNode = nil

	for x:=0; x<4; x++ {
		dcid := DatacenterId(fmt.Sprintf("DC%v000", x))
		for y:=0; y<10; y++ {
			node := newMockNode(
				NewNodeId(),
				dcid,
				partitioner.GetToken(fmt.Sprint(y)),
				fmt.Sprintf("D%vN%v", x, y),
			)
			node.testPtr = t
			c.addNode(node)
		}
	}
	return c
}

// tests that an invalid read command passed
// into execute read returns an error
func TestInvalidReadCommand(t *testing.T) {
	s := newMockStore()
	s.isRead = false
	c := setupReadTestCluster(t, s)
	val, err := c.ExecuteRead("GET", "a", []string{}, CONSISTENCY_ONE, time.Duration(10), false)
	if val != nil {
		t.Errorf("Expected nil value, got: %v", val)
	}
	if err == nil {
		t.Errorf("Expected error, got nil")
	}
}

// tests consistency ONE where all nodes respond
func TestReadSuccessCaseCLONE(t *testing.T) {
	mStore := newMockStore()
	tCluster := setupReadTestCluster(t, mStore)
	key := "a"

	// send responses to nodes
	expectedVal := newMockString("b", time.Now())
	nodeMap := tCluster.GetNodesForKey(key)
	for dcid, nodes := range nodeMap {
		if dcid != tCluster.GetDatacenterId() { continue }
		for _, node := range nodes {
			mNode := node.(*mockNode)
			mNode.addReadResponse(expectedVal, nil)
		}
	}

	mStore.addReconcileResponse(expectedVal, make(map[string][]*store.Instruction), nil)
	mStore.addReconcileResponse(expectedVal, make(map[string][]*store.Instruction), nil)

	timeout := time.Duration(1)
	val, err := tCluster.ExecuteRead("GET", key, []string{}, CONSISTENCY_ONE, timeout, false)
	if err != nil {
		t.Errorf("Unexpected error executing read: %v", err)
	}

	// wait for reconciliation to finish
	start := time.Now()
	for len(mStore.reconcileCalls) < 2 {
		time.Sleep(time.Duration(1 * time.Millisecond))
		if (time.Now().After(start.Add(timeout * time.Millisecond * 2))){
			break
		}
	}

	if val == nil || !expectedVal.Equal(val) {
		t.Errorf("expected and actual value are not equal. Expected: %v, Actual %v", expectedVal, val)
	}

	// check that local nodes were queried properly
	for _, rnode := range nodeMap[tCluster.GetDatacenterId()] {
		node := rnode.(*mockNode)
		nodefmt := func(s string) string { return fmt.Sprintf("Node %v: %v", node.Name(), s) }
		testing_helpers.AssertEqual(t, nodefmt("read size"), 1, len(node.reads))
		read := node.reads[0]
		testing_helpers.AssertEqual(t, nodefmt("cmd"), "GET", read.cmd)
		testing_helpers.AssertEqual(t, nodefmt("key"), key, read.key)
		testing_helpers.AssertEqual(t, nodefmt("num args"), 0, len(read.args))

	}

	// check that remote nodes were not queried
	for dcid, nodes := range nodeMap {
		// skip local cluster
		if dcid == tCluster.GetDatacenterId() {
			continue
		}
		for _, rnode := range nodes {
			node := rnode.(*mockNode)
			nodefmt := func(s string) string { return fmt.Sprintf("Node %v: %v", node.Name(), s) }
			testing_helpers.AssertEqual(t, nodefmt("read size"), 0, len(node.reads))
		}
	}

	// check that no writes (reconciliations) were issued against the nodes
	for _, nodes := range nodeMap {
		for _, rnode := range nodes {
			node := rnode.(*mockNode)
			nodefmt := func(s string) string { return fmt.Sprintf("Node %v: %v", node.Name(), s) }
			testing_helpers.AssertEqual(t, nodefmt("write size"), 0, len(node.writes))
		}
	}

	// check that reconcile was called twice
	testing_helpers.AssertEqual(t, "reconcile calls", 2, len(mStore.reconcileCalls))
}

// tests consistency ONE where consistency is satisfied
// but not all nodes return a response
func TestReadPartialSuccessCaseCLONE(t *testing.T) {
	mStore := newMockStore()
	tCluster := setupReadTestCluster(t, mStore)
	key := "a"

	// send responses to nodes
	expectedVal := newMockString("b", time.Now())
	nodeMap := tCluster.GetNodesForKey(key)
	for dcid, nodes := range nodeMap {
		if dcid != tCluster.GetDatacenterId() { continue }
		for idx, node := range nodes {
			// we only want one node to respond
			if idx != 0 { continue }
			mNode := node.(*mockNode)
			mNode.addReadResponse(expectedVal, nil)
		}
	}

	mStore.addReconcileResponse(expectedVal, make(map[string][]*store.Instruction), nil)
	mStore.addReconcileResponse(expectedVal, make(map[string][]*store.Instruction), nil)

	timeout := time.Duration(1)
	val, err := tCluster.ExecuteRead("GET", key, []string{}, CONSISTENCY_ONE, timeout, true)
	if err != nil {
		t.Errorf("Unexpected error executing read: %v", err)
	}

	if val == nil || !expectedVal.Equal(val) {
		t.Errorf("expected and actual value are not equal. Expected: %v, Actual %v", expectedVal, val)
	}

	// wait for reconciliation to finish
	start := time.Now()
	for len(mStore.reconcileCalls) < 2 {
		time.Sleep(time.Duration(1 * time.Millisecond))
		if (time.Now().After(start.Add(timeout * time.Millisecond * 2))){
			break
		}
	}

	// check that local nodes were queried properly
	for _, rnode := range nodeMap[tCluster.GetDatacenterId()] {
		node := rnode.(*mockNode)
		nodefmt := func(s string) string { return fmt.Sprintf("Node %v: %v", node.Name(), s) }
		testing_helpers.AssertEqual(t, nodefmt("read size"), 1, len(node.reads))
		read := node.reads[0]
		testing_helpers.AssertEqual(t, nodefmt("cmd"), "GET", read.cmd)
		testing_helpers.AssertEqual(t, nodefmt("key"), key, read.key)
		testing_helpers.AssertEqual(t, nodefmt("num args"), 0, len(read.args))

	}

	// check that remote nodes were not queried
	for dcid, nodes := range nodeMap {
		// skip local cluster
		if dcid == tCluster.GetDatacenterId() {
			continue
		}
		for _, rnode := range nodes {
			node := rnode.(*mockNode)
			nodefmt := func(s string) string { return fmt.Sprintf("Node %v: %v", node.Name(), s) }
			testing_helpers.AssertEqual(t, nodefmt("read size"), 0, len(node.reads))
		}
	}

	// check that no writes (reconciliations) were issued against the nodes
	for _, nodes := range nodeMap {
		for _, rnode := range nodes {
			node := rnode.(*mockNode)
			nodefmt := func(s string) string { return fmt.Sprintf("Node %v: %v", node.Name(), s) }
			testing_helpers.AssertEqual(t, nodefmt("write size"), 0, len(node.writes))
		}
	}

	// check that only one value was received for reconciliation
	testing_helpers.AssertEqual(t, "reconcile calls", 2, len(mStore.reconcileCalls))
	for _, call := range mStore.reconcileCalls {
		testing_helpers.AssertEqual(t, "reconciled values", 1, len(call.values))
	}
}

// tests consistency ONE where no nodes can be reached
func TestReadFailureCaseCLONE(t *testing.T) {

}

// tests consistency QUORUM where consistency cannot be satisfied
func TestReadSuccessCaseCLQUORUM(t *testing.T) {

}

// tests consistency QUORUM where consistency is satisfied
// but not all nodes are reached
func TestReadPartialSuccessCaseCLQUORUM(t *testing.T) {
	// TODO: test with single and multi dc configs

}

// tests consistency QUORUM where consistency cannot be satisfied
func TestReadFailureCaseCLQUORUM(t *testing.T) {

}

// tests consistency QUORUM_LOCAL where all nodes respond
func TestReadSuccessCaseCLQUORUM_LOCAL(t *testing.T) {

}

// tests consistency QUORUM_LOCAL where consistency is satisfied
// but not all nodes are reached
func TestReadPartialSuccessCaseCLQUORUM_LOCAL(t *testing.T) {

}

// tests consistency QUORUM_LOCAL where consistency cannot be satisfied
func TestReadFailureCaseCLQUORUM_LOCAL(t *testing.T) {

}

// tests consistency ALL where consistency is satisfied
// but not all nodes are reached
func TestReadPartialSuccessCaseCLALL(t *testing.T) {

}

// tests consistency ALL where no nodes can be reached
func TestReadFailureCaseCLALL(t *testing.T) {

}

// tests consistency ALL_LOCAL where all nodes respond
func TestReadSuccessCaseCLALL_LOCAL(t *testing.T) {

}

// tests consistency ALL_LOCAL where consistency is satisfied
// but not all nodes are reached
func TestReadPartialSuccessCaseCLALL_LOCAL(t *testing.T) {

}

// tests consistency ALL_LOCAL where no nodes can be reached
func TestReadFailureCaseCLALL_LOCAL(t *testing.T) {

}

// tests consistency CONSENSUS where all nodes respond
func TestReadSuccessCaseCLCONSENSUS(t *testing.T) {

}

// tests consistency CONSENSUS where consistency is satisfied
// but not all nodes are reached
func TestReadPartialSuccessCaseCLCONSENSUS(t *testing.T) {

}

// tests consistency CONSENSUS where no nodes can be reached
func TestReadFailureCaseCLCONSENSUS(t *testing.T) {

}

// tests consistency CONSENSUS_LOCAL where all nodes respond
func TestReadSuccessCaseCLCONSENSUS_LOCAL(t *testing.T) {

}

// tests consistency CONSENSUS_LOCAL where consistency is satisfied
// but not all nodes are reached
func TestReadPartialSuccessCaseCLCONSENSUS_LOCAL(t *testing.T) {

}

// tests consistency CONSENSUS_LOCAL where no nodes can be reached
func TestReadFailureCaseCLCONSENSUS_LOCAL(t *testing.T) {

}


