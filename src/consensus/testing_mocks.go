package consensus

import (
	"bufio"
	"bytes"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
)

import (
	"github.com/cactus/go-statsd-client/statsd"
)

import (
	"message"
	"node"
	"store"
)

type intVal struct {
	value int
	time time.Time
}

func newIntVal(val int, ts time.Time) *intVal {
	return &intVal{value:val, time:ts}
}

func (v *intVal) GetTimestamp() time.Time { return v.time }
func (v *intVal) GetValueType() store.ValueType { return store.ValueType("INT") }

func (v *intVal) Equal(value store.Value) bool {
	if val, ok := value.(*intVal); ok {
		return val.value == v.value
	}
	return false
}

func (v *intVal) Serialize(_ *bufio.Writer) error { return nil }
func (v *intVal) Deserialize(_ *bufio.Reader) error { return nil }

type mockCluster struct {
	id node.NodeId
	nodes []node.Node
	lock sync.Mutex
	instructions []*store.Instruction
	values map[string]*intVal
}

func newMockCluster() *mockCluster {
	return &mockCluster{
		id: node.NewNodeId(),
		nodes: make([]node.Node, 0, 10),
		instructions: make([]*store.Instruction, 0),
		values: make(map[string]*intVal),
	}
}

func (c *mockCluster) addNodes(n ...node.Node) {
	c.nodes = append(c.nodes, n...)
}

func (c *mockCluster) GetID() node.NodeId    { return c.id }
func (c *mockCluster) GetStore() store.Store { return nil }
func (c *mockCluster) GetNodesForKey(key string) []node.Node {
	return c.nodes
}

func (c *mockCluster) ExecuteQuery(cmd string, key string, args []string, timestamp time.Time) (store.Value, error) {
	panic("mockCluster doesn't implement Execute Query")
}

// executes a query against the local store
func (c *mockCluster) ApplyQuery(cmd string, key string, args []string, timestamp time.Time) (store.Value, error) {
	c.lock.Lock()
	defer c.lock.Unlock()
	intVal, err := strconv.Atoi(args[0])
	if err != nil { return nil, err }
	val := newIntVal(intVal, timestamp)
	c.values[key] = val
	c.instructions = append(c.instructions, store.NewInstruction(cmd, key, args, timestamp))
	return val, nil
}

func mockNodeDefaultMessageHandler(mn *mockNode, msg message.Message) (message.Message, error) {
	return mn.manager.HandleMessage(msg)
}

type mockNode struct {
	id node.NodeId

	// tracks the queries executed
	// against this node
	queries []*store.Instruction

	cluster        *mockCluster
	manager        *Manager
	messageHandler func(*mockNode, message.Message) (message.Message, error)
	sentMessages   []message.Message
	lock		   sync.Mutex
	partition     bool
	stats			statsd.Statter
}

func newMockNode() *mockNode {
	cluster := newMockCluster()
	return &mockNode{
		id:             cluster.GetID(),
		queries:        []*store.Instruction{},
		cluster:        cluster,
		manager:        NewManager(cluster),
		messageHandler: mockNodeDefaultMessageHandler,
		sentMessages:   make([]message.Message, 0),
		stats:			newMockStatter(),
	}
}

func (n *mockNode) GetId() node.NodeId { return n.id }

func (n *mockNode) ExecuteQuery(cmd string, key string, args []string, timestamp time.Time) (store.Value, error) {
	store.NewInstruction(cmd, key, args, timestamp)
	return nil, nil
}

func (n *mockNode) SendMessage(srcRequest message.Message) (message.Message, error) {
	var err error
	var start time.Time

	getDuration := func(s time.Time) int64 {
		e := time.Now()
		delta := e.Sub(s) / time.Millisecond
		return int64(delta) + 1
	}

	buf := &bytes.Buffer{}
	if n.partition {
		logger.Debug("Skipping sent message from partitioned node")
		return nil, fmt.Errorf("Partition")
	}
	n.lock.Lock()
	start = time.Now()
	err = message.WriteMessage(buf, srcRequest)
	n.stats.Timing(
		strings.Replace(fmt.Sprintf("serialize.%T", srcRequest), "*", "", -1),
		getDuration(start),
		1.0,
	)

	if err != nil {
		return nil, err
//		panic(err)
	}
	start = time.Now()
	dstRequest, err := message.ReadMessage(buf)
	n.stats.Timing(
		strings.Replace(fmt.Sprintf("deserialize.%T", dstRequest), "*", "", -1),
		getDuration(start),
		1.0,
	)
	if err != nil {
		return nil, err
//		panic(err)
	}
	n.sentMessages = append(n.sentMessages, dstRequest)
	n.lock.Unlock()
	start = time.Now()
	srcResponse, err := n.messageHandler(n, dstRequest)
	n.stats.Timing(
		strings.Replace(fmt.Sprintf("process.%T", srcResponse), "*", "", -1),
		getDuration(start),
		1.0,
	)
	if err != nil {
		n.stats.Inc(
			strings.Replace(fmt.Sprintf("error.%T", srcRequest), "*", "", -1),
			1,
			1.0,
		)
		return nil, err
//		panic(err)
	}
	n.lock.Lock()
	buf.Reset()
	start = time.Now()
	err = message.WriteMessage(buf, srcResponse)
	n.stats.Timing(
		strings.Replace(fmt.Sprintf("serialize.%T", srcResponse), "*", "", -1),
		getDuration(start),
		1.0,
	)
	if err != nil {
		return nil, err
//		panic(err)
	}
	logger.Debug("Response size: %v\n", len(buf.Bytes()))
	start = time.Now()
	dstResponse, err := message.ReadMessage(buf)
	n.stats.Timing(
		strings.Replace(fmt.Sprintf("deserialize.%T", dstResponse), "*", "", -1),
		getDuration(start),
		1.0,
	)
	n.lock.Unlock()
	if err != nil {
		return nil, err
//		panic(err)
	}
	return dstResponse, nil
}

func transformMockNodeArray(src []*mockNode) []node.Node {
	dst := make([]node.Node, len(src))
	for i := range src {
		dst[i] = node.Node(src[i])
	}
	return dst
}

// implements the statter interface
// used for testing things were called internally
// guages and timers only keep the most recent value
type mockStatter struct {
	mutex sync.RWMutex
	counters map[string]int64
	timers map[string]int64
	guages map[string]int64
}

func newMockStatter() *mockStatter {
	return &mockStatter{
		counters: make(map[string]int64),
		timers: make(map[string]int64),
		guages: make(map[string]int64),
	}
}

func (s *mockStatter) Inc(stat string, value int64, rate float32) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.counters[stat] += value
	return nil
}

func (s *mockStatter) Dec(stat string, value int64, rate float32) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.counters[stat] -= value
	return nil
}

func (s *mockStatter) Gauge(stat string, value int64, rate float32) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.guages[stat] = value
	return nil
}

func (s *mockStatter) GaugeDelta(stat string, value int64, rate float32) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.guages[stat] += value
	return nil
}

func (s *mockStatter) Timing(stat string, delta int64, rate float32) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.timers[stat] = delta
	return nil
}

func (s *mockStatter) SetPrefix(prefix string) {
}

func (s *mockStatter) Close() error {
	return nil
}
