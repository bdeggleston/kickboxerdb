package consensus

import (
	"time"
)

import (
	"node"
)

func makeAcceptCommitTimeout() time.Time {
	return time.Now().Add(time.Duration(ACCEPT_COMMIT_TIMEOUT) * time.Millisecond)
}

// sets the given instance as accepted
// in the case of handling messages from leaders to replicas
// the message instance should be passed in. It will either
// update the existing instance in place, or add the message
// instance to the scope's instance
// returns a bool indicating that the instance was actually
// accepted (and not skipped), and an error, if applicable
func (s *Scope) acceptInstanceUnsafe(inst *Instance) error {
	var instance *Instance
	if existing, exists := s.instances[inst.InstanceID]; exists {
		if existing.Status > INSTANCE_ACCEPTED {
			return NewInvalidStatusUpdateError(existing, INSTANCE_ACCEPTED)
		} else {
			existing.Dependencies = inst.Dependencies
			existing.Sequence = inst.Sequence
			existing.MaxBallot = inst.MaxBallot
			existing.Noop = inst.Noop
		}
		instance = existing
	} else {
		instance = inst
	}

	instance.Status = INSTANCE_ACCEPTED
	instance.commitTimeout = makeAcceptCommitTimeout()
	s.instances.Add(instance)
	s.inProgress.Add(instance)

	if instance.Sequence > s.maxSeq {
		s.maxSeq = instance.Sequence
	}

	if err := s.Persist(); err != nil {
		return err
	}
	return nil
}

// sets the given instance as accepted
// in the case of handling messages from leaders to replicas
// the message instance should be passed in. It will either
// update the existing instance in place, or add the message
// instance to the scope's instance
// returns a bool indicating that the instance was actually
// accepted (and not skipped), and an error, if applicable
func (s *Scope) acceptInstance(instance *Instance) error {
	s.lock.Lock()
	defer s.lock.Unlock()

	return s.acceptInstanceUnsafe(instance)
}

func (s *Scope) sendAccept(instance *Instance, replicas []node.Node) error {
	oldTimeout := PREACCEPT_TIMEOUT
	PREACCEPT_TIMEOUT = 50
	defer func() { PREACCEPT_TIMEOUT = oldTimeout }()

	// send the message
	recvChan := make(chan *AcceptResponse, len(replicas))
	msg := &AcceptRequest{Scope: s.name, Instance: instance}
	sendMsg := func(n node.Node) {
		if response, err := n.SendMessage(msg); err != nil {
			logger.Warning("Error receiving PreAcceptResponse: %v", err)
		} else {
			if accept, ok := response.(*AcceptResponse); ok {
				recvChan <- accept
			} else {
				logger.Warning("Unexpected Accept response type: %T", response)
			}
		}
	}
	for _, replica := range replicas {
		go sendMsg(replica)
	}

	// receive the replies
	numReceived := 1 // this node counts as a response
	quorumSize := ((len(replicas) + 1) / 2) + 1
	timeoutEvent := getTimeoutEvent(time.Duration(ACCEPT_TIMEOUT) * time.Millisecond)
	var response *AcceptResponse
	responses := make([]*AcceptResponse, 0, len(replicas))
	for numReceived < quorumSize {
		select {
		case response = <-recvChan:
			responses = append(responses, response)
			numReceived++
		case <-timeoutEvent:
			return NewTimeoutError("Timeout while awaiting accept responses")
		}
	}

	// check if any of the messages were rejected
	accepted := true
	for _, response := range responses {
		accepted = accepted && response.Accepted
	}

	// handle rejected pre-accept messages
	if !accepted {
		// update max ballot from responses
		bmResponses := make([]BallotMessage, len(responses))
		for i, response := range responses {
			bmResponses[i] = BallotMessage(response)
		}
		s.updateInstanceBallotFromResponses(instance, bmResponses)
		return NewBallotError("Ballot number rejected")
	}
	return nil
}

// assigned to var for testing
var scopeAcceptPhase = func(s *Scope, instance *Instance) error {
	replicas := s.manager.getScopeReplicas(s)

	if err := s.acceptInstance(instance); err != nil {
		if _, ok := err.(InvalidStatusUpdateError); !ok {
			return err
		}
	}
	if err := s.sendAccept(instance, replicas); err != nil {
		return err
	}
	return nil
}

func (s *Scope) acceptPhase(instance *Instance) error {
	return scopeAcceptPhase(s, instance)
}

// handles an accept message from the command leader for an instance
// this executes the replica accept phase for the given instance
func (s *Scope) HandleAccept(request *AcceptRequest) (*AcceptResponse, error) {
	s.lock.Lock()
	defer s.lock.Unlock()

	if instance, exists := s.instances[request.Instance.InstanceID]; exists {
		if instance.MaxBallot >= request.Instance.MaxBallot {
			return &AcceptResponse{Accepted: false, MaxBallot: instance.MaxBallot}, nil
		}
	}

	if err := s.acceptInstanceUnsafe(request.Instance); err != nil {
		if _, ok := err.(InvalidStatusUpdateError); !ok {
			return nil, err
		}
	}

	if len(request.MissingInstances) > 0 {
		s.addMissingInstancesUnsafe(request.MissingInstances...)
	}

	if err := s.Persist(); err != nil {
		return nil, err
	}
	return &AcceptResponse{Accepted: true}, nil
}

