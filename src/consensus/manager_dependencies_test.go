package consensus

import (
	"time"
)

import (
	"launchpad.net/gocheck"
)

import (
	"store"
)

type baseDependencyTest struct {
	baseManagerTest
}

func (s *baseDependencyTest) newInstruction(key string) store.Instruction {
	return store.NewInstruction("SET", key, []string{}, time.Now())
}

type DependencyMapTest struct {
	baseDependencyTest
}

var _ = gocheck.Suite(&DependencyMapTest{})

// tests that a new dependencies object is created for new root nodes
func (s *DependencyMapTest) TestNewRootDependencyMap(c *gocheck.C) {
	instance := s.manager.makeInstance(s.newInstruction("a"))

	c.Assert(s.manager.depsMngr.deps.deps["a"], gocheck.IsNil)

	deps, err := s.manager.depsMngr.GetAndSetDeps(instance)
	c.Assert(err, gocheck.IsNil)
	c.Assert(deps, gocheck.NotNil)
	c.Assert(deps, gocheck.DeepEquals, []InstanceID{})

	c.Assert(s.manager.depsMngr.deps.deps["a"], gocheck.NotNil)
}

// tests that an existing dependencies object is used for a key if it exists
func (s *DependencyMapTest) TestExistingRootDependencyMap(c *gocheck.C) {
	instance := s.manager.makeInstance(s.newInstruction("a"))

	depsNode := s.manager.depsMngr.deps.get("a")
	lastWrite := NewInstanceID()
	depsNode.writes = NewInstanceIDSet([]InstanceID{lastWrite})
	c.Assert(s.manager.depsMngr.deps.deps["a"], gocheck.NotNil)

	deps, err := s.manager.depsMngr.GetAndSetDeps(instance)
	c.Assert(err, gocheck.IsNil)
	c.Assert(deps, gocheck.NotNil)
	c.Assert(deps, gocheck.DeepEquals, []InstanceID{lastWrite})

	c.Assert(s.manager.depsMngr.deps.deps["a"], gocheck.NotNil)
	c.Assert(s.manager.depsMngr.deps.get("a"), gocheck.Equals, depsNode)

}

// tests overall behavior for a single key
func (s *DependencyMapTest) TestSingleKeyIntegration(c *gocheck.C) {
	var err error
	depsNode := s.manager.depsMngr.deps.get("a")
	expected := NewSizedInstanceIDSet(0)

	// instance 0
	instance0 := s.manager.makeInstance(s.newInstruction("a"))
	instance0.Dependencies, err = s.manager.depsMngr.GetAndSetDeps(instance0)
	c.Assert(err, gocheck.IsNil)

	c.Check(NewInstanceIDSet(instance0.Dependencies), gocheck.DeepEquals, expected)
	c.Check(depsNode.writes.Contains(instance0.InstanceID), gocheck.Equals, true)

	// instance 1
	expected.Add(instance0.InstanceID)
	instance1 := s.manager.makeInstance(s.newInstruction("a"))
	instance1.Dependencies, err = s.manager.depsMngr.GetAndSetDeps(instance1)
	c.Assert(err, gocheck.IsNil)

	c.Check(NewInstanceIDSet(instance1.Dependencies), gocheck.DeepEquals, expected)
	c.Check(depsNode.writes.Contains(instance1.InstanceID), gocheck.Equals, true)

	// acknowledge
	err = s.manager.depsMngr.ReportAcknowledged(instance1)
	c.Check(depsNode.acknowledged.Contains(instance0.InstanceID), gocheck.Equals, true)
	c.Assert(err, gocheck.IsNil)

	// instance 2
	expected.Add(instance1.InstanceID)
	instance2 := s.manager.makeInstance(s.newInstruction("a"))
	instance2.Dependencies, err = s.manager.depsMngr.GetAndSetDeps(instance2)
	c.Assert(err, gocheck.IsNil)

	c.Check(NewInstanceIDSet(instance2.Dependencies), gocheck.DeepEquals, expected)
	c.Check(depsNode.writes.Contains(instance2.InstanceID), gocheck.Equals, true)

	// execute
	err = s.manager.depsMngr.ReportExecuted(instance0)
	c.Check(depsNode.executed.Contains(instance0.InstanceID), gocheck.Equals, true)
	c.Assert(err, gocheck.IsNil)

	// instance 3
	expected.Add(instance2.InstanceID)
	instance3 := s.manager.makeInstance(s.newInstruction("a"))
	instance3.Dependencies, err = s.manager.depsMngr.GetAndSetDeps(instance3)
	c.Assert(err, gocheck.IsNil)

	c.Check(NewInstanceIDSet(instance3.Dependencies), gocheck.DeepEquals, expected)
	c.Check(depsNode.writes.Contains(instance3.InstanceID), gocheck.Equals, true)

	// instance 4
	expected.Add(instance3.InstanceID)
	expected.Remove(instance0.InstanceID)
	instance4 := s.manager.makeInstance(s.newInstruction("a"))
	instance4.Dependencies, err = s.manager.depsMngr.GetAndSetDeps(instance4)
	c.Assert(err, gocheck.IsNil)

	c.Check(NewInstanceIDSet(instance4.Dependencies), gocheck.DeepEquals, expected)
	c.Check(depsNode.writes.Contains(instance4.InstanceID), gocheck.Equals, true)
}

