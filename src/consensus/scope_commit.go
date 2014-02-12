package consensus

import (
	"time"
)

import (
	"node"
)

func makeExecuteTimeout() time.Time {
	return time.Now().Add(time.Duration(EXECUTE_TIMEOUT) * time.Millisecond)
}

// sets the given instance as committed
// in the case of handling messages from leaders to replicas
// the message instance should be passed in. It will either
// update the existing instance in place, or add the message
// instance to the scope's instance
// returns a bool indicating that the instance was actually
// accepted (and not skipped), and an error, if applicable
func (s *Scope) commitInstanceUnsafe(inst *Instance, incrementBallot bool) error {
	var instance *Instance
	if existing, exists := s.instances[inst.InstanceID]; exists {
		if existing.Status > INSTANCE_COMMITTED {
			logger.Debug("Commit: Can't commit instance %v with status %v", inst.InstanceID, inst.Status)
			return NewInvalidStatusUpdateError(existing, INSTANCE_COMMITTED)
		} else {
			logger.Debug("Commit: committing existing instance %v", inst.InstanceID)
			// this replica may have missed an accept message
			// so copy the seq & deps onto the existing instance
			existing.Dependencies = inst.Dependencies
			existing.Sequence = inst.Sequence
			existing.MaxBallot = inst.MaxBallot
			existing.Noop = inst.Noop
		}
		instance = existing
	} else {
		logger.Debug("Commit: committing new instance %v", inst.InstanceID)
		instance = inst
	}

	instance.Status = INSTANCE_COMMITTED
	if incrementBallot {
		instance.MaxBallot++
	}
	s.inProgress.Remove(instance)
	s.instances.Add(instance)
	s.committed.Add(instance)

	if instance.Sequence > s.maxSeq {
		s.maxSeq = instance.Sequence
	}

	instance.executeTimeout = makeExecuteTimeout()

	if err := s.Persist(); err != nil {
		return err
	}

	// wake up any goroutines waiting on this instance
	instance.broadcastCommitEvent()
	s.statCommitCount++

	logger.Debug("Commit: success for Instance: %v", instance.InstanceID)
	return nil
}

// sets the given instance as committed
// in the case of handling messages from leaders to replicas
// the message instance should be passed in. It will either
// update the existing instance in place, or add the message
// instance to the scope's instance
// returns a bool indicating that the instance was actually
// accepted (and not skipped), and an error, if applicable
func (s *Scope) commitInstance(instance *Instance, incrementBallot bool) error {
	s.lock.Lock()
	defer s.lock.Unlock()

	return s.commitInstanceUnsafe(instance, incrementBallot)
}

// Mark the instance as committed locally, persist state to disk, then send
// commit messages to the other replicas
// We're not concerned with whether the replicas actually respond because, since
// a quorum of nodes have agreed on the dependency graph for this instance, they
// won't be able to do anything else without finding out about it. This method
// will only return an error if persisting the committed state fails
func (s *Scope) sendCommit(instance *Instance, replicas []node.Node) error {
	instanceCopy, err := s.copyInstanceAtomic(instance)
	if err != nil {
		return err
	}
	msg := &CommitRequest{Scope: s.name, Instance: instanceCopy}
	sendCommitMessage := func(n node.Node) { n.SendMessage(msg) }
	for _, replica := range replicas {
		go sendCommitMessage(replica)
	}
	return nil
}

var scopeCommitPhase = func(s *Scope, instance *Instance) error {
	s.debugInstanceLog(instance, "Commit phase started")
	replicas := s.manager.getScopeReplicas(s)

	if err := s.commitInstance(instance, true); err != nil {
		if _, ok := err.(InvalidStatusUpdateError); !ok {
			return err
		}
	}
	if err := s.sendCommit(instance, replicas); err != nil {
		return err
	}
	s.debugInstanceLog(instance, "Commit phase completed")
	return nil
}

func (s *Scope) commitPhase(instance *Instance) error {
	return scopeCommitPhase(s, instance)
}

// handles an commit message from the command leader for an instance
// this executes the replica commit phase for the given instance
func (s *Scope) HandleCommit(request *CommitRequest) (*CommitResponse, error) {
	s.lock.Lock()
	defer s.lock.Unlock()
	logger.Debug("Commit message received, ballot: %v", request.Instance.MaxBallot)

	if err := s.commitInstanceUnsafe(request.Instance, false); err != nil {
		if _, ok := err.(InvalidStatusUpdateError); !ok {
			return nil, err
		}
	} else {
		// asynchronously apply mutation
		go s.executeInstance(s.instances[request.Instance.InstanceID])
	}

	logger.Debug("Commit message replied")
	return &CommitResponse{}, nil
}

