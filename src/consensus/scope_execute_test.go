/**
tests the execution of committed commands
*/
package consensus

import (
	"runtime"
)

import (
	"launchpad.net/gocheck"
)

type ExecuteDependencyChanTest struct {
	baseScopeTest
	expectedOrder []InstanceID
}

var _ = gocheck.Suite(&ExecuteDependencyChanTest{})

// makes a set of interdependent instances, and sets
// their expected ordering
func (s *ExecuteDependencyChanTest) SetUpTest(c *gocheck.C) {
	s.baseScopeTest.SetUpTest(c)
	s.expectedOrder  = make([]InstanceID, 0)
	lastVal := 0

	// sets up a new instance, and appends it to the expected order needs
	// to be called in the same order as the expected dependency ordering
	addInst := func() *Instance {
		inst := s.scope.makeInstance(s.getInstructions(lastVal))
		lastVal++
		s.scope.preAcceptInstanceUnsafe(inst)
		s.expectedOrder = append(s.expectedOrder, inst.InstanceID)
		return inst
	}

	// adds additional dependecies to the given instance
	addDeps := func(inst *Instance, deps ...*Instance) {
		for _, dep := range deps {
			inst.Dependencies = append(inst.Dependencies, dep.InstanceID)
		}
	}

	// set 1, interdependent
	i0 := addInst()
	i1 := addInst()
	i2 := addInst()
	addDeps(i0, i1, i2)
	addDeps(i1, i2)

	// set 1, conflicting seq
	i3 := addInst()
	i4 := addInst()
	i5 := addInst()
	addDeps(i3, i4, i5)
	addDeps(i4, i5)
	i3.Sequence = i5.Sequence
	i4.Sequence = i5.Sequence
}

// commits all instances
func (s *ExecuteDependencyChanTest) commitInstances() {
	for _, iid := range s.expectedOrder {
		s.scope.commitInstance(s.scope.instances[iid])
	}
}

func (s *ExecuteDependencyChanTest) TestDependencyOrdering(c *gocheck.C) {
	s.commitInstances()

	lastInstance := s.scope.instances[s.expectedOrder[len(s.expectedOrder) - 1]]
	actual := s.scope.getExecutionOrder(lastInstance)
	c.Assert(len(s.expectedOrder), gocheck.Equals, len(actual))
	for i := range s.expectedOrder {
		c.Check(s.expectedOrder[i], gocheck.Equals, actual[i], gocheck.Commentf("iid %v", i))
	}
}

// tests that instances up to and including the given
// instance are executed
func (s *ExecuteDependencyChanTest) TestSuccess(c *gocheck.C) {

}

// tests that an error is returned if an uncommitted instance id is provided
func (s *ExecuteDependencyChanTest) TestUncommittedFailure(c *gocheck.C) {

}

// tests that execute dependency chain only executes up
// to the target instance
func (s *ExecuteDependencyChanTest) TestStopOnInstance(c *gocheck.C) {

}

// tests that instances are not executed twice
func (s *ExecuteDependencyChanTest) TestSkipExecuted(c *gocheck.C) {

}

// tests that unexecuted dependencies, where the command leader
// is the local node, waits for the owning goroutine to execute
// before continuing
func (s *ExecuteDependencyChanTest) TestUnexecutedLocal(c *gocheck.C) {

}

// tests that unexecuted dependencies, where the command leader
// is the local node, waits until the execute timeout before executing
func (s *ExecuteDependencyChanTest) TestUnexecutedLocalTimeout(c *gocheck.C) {

}

// tests that unexecuted dependencies, where the command leader
// is not the local node, executes dependencies as soon as it finds them
func (s *ExecuteDependencyChanTest) TestUnexecutedRemote(c *gocheck.C) {

}


type ApplyInstanceTest struct {
	baseScopeTest
}

var _ = gocheck.Suite(&ApplyInstanceTest{})

func (s *ApplyInstanceTest) TestSuccess(c *gocheck.C) {
	instance := s.scope.makeInstance(s.getInstructions(5))
	committed, err := s.scope.commitInstance(instance)
	c.Assert(committed, gocheck.Equals, true)
	c.Assert(err, gocheck.IsNil)
	val, err := s.scope.applyInstance(instance)
	c.Assert(err, gocheck.IsNil)
	c.Assert(val, gocheck.FitsTypeOf, &intVal{})
	c.Assert(val.(*intVal).value, gocheck.Equals, 5)
	c.Check(instance.Status, gocheck.Equals, INSTANCE_EXECUTED)
	c.Check(len(s.cluster.instructions), gocheck.Equals, 1)
	c.Check(s.cluster.values["a"].value, gocheck.Equals, 5)
}

// tests the executing an instance against the store
// broadcasts to an existing notify instance, and
// removes it from the executeNotify map
func (s *ApplyInstanceTest) TestNotifyHandling(c *gocheck.C) {
	instance := s.scope.makeInstance(s.getInstructions(5))
	s.scope.commitInstance(instance)
	s.scope.executeNotify[instance.InstanceID] = makeConditional()

	broadcast := false
	broadcastListener := func() {
		cond := s.scope.executeNotify[instance.InstanceID]
		c.Check(cond, gocheck.NotNil)
		cond.Wait()
		broadcast = true
	}
	go broadcastListener()
	runtime.Gosched() // yield goroutine

	c.Check(broadcast, gocheck.Equals, false)
	c.Check(s.scope.executeNotify[instance.InstanceID], gocheck.NotNil)

	_, err := s.scope.applyInstance(instance)
	runtime.Gosched() // yield goroutine
	c.Assert(err, gocheck.IsNil)

	c.Check(broadcast, gocheck.Equals, true)
	c.Check(s.scope.executeNotify[instance.InstanceID], gocheck.IsNil)
}

// tests that apply instance marks the instance as
// executed, and moves it into the executed container
func (s *ApplyInstanceTest) TestBookKeeping(c *gocheck.C) {
	instance := s.scope.makeInstance(s.getInstructions(5))
	iid := instance.InstanceID
	s.scope.commitInstance(instance)

	// sanity check
	c.Check(s.scope.committed, instMapContainsKey, iid)
	c.Check(s.scope.executed, gocheck.Not(instIdSliceContains), iid)
	c.Check(instance.Status, gocheck.Equals, INSTANCE_COMMITTED)

	_, err := s.scope.applyInstance(instance)
	c.Assert(err, gocheck.IsNil)

	// check expected state
	c.Check(s.scope.committed, gocheck.Not(instMapContainsKey), iid)
	c.Check(s.scope.executed, instIdSliceContains, iid)
	c.Check(instance.Status, gocheck.Equals, INSTANCE_EXECUTED)
}

// tests that apply instance fails if the instance is not committed
func (s *ApplyInstanceTest) TestUncommittedFailure(c *gocheck.C) {
	instance := s.scope.makeInstance(s.getInstructions(5))
	iid := instance.InstanceID
	s.scope.acceptInstance(instance)

	// sanity check
	c.Check(s.scope.inProgress, instMapContainsKey, iid)
	c.Check(s.scope.executed, gocheck.Not(instIdSliceContains), iid)
	c.Check(instance.Status, gocheck.Equals, INSTANCE_ACCEPTED)

	_, err := s.scope.applyInstance(instance)
	c.Assert(err, gocheck.NotNil)
}