type DependenciesTest struct {
	baseDependencyTest
}

var _ = gocheck.Suite(&DependenciesTest{})

// tests that a new dependencies object is created for new leaf nodes
func (s *DependenciesTest) TestNewDependencyMap(c *gocheck.C) {
	instance := s.manager.makeInstance(s.newInstruction("a:b"))
	keys := []string{"a", "b"}
	deps := newDependencies()

	c.Assert(len(deps.subDependencies.deps), gocheck.Equals, 0)
	c.Assert(deps.subDependencies.deps["b"], gocheck.IsNil)

	deps.GetAndSetDeps(keys, instance)

	c.Assert(len(deps.subDependencies.deps), gocheck.Equals, 1)
	c.Assert(deps.subDependencies.deps["b"], gocheck.NotNil)
}

// tests that existing dependencies object is used if it exists
func (s *DependenciesTest) TestExistingDependencyMap(c *gocheck.C) {
	instance := s.manager.makeInstance(s.newInstruction("a:b"))
	keys := []string{"a", "b"}
	deps := newDependencies()


	bdeps := deps.subDependencies.get("b")
	c.Assert(len(deps.subDependencies.deps), gocheck.Equals, 1)
	c.Assert(deps.subDependencies.deps["b"], gocheck.NotNil)

	deps.GetAndSetDeps(keys, instance)

	c.Assert(len(deps.subDependencies.deps), gocheck.Equals, 1)
	c.Assert(deps.subDependencies.deps["b"], gocheck.NotNil)
	c.Assert(deps.subDependencies.get("b"), gocheck.Equals, bdeps)
}

// tests the last reads array is updated if the instance is a read
func (s *DependenciesTest) TestLastKeyReadIsUpdated(c *gocheck.C) {
	instance := s.manager.makeInstance(s.newInstruction("a"))
	instance.ReadOnly = true
	keys := []string{"a"}
	deps := newDependencies()

	c.Assert(deps.writes.Size(), gocheck.Equals, 0)
	c.Assert(deps.reads.Size(), gocheck.Equals, 0)

	deps.GetAndSetDeps(keys, instance)

	expectedReads := NewInstanceIDSet([]InstanceID{instance.InstanceID})
	c.Assert(deps.writes.Size(), gocheck.Equals, 0)
	c.Assert(deps.reads, gocheck.DeepEquals, expectedReads)
}

// tests that an instance cannot gain a dependency on itself
func (s *DependenciesTest) TestNoSelfDependence(c *gocheck.C) {
	instance := s.manager.makeInstance(s.newInstruction("a"))
	keys := []string{"a"}
	deps := newDependencies()

	deps.writes.Add(instance.InstanceID)
	c.Assert(deps.writes.Contains(instance.InstanceID), gocheck.Equals, true)

	instanceDeps := deps.GetAndSetDeps(keys, instance)

	c.Assert(instanceDeps.Contains(instance.InstanceID), gocheck.Equals, false)
}

// tests the last write is updated if the instance is a write
func (s *DependenciesTest) TestLastKeyWriteIsUpdated(c *gocheck.C) {
	instance := s.manager.makeInstance(s.newInstruction("a"))
	keys := []string{"a"}
	deps := newDependencies()

	c.Assert(deps.writes.Size(), gocheck.Equals, 0)
	c.Assert(deps.reads.Size(), gocheck.DeepEquals, 0)

	deps.GetAndSetDeps(keys, instance)

	c.Assert(deps.writes, gocheck.DeepEquals, NewInstanceIDSet([]InstanceID{instance.InstanceID}))
	c.Assert(deps.reads.Size(), gocheck.Equals, 0)
}

