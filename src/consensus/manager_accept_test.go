package consensus

import (
	"fmt"
	"time"
)

import (
	"launchpad.net/gocheck"
)

import (
	"message"
	"node"
)

type AcceptInstanceTest struct {
	baseManagerTest
}

var _ = gocheck.Suite(&AcceptInstanceTest{})

// tests that an instance is marked as accepted,
// added to the inProgress set, has it's seq & deps
// updated and persisted if it's only preaccepted
func (s *AcceptInstanceTest) TestSuccessCase(c *gocheck.C) {
	replicaInstance := makeInstance(node.NewNodeId(), makeDependencies(4))
	originalBallot := replicaInstance.MaxBallot

	s.manager.instances.Add(replicaInstance)

	// sanity checks
	c.Assert(4, gocheck.Equals, len(replicaInstance.Dependencies))

	leaderInstance, _ := replicaInstance.Copy()
	leaderInstance.Dependencies = append(leaderInstance.Dependencies, NewInstanceID())
	err := s.manager.acceptInstance(leaderInstance, false)
	c.Assert(err, gocheck.IsNil)

	c.Check(INSTANCE_ACCEPTED, gocheck.Equals, replicaInstance.Status)
	c.Check(5, gocheck.Equals, len(replicaInstance.Dependencies))
	c.Check(replicaInstance.MaxBallot, gocheck.Equals, originalBallot)
}

func (s *AcceptInstanceTest) TestBallotIncrement(c *gocheck.C) {
	instance := makeInstance(node.NewNodeId(), makeDependencies(4))
	originalBallot := instance.MaxBallot

	err := s.manager.acceptInstance(instance, true)
	c.Assert(err, gocheck.IsNil)

	c.Check(instance.MaxBallot, gocheck.Equals, originalBallot + 1)
}

// tests that an instance is marked as accepted,
// added to the instances and inProgress set, and
// persisted if the instance hasn't been seen before
func (s *AcceptInstanceTest) TestNewInstanceSuccess(c *gocheck.C) {
	leaderInstance := makeInstance(node.NewNodeId(), makeDependencies(4))

	// sanity checks
	c.Assert(s.manager.instances.Contains(leaderInstance), gocheck.Equals, false)

	err := s.manager.acceptInstance(leaderInstance, false)
	c.Assert(err, gocheck.IsNil)

	c.Check(s.manager.instances.Contains(leaderInstance), gocheck.Equals, true)

	replicaInstance := s.manager.instances.Get(leaderInstance.InstanceID)
	c.Check(replicaInstance.Status, gocheck.Equals, INSTANCE_ACCEPTED)
	c.Check(leaderInstance.Status, gocheck.Equals, INSTANCE_ACCEPTED)
	c.Check(len(replicaInstance.Dependencies), gocheck.Equals, 4)
}

// tests that an instance is not marked as accepted,
// or added to the inProgress set if it already has
// a higher status
func (s *AcceptInstanceTest) TestHigherStatusFailure(c *gocheck.C) {
	replicaInstance := makeInstance(node.NewNodeId(), makeDependencies(4))
	replicaInstance.Status = INSTANCE_COMMITTED

	s.manager.instances.Add(replicaInstance)

	leaderInstance, _ := replicaInstance.Copy()
	leaderInstance.Status = INSTANCE_ACCEPTED

	err := s.manager.acceptInstance(leaderInstance, false)
	c.Assert(err, gocheck.FitsTypeOf, InvalidStatusUpdateError{})
}

// if an instance is being accepted twice
// which is possible if there's an explicit
// prepare, it should copy some attributes,
// (noop), and not overwrite any existing
// instances references in the manager's containers
func (s *AcceptInstanceTest) TestRepeatAccept(c *gocheck.C ) {
	var err error
	instance := s.manager.makeInstance(getBasicInstruction())
	repeat, _ := instance.Copy()

	err = s.manager.acceptInstance(instance, false)
	c.Assert(err, gocheck.IsNil)

	err = s.manager.acceptInstance(repeat, false)
	c.Assert(err, gocheck.IsNil)
	c.Assert(s.manager.instances.Get(instance.InstanceID), gocheck.Equals, instance)
	c.Assert(s.manager.instances.Get(instance.InstanceID), gocheck.Not(gocheck.Equals), repeat)
}

