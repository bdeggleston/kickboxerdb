package cluster

import (
	"fmt"
	"time"
)

import (
	"message"
	"node"
	"partitioner"
	"store"
	"topology"
)

type NodeError struct {
	reason string
}

func NewNodeError(reason string) *NodeError {
	return &NodeError{reason:reason}
}

func (e *NodeError) Error() string {
	return e.reason
}

// the baseNode defines all of the properties
// and methods in common among node types
type baseNode struct {
	name string
	addr string
	token partitioner.Token
	id node.NodeId
	dcId topology.DatacenterID
	status topology.NodeStatus
}

func (n *baseNode) Name() string { return n.name }

func (n *baseNode) GetAddr() string { return n.addr }

func (n *baseNode) GetToken() partitioner.Token { return n.token }

func (n *baseNode) GetId() node.NodeId { return n.id }

func (n *baseNode) GetDatacenterId() topology.DatacenterID { return n.dcId }

func (n *baseNode) GetStatus() topology.NodeStatus { return n.status }

// LocalNode provides access to the local store
type LocalNode struct {
	baseNode
	store store.Store
	isStarted bool
}

var _ = topology.Node(&LocalNode{})

func NewLocalNode(id node.NodeId, dcId topology.DatacenterID, token partitioner.Token, name string, store store.Store) (*LocalNode) {
	//
	n := &LocalNode{}
	n.id = id
	n.dcId = dcId
	n.token = token
	n.name = name
	n.store = store
	n.status = topology.NODE_UP
	return n
}

func (n *LocalNode) Start() error {
	// connect the store
	n.isStarted = true
	return nil
}

func (n *LocalNode) Stop() error {
	n.isStarted = false
	return nil
}

func (n *LocalNode) IsStarted() bool {
	return n.isStarted
}

func (n *LocalNode) SendMessage(m message.Message) (message.Message, error) {
	// TODO: find a more sane solution
	panic("can't send messages to local nodes")
}

// executes a write instruction against the node's store
func (n *LocalNode) ExecuteQuery(cmd string, key string, args []string, timestamp time.Time) (store.Value, error) {
	_ = cmd
	_ = key
	_ = args
	_ = timestamp

	return nil, nil
}

// RemoteNode communicates with other nodes in the cluster
type RemoteNode struct {
	baseNode

	pool ConnectionPool
	cluster *Cluster

	isStarted bool
}

var _ = topology.Node(&RemoteNode{})

var newRemoteNode = func(addr string, cluster *Cluster) (*RemoteNode) {
	n := &RemoteNode{}
	n.addr = addr
	n.pool = *NewConnectionPool(n.addr, 10, 10000)
	n.cluster = cluster
	return n
}

// creates a new remote node from only an address
func NewRemoteNode(addr string, cluster *Cluster) (*RemoteNode) {
	return newRemoteNode(addr, cluster)
}

// creates a new remote node from info provided from the node
func NewRemoteNodeInfo(id node.NodeId, dcId topology.DatacenterID, token partitioner.Token, name string, addr string, cluster *Cluster) (n *RemoteNode) {
	n = NewRemoteNode(addr, cluster)
	n.id = id
	n.dcId = dcId
	n.token = token
	n.name = name
	return n
}

func (n *RemoteNode) Start() error {
	// connect to the node and get it's info
	conn, err := n.getConnection()
	if err != nil { return err }
	n.pool.Put(conn)
	n.status = topology.NODE_UP
	n.isStarted = true
	return nil
}

func (n *RemoteNode) Stop() error {
	// connect to the node and get it's info
	n.isStarted = false
	return nil
}

func (n *RemoteNode) IsStarted() bool {
	return n.isStarted
}

// returns a connection with a completed handshake
func (n *RemoteNode) getConnection() (*Connection, error) {

	conn, err := n.pool.Get()
	if err != nil { return nil, err }

	if !conn.HandshakeCompleted() {
		msg := &ConnectionRequest{PeerData{
			NodeId:n.cluster.GetNodeId(),
			DCId:n.cluster.GetDatacenterId(),
			Addr:n.cluster.GetPeerAddr(),
			Name:n.cluster.GetName(),
			Token:n.cluster.GetToken(),
		}}
		if err := message.WriteMessage(conn, msg); err != nil {
			n.status = topology.NODE_DOWN
			return nil, err
		}
		response, err := message.ReadMessage(conn)
		if err != nil {
			n.status = topology.NODE_DOWN
			return nil, err
		}
		if _, ok := response.(*ConnectionAcceptedResponse); !ok {
			n.status = topology.NODE_DOWN
			return nil, fmt.Errorf("Unexpected response type, expected *ConnectionAcceptedResponse, got %T", response)
		} else if n.status == topology.NODE_INITIALIZING {
			// copy the response info if we're still initializing
			accept := response.(*ConnectionAcceptedResponse)
			n.id = accept.NodeId
			n.dcId = accept.DCId
			n.name = accept.Name
			n.token = accept.Token
		}

		conn.SetHandshakeCompleted()
	}
	return conn, nil
}

func (n *RemoteNode) SendMessage(m message.Message) (message.Message, error) {

	// get connection
	conn, err := n.getConnection()
	if  err != nil {
		n.status = topology.NODE_DOWN
		return nil, err
	}


	// send the message
	if err := message.WriteMessage(conn, m); err != nil {
		conn.Close()
		n.status = topology.NODE_DOWN
		return nil, err
	}

	// receive the message
	response, err := message.ReadMessage(conn)
	if err != nil {
		conn.Close()
		n.status = topology.NODE_DOWN
		return nil, err
	}

	n.status = topology.NODE_UP
	n.pool.Put(conn)
	return response, nil
}

// executes a write instruction against the node's store
func (n *RemoteNode) ExecuteQuery(cmd string, key string, args []string, timestamp time.Time) (store.Value, error) {
	_ = cmd
	_ = key
	_ = args
	_ = timestamp
	return nil, nil
}