// tests the deps reported by a single deps node for a read
func (s *DependenciesTest) TestLocalReadDeps(c *gocheck.C) {
	depsNode := newDependencies()
	depsNode.reads.Add(NewInstanceID(), NewInstanceID())

	instance := s.manager.makeInstance(s.newInstruction("a"))
	instance.ReadOnly = true

	deps := depsNode.getLocalDeps(instance)

	c.Assert(deps, gocheck.DeepEquals, depsNode.writes)
}

// tests the deps reported by a single deps node for a write
func (s *DependenciesTest) TestLocalWriteDeps(c *gocheck.C) {
	depsNode := newDependencies()
	depsNode.reads.Add(NewInstanceID(), NewInstanceID())

	expected := depsNode.reads.Copy()
	expected.Combine(depsNode.writes)

	instance := s.manager.makeInstance(s.newInstruction("a"))

	actual := depsNode.getLocalDeps(instance)

	c.Assert(actual, gocheck.DeepEquals, expected)
}

func (s *DependenciesTest) TestReportAcknowledged(c *gocheck.C) {
	depsNode := newDependencies()
	depsNode.reads.Add(NewInstanceID(), NewInstanceID())

	instance := s.manager.makeInstance(s.newInstruction("a"))
	dependency := NewInstanceID()
	instance.Dependencies = []InstanceID{dependency}

	c.Assert(depsNode.acknowledged.Contains(instance.InstanceID), gocheck.Equals, false)
	depsNode.ReportAcknowledged([]string{"a"}, instance)
	c.Assert(depsNode.acknowledged.Contains(instance.InstanceID), gocheck.Equals, false)
	c.Assert(depsNode.acknowledged.Contains(dependency), gocheck.Equals, true)
}

func (s *DependenciesTest) TestReportExecution(c *gocheck.C) {
	depsNode := newDependencies()
	depsNode.reads.Add(NewInstanceID(), NewInstanceID())

	instance := s.manager.makeInstance(s.newInstruction("a"))

	c.Assert(depsNode.executed.Contains(instance.InstanceID), gocheck.Equals, false)
	depsNode.ReportExecuted([]string{"a"}, instance)
	c.Assert(depsNode.executed.Contains(instance.InstanceID), gocheck.Equals, true)
}

// tests that read deps are removed from the dependency manager if they've
// been both executed and acknowledged
func (s *DependenciesTest) TestRemovalOfExAckedWrites(c *gocheck.C) {
	depsNode := newDependencies()
	executed := NewInstanceID()
	acknowledged := NewInstanceID()
	exAcked := NewInstanceID()

	depsNode.writes.Add(executed, acknowledged, exAcked)
	depsNode.executed.Add(executed, exAcked)
	depsNode.acknowledged.Add(acknowledged, exAcked)

	instance := s.manager.makeInstance(s.newInstruction("a"))

	deps := depsNode.GetAndSetDeps([]string{"a"}, instance)

	// check executed
	c.Check(depsNode.executed.Contains(executed), gocheck.Equals, true)
	c.Check(depsNode.executed.Contains(exAcked), gocheck.Equals, false)
	c.Check(depsNode.writes.Contains(executed), gocheck.Equals, true)
	c.Check(depsNode.writes.Contains(exAcked), gocheck.Equals, false)

	// check acknowledged
	c.Check(depsNode.acknowledged.Contains(acknowledged), gocheck.Equals, true)
	c.Check(depsNode.acknowledged.Contains(exAcked), gocheck.Equals, false)
	c.Check(depsNode.writes.Contains(acknowledged), gocheck.Equals, true)
	c.Check(depsNode.writes.Contains(exAcked), gocheck.Equals, false)

	// check deps
	c.Check(deps.Contains(acknowledged), gocheck.Equals, true)
	c.Check(deps.Contains(executed), gocheck.Equals, true)
	c.Check(deps.Contains(exAcked), gocheck.Equals, true)
}