// tests that the noop flag is recognized when
// preaccepting new instances
func (s *AcceptInstanceTest) TestNewNoopAccept(c *gocheck.C) {

}

// tests that the noop flag is recognized when
// preaccepting previously seen instances
func (s *AcceptInstanceTest) TestOldNoopAccept(c *gocheck.C) {

}

// tests that instance dependencies are marked as acknowledged on commit
func (s *AcceptInstanceTest) TestReportAcknowledged(c *gocheck.C) {
	var err error
	instance := s.manager.makeInstance(getBasicInstruction())
	toAcknowledge := NewInstanceID()
	instance.Dependencies = []InstanceID{toAcknowledge}

	// check that this instance hasn't already been somehow acknowledged
	depsNode := s.manager.depsMngr.deps.get("a")
	c.Assert(depsNode.acknowledged.Contains(instance.InstanceID), gocheck.Equals, false)

	err = s.manager.acceptInstance(instance, false)
	c.Assert(err, gocheck.IsNil)

	// check that this instance's deps has been acknowledged, but it hasn't
	c.Check(depsNode.acknowledged.Contains(toAcknowledge), gocheck.Equals, true)
	c.Check(depsNode.acknowledged.Contains(instance.InstanceID), gocheck.Equals, false)
	c.Check(depsNode.executed.Contains(instance.InstanceID), gocheck.Equals, false)
}

type AcceptLeaderTest struct {
	baseReplicaTest
	instance *Instance
	oldAcceptTimeout uint64
}

var _ = gocheck.Suite(&AcceptLeaderTest{})

func (s *AcceptLeaderTest) SetUpSuite(c *gocheck.C) {
	s.baseReplicaTest.SetUpSuite(c)
	s.oldAcceptTimeout = ACCEPT_TIMEOUT
	ACCEPT_TIMEOUT = 50
}

func (s *AcceptLeaderTest) TearDownSuite(c *gocheck.C) {
	ACCEPT_TIMEOUT = s.oldAcceptTimeout
}

func (s *AcceptLeaderTest) SetUpTest(c *gocheck.C) {
	s.baseReplicaTest.SetUpTest(c)
	s.instance = s.manager.makeInstance(getBasicInstruction())
	var err error

	err = s.manager.preAcceptInstance(s.instance, false)
	c.Assert(err, gocheck.IsNil)
	err = s.manager.acceptInstance(s.instance, false)
	c.Assert(err, gocheck.IsNil)
}

// tests all replicas returning results
func (s *AcceptLeaderTest) TestSendAcceptSuccess(c *gocheck.C) {
	// all replicas agree
	responseFunc := func(n *mockNode, m message.Message) (message.Message, error) {
		return &AcceptResponse{
			Accepted:         true,
			MaxBallot:        s.instance.MaxBallot,
		}, nil
	}

	for _, replica := range s.replicas {
		replica.messageHandler = responseFunc
	}

	err := s.manager.sendAccept(s.instance, transformMockNodeArray(s.replicas))
	c.Assert(err, gocheck.IsNil)

	// test that the nodes received the correct message
	for _, replica := range s.replicas {
		c.Assert(len(replica.sentMessages), gocheck.Equals, 1)
		msg := replica.sentMessages[0]
		c.Check(msg, gocheck.FitsTypeOf, &AcceptRequest{})
	}
}

// tests proper error is returned if
// less than a quorum respond
func (s *AcceptLeaderTest) TestQuorumFailure(c *gocheck.C) {
	// all replicas agree
	responseFunc := func(n *mockNode, m message.Message) (message.Message, error) {
		return &AcceptResponse{
			Accepted:         true,
			MaxBallot:        s.instance.MaxBallot,
		}, nil
	}
	hangResponse := func(n *mockNode, m message.Message) (message.Message, error) {
		time.Sleep(1 * time.Second)
		return nil, fmt.Errorf("nope")
	}

	for i, replica := range s.replicas {
		if i == 0 {
			replica.messageHandler = responseFunc
		} else {
			replica.messageHandler = hangResponse
		}
	}

	err := s.manager.sendAccept(s.instance, transformMockNodeArray(s.replicas))
	c.Assert(err, gocheck.NotNil)
	c.Check(err, gocheck.FitsTypeOf, TimeoutError{})
}