// tests that write deps are removed from the dependency manager if they've
// been both executed and acknowledged
func (s *DependenciesTest) TestRemovalOfExAckedReads(c *gocheck.C) {
	depsNode := newDependencies()
	executed := NewInstanceID()
	acknowledged := NewInstanceID()
	exAcked := NewInstanceID()

	depsNode.reads.Add(executed, acknowledged, exAcked)
	depsNode.executed.Add(executed, exAcked)
	depsNode.acknowledged.Add(acknowledged, exAcked)

	instance := s.manager.makeInstance(s.newInstruction("a"))

	deps := depsNode.GetAndSetDeps([]string{"a"}, instance)

	// check executed
	c.Check(depsNode.executed.Contains(executed), gocheck.Equals, true)
	c.Check(depsNode.executed.Contains(exAcked), gocheck.Equals, false)
	c.Check(depsNode.reads.Contains(executed), gocheck.Equals, true)
	c.Check(depsNode.reads.Contains(exAcked), gocheck.Equals, false)

	// check acknowledged
	c.Check(depsNode.acknowledged.Contains(acknowledged), gocheck.Equals, true)
	c.Check(depsNode.acknowledged.Contains(exAcked), gocheck.Equals, false)
	c.Check(depsNode.reads.Contains(acknowledged), gocheck.Equals, true)
	c.Check(depsNode.reads.Contains(exAcked), gocheck.Equals, false)

	// check deps
	c.Check(deps.Contains(acknowledged), gocheck.Equals, true)
	c.Check(deps.Contains(executed), gocheck.Equals, true)
	c.Check(deps.Contains(exAcked), gocheck.Equals, true)
}

func (s *DependenciesTest) TestSelfDependenciesAreNotAcknowledged(c *gocheck.C) {
	depsNode := newDependencies()
	toAcknowledge := NewInstanceID()

	instance := s.manager.makeInstance(s.newInstruction("a"))
	instance.Dependencies = []InstanceID{instance.InstanceID, toAcknowledge}

	c.Assert(len(depsNode.acknowledged), gocheck.Equals, 0)

	depsNode.ReportAcknowledged([]string{"a"}, instance)

	c.Check(depsNode.acknowledged.Contains(toAcknowledge), gocheck.Equals, true)
	c.Check(depsNode.acknowledged.Contains(instance.InstanceID), gocheck.Equals, false)
}

func (s *DependenciesTest) TestAddReadDependency(c *gocheck.C) {
	depsNode := newDependencies()
	instance := s.manager.makeInstance(s.newInstruction("a"))

	c.Check(depsNode.writes.Contains(instance.InstanceID), gocheck.Equals, false)

	depsNode.AddDependency([]string{"a"}, instance)

	c.Check(depsNode.writes.Contains(instance.InstanceID), gocheck.Equals, true)
}

func (s *DependenciesTest) TestAddWriteDependency(c *gocheck.C) {
	depsNode := newDependencies()
	instance := s.manager.makeInstance(s.newInstruction("a"))
	instance.ReadOnly = true

	c.Check(depsNode.reads.Contains(instance.InstanceID), gocheck.Equals, false)

	depsNode.AddDependency([]string{"a"}, instance)

	c.Check(depsNode.reads.Contains(instance.InstanceID), gocheck.Equals, true)
}

func (s *DependenciesTest) TestIntegration(c *gocheck.C) {
	// the key "a:b" is the key being used for tests
	addInstance := func(key string, readOnly bool) InstanceID {
		instance := s.manager.makeInstance(s.newInstruction(key))
		instance.ReadOnly = readOnly
		_, err := s.manager.depsMngr.GetAndSetDeps(instance)
		c.Assert(err, gocheck.IsNil)
		return instance.InstanceID
	}

	aWrite := addInstance("a", false)
	aRead := addInstance("a", true)
	abWrite := addInstance("a:b", false)
	abRead := addInstance("a:b", true)
	abcWrite := addInstance("a:b:c", false)
	abcRead := addInstance("a:b:c", true)
	abcdWrite := addInstance("a:b:c:d", false)
	abcdRead := addInstance("a:b:c:d", true)

	// add sibling deps, these should never be returned
	addInstance("a:b1", false)
	addInstance("a:b1", true)

	// check read deps
	readInstance := s.manager.makeInstance(s.newInstruction("a:b"))
	readInstance.ReadOnly = true
	expected := NewInstanceIDSet([]InstanceID{aWrite, abWrite, abcWrite, abcdWrite})
	deps, err := s.manager.depsMngr.GetAndSetDeps(readInstance)
	c.Assert(err, gocheck.IsNil)
	actual := NewInstanceIDSet(deps)
	c.Check(actual, gocheck.DeepEquals, expected)

	// check write deps
	writeInstance := s.manager.makeInstance(s.newInstruction("a:b"))
	expected.Add(aRead, abRead, abcRead, abcdRead, readInstance.InstanceID)
	deps, err = s.manager.depsMngr.GetAndSetDeps(writeInstance)
	c.Assert(err, gocheck.IsNil)
	actual = NewInstanceIDSet(deps)
	c.Check(actual, gocheck.DeepEquals, expected)
}