// check that a ballot error is returned if the remote instance
// rejects the message
func (s *AcceptLeaderTest) TestSendAcceptBallotFailure(c *gocheck.C) {
	// all replicas agree
	responseFunc := func(n *mockNode, m message.Message) (message.Message, error) {
		return &AcceptResponse{
			Accepted:         true,
			MaxBallot:        s.instance.MaxBallot,
		}, nil
	}
	rejectFunc := func(n *mockNode, m message.Message) (message.Message, error) {
		return &AcceptResponse{
			Accepted:         false,
			MaxBallot:        s.instance.MaxBallot + 1,
		}, nil
	}

	originalBallot := s.instance.MaxBallot
	for i, replica := range s.replicas {
		if i == 0 {
			replica.messageHandler = responseFunc
		} else {
			replica.messageHandler = rejectFunc
		}
	}

	err := s.manager.sendAccept(s.instance, transformMockNodeArray(s.replicas))
	c.Assert(err, gocheck.NotNil)
	c.Check(err, gocheck.FitsTypeOf, BallotError{})
	c.Check(s.instance.MaxBallot, gocheck.Equals, originalBallot + 1)
}

// tests that the accept messages sent out have the same ballot
// as the local instance
func (s *AcceptLeaderTest) TestAcceptMessageBallotIsUpToDate(c *gocheck.C) {
	var sentBallot uint32
	// all replicas agree
	responseFunc := func(n *mockNode, m message.Message) (message.Message, error) {
		request := m.(*AcceptRequest)
		sentBallot = request.Instance.MaxBallot
		return &AcceptResponse{
			Accepted:         true,
			MaxBallot:        s.instance.MaxBallot,
		}, nil
	}

	for _, replica := range s.replicas {
		replica.messageHandler = responseFunc
	}

	duplicateInstance, err := s.instance.Copy()
	c.Assert(err, gocheck.IsNil)

	expectedBallot := duplicateInstance.MaxBallot + 1
	err = s.manager.acceptPhase(duplicateInstance)
	c.Assert(err, gocheck.IsNil)
	c.Assert(sentBallot, gocheck.Equals, expectedBallot)
}

/** replica **/

type AcceptReplicaTest struct {
	baseManagerTest
	instance *Instance
}

var _ = gocheck.Suite(&AcceptReplicaTest{})

func (s *AcceptReplicaTest) SetUpTest(c *gocheck.C) {
	s.baseManagerTest.SetUpTest(c)
	s.instance = s.manager.makeInstance(getBasicInstruction())
}

// test that instances are marked as accepted when
// an accept request is received, and there are no
// problems with the request
func (s *AcceptReplicaTest) TestHandleSuccessCase(c *gocheck.C) {
	var err error

	err = s.manager.preAcceptInstance(s.instance, false)
	c.Assert(err, gocheck.IsNil)

	leaderInstance, _ := s.instance.Copy()
	leaderInstance.Dependencies = append(leaderInstance.Dependencies, NewInstanceID())
	leaderInstance.MaxBallot++

	request := &AcceptRequest{
		Instance: leaderInstance,
		MissingInstances: []*Instance{},
	}

	response, err := s.manager.HandleAccept(request)
	c.Assert(err, gocheck.IsNil)
	c.Check(response.Accepted, gocheck.Equals, true)

	// check dependencies
	expectedDeps := NewInstanceIDSet(leaderInstance.Dependencies)
	actualDeps := NewInstanceIDSet(s.instance.Dependencies)
	c.Check(len(actualDeps), gocheck.Equals, len(expectedDeps))
	c.Assert(expectedDeps.Equal(actualDeps), gocheck.Equals, true)
}

func (s *AcceptReplicaTest) TestHandleNoop(c *gocheck.C) {
	var err error

	err = s.manager.preAcceptInstance(s.instance, false)
	c.Assert(err, gocheck.IsNil)

	leaderInstance, _ := s.instance.Copy()
	leaderInstance.Dependencies = append(leaderInstance.Dependencies, NewInstanceID())
	leaderInstance.MaxBallot++
	leaderInstance.Noop = true

	request := &AcceptRequest{
		Instance: leaderInstance,
		MissingInstances: []*Instance{},
	}

	response, err := s.manager.HandleAccept(request)
	c.Assert(err, gocheck.IsNil)
	c.Check(response.Accepted, gocheck.Equals, true)

	// check noop flag
	c.Check(s.instance.Noop, gocheck.Equals, true)
}

// tests that accepts are handled properly if
// the commit if for an instance the node has
// not been previously seen by this replica
func (s *AcceptReplicaTest) TestNewInstanceSuccess(c *gocheck.C) {
	var err error
	leaderID := node.NewNodeId()
	leaderInstance := makeInstance(leaderID, []InstanceID{})
	leaderInstance.Dependencies, err = s.manager.getInstanceDeps(leaderInstance)
	c.Assert(err, gocheck.IsNil)

	request := &AcceptRequest{
		Instance: leaderInstance,
		MissingInstances: []*Instance{},
	}

	// sanity checks
	c.Assert(s.manager.instances.ContainsID(leaderInstance.InstanceID), gocheck.Equals, false)

	response, err := s.manager.HandleAccept(request)
	c.Assert(err, gocheck.IsNil)

	c.Assert(s.manager.instances.ContainsID(leaderInstance.InstanceID), gocheck.Equals, true)
	s.instance = s.manager.instances.Get(leaderInstance.InstanceID)

	c.Check(response.Accepted, gocheck.Equals, true)

	// check dependencies
	expectedDeps := NewInstanceIDSet(leaderInstance.Dependencies)
	actualDeps := NewInstanceIDSet(s.instance.Dependencies)
	c.Check(len(actualDeps), gocheck.Equals, len(expectedDeps))
	c.Assert(expectedDeps.Equal(actualDeps), gocheck.Equals, true)
}

// tests that accept messages fail if an higher
// ballot number has been seen for this message
func (s *AcceptReplicaTest) TestOldBallotFailure(c *gocheck.C) {
	c.Skip("invalid... for now")
	var err error
	err = s.manager.preAcceptInstance(s.instance, false)
	c.Assert(err, gocheck.IsNil)

	leaderInstance, _ := s.instance.Copy()

	request := &AcceptRequest{
		Instance: leaderInstance,
		MissingInstances: []*Instance{},
	}

	s.instance.MaxBallot++
	response, err := s.manager.HandleAccept(request)
	c.Assert(err, gocheck.IsNil)

	c.Check(response.Accepted, gocheck.Equals, false)
	c.Check(s.instance.MaxBallot, gocheck.Equals, response.MaxBallot)
}

// tests that handle accept adds any missing instances
// in the missing instances message
func (s *AcceptReplicaTest) TestMissingInstanceSuccess(c *gocheck.C) {
	var err error
	err = s.manager.preAcceptInstance(s.instance, false)
	c.Assert(err, gocheck.IsNil)

	leaderID := node.NewNodeId()
	missingInstance := makeInstance(leaderID, s.instance.Dependencies)
	leaderInstance, _ := s.instance.Copy()
	leaderInstance.Dependencies = append(leaderInstance.Dependencies, missingInstance.InstanceID)
	leaderInstance.MaxBallot++

	// sanity checks
	c.Check(s.manager.instances.ContainsID(missingInstance.InstanceID), gocheck.Equals, false)

	request := &AcceptRequest{
		Instance: leaderInstance,
		MissingInstances: []*Instance{missingInstance},
	}

	response, err := s.manager.HandleAccept(request)
	c.Assert(err, gocheck.IsNil)

	c.Check(response.Accepted, gocheck.Equals, true)
	c.Check(s.manager.instances.ContainsID(missingInstance.InstanceID), gocheck.Equals, true)
}

