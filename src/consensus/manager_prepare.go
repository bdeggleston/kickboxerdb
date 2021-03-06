package consensus

import (
	"fmt"
	"time"
)

import (
	"node"
)

// analyzes the responses to a prepare request, and returns an instance
// the prepare phase should use to determine how to proceed
func (m *Manager) analyzePrepareResponses(responses []*PrepareResponse) (*Instance) {
	otherInstancesSeen := false

	// find the highest response ballot
	maxBallot := uint32(0)
	for _, response := range responses {
		if response.Instance != nil {
			otherInstancesSeen = true
			if ballot := response.Instance.MaxBallot; ballot > maxBallot {
				maxBallot = ballot
			}
		}
	}

	if !otherInstancesSeen {
		return nil
	}

	// find the highest response status
	var maxStatus InstanceStatus
	var instance *Instance
	for _, response := range responses {
		if response.Instance == nil {
			continue
		}
		if response.Instance.MaxBallot < maxBallot {
			continue
		}
		if status := response.Instance.Status; status > maxStatus {
			maxStatus = status
			instance = response.Instance
		}
	}

	return instance
}

// TODO: not used/done yet, replaces analyzePrepareResponses
// transforms the local instance from the prepare responses
func (m *Manager) applyPrepareResponses(responses []*PrepareResponse, localInstance *Instance) (*Instance, error) {
	var maxBallot uint32
	var maxStatus InstanceStatus

	// indicates that nil instances
	// were encountered in the responses
	var nilInstances bool

	// indicates that non nil instances
	// were encountered in the responses
	var otherInstances bool

	// get info about the response instances
	for _, response := range responses {
		if response.Instance != nil {
			otherInstances = true
			if ballot := response.Instance.MaxBallot; ballot > maxBallot {
				maxBallot = ballot
			}
			if status := response.Instance.Status; status > maxStatus {
				maxStatus = status
			}
		} else {
			nilInstances = true
		}
	}

	referenceInstance, err := localInstance.Copy()
	if err != nil {
		return nil, err
	}

	// if no other instances have been seen...
	if !otherInstances {
		if referenceInstance.Status != INSTANCE_PREACCEPTED {
			return nil, fmt.Errorf(
				"Instance %v is unknown by other nodes, but has a status of %v",
				referenceInstance.InstanceID,
				referenceInstance.Status,
			)
		}
		referenceInstance.Noop = true
		return referenceInstance, nil
	}

	// TODO: do all of the ballots need to match to advance the preaccept process? Yes, and the prepare messages need to be accepted
	if maxStatus == INSTANCE_PREACCEPTED && !nilInstances {

	} else if maxStatus == INSTANCE_PREACCEPTED {

	}
	return nil, nil
}

var managerSendPrepare = func(m *Manager, instance *Instance) ([]*PrepareResponse, error) {
	start := time.Now()
	defer m.statsTiming("prepare.message.send.time", start)
	m.statsInc("prepare.message.send.count", 1)

	replicas := m.getInstanceReplicas(instance)

	ballot := instance.incrementBallot()
	if err := m.Persist(); err != nil {
		return nil, err
	}
	msg := &PrepareRequest{Ballot:ballot, InstanceID:instance.InstanceID}

	recvChan := make(chan *PrepareResponse, len(replicas))
	sendMsg := func(n node.Node) {
		logger.Debug("Sending prepare request to node %v with ballot %v", n.GetId(), msg.Ballot)
		if response, err := n.SendMessage(msg); err != nil {
			logger.Warning("Error receiving PrepareResponse: %v", err)
		} else {
			if preAccept, ok := response.(*PrepareResponse); ok {
				recvChan <- preAccept
			} else {
				logger.Warning("Unexpected Prepare response type: %T", response)
			}
		}
	}

	for _, replica := range replicas {
		go sendMsg(replica)
	}

	// receive responses from at least a quorum of nodes
//	quorumSize := ((len(replicas) + 1) / 2) + 1
	quorumSize := (len(replicas) / 2) + 1
	numReceived := 1  // this node counts as a response
	timeoutEvent := getTimeoutEvent(time.Duration(PREPARE_TIMEOUT) * time.Millisecond)
	var response *PrepareResponse
	responses := make([]*PrepareResponse, 0, len(replicas))
	for numReceived < quorumSize {
		select {
		case response = <-recvChan:
			m.statsInc("prepare.message.receive.success.count", 1)
			logger.Debug("Prepare response received: %v", instance.InstanceID)
			responses = append(responses, response)
			numReceived++
		case <-timeoutEvent:
			m.statsInc("prepare.message.receive.timeout.count", 1)
			logger.Info("Prepare timeout for instance: %v", instance.InstanceID)
			return nil, NewTimeoutError("Timeout while awaiting pre accept responses")
		}
	}

	// finally, receive any additional responses
	drain: for {
		select {
		case response = <-recvChan:
			responses = append(responses, response)
			numReceived++
		default:
			break drain
		}
	}

	return responses, nil
}

// performs an initial check of the responses
// if any of the responses have been rejected, the local ballot will be updated,
// as well as it's status if there are any responses with a higher status
var managerPrepareCheckResponses = func(m *Manager, instance *Instance, responses []*PrepareResponse) error {
	accepted := true
	var maxBallot uint32
	var maxStatus InstanceStatus
	for _, response := range responses {
		if response.Instance != nil {
			// TODO: update local status if the response status is higher than the local status
			if ballot := response.Instance.MaxBallot; ballot > maxBallot {
				maxBallot = ballot
			}
			if status := response.Instance.Status; status > maxStatus {
				maxStatus = status
			}
		}
		accepted = accepted && response.Accepted
	}

	// update the local ballot
	if instance.updateBallot(maxBallot) {
		if err := m.Persist(); err != nil {
			return err
		}
	}

	if !accepted {
		var maxInstance *Instance
		for _, response := range responses {
			if response.Instance != nil {
				rInstance := response.Instance
				if rInstance.MaxBallot == maxBallot && rInstance.Status == maxStatus {
					maxInstance = rInstance
				}
			}
		}

		// update local ballot and/or status
		if maxInstance != nil && maxInstance.Status > instance.getStatus() {
			switch maxInstance.Status {
			case INSTANCE_ACCEPTED:
				if err := m.acceptInstance(maxInstance, false); err != nil {
					return err
				}
			case INSTANCE_COMMITTED, INSTANCE_EXECUTED:
				if err := m.commitInstance(maxInstance, false); err != nil {
					return err
				}
			}
		}

		logger.Info("Prepare request(s) rejected")
		return NewBallotError("Prepare request(s) rejected")
	}

	return nil
}

// uses the remote instance to start a preaccept phase, an accept phase, or a commit phase
var managerPrepareApply = func(m *Manager, instance *Instance, responses []*PrepareResponse) error {
	if err := managerPrepareCheckResponses(m, instance, responses); err != nil {
		return err
	}

	remoteInstance := m.analyzePrepareResponses(responses)

	var status InstanceStatus
	var prepareInstance *Instance
	if remoteInstance != nil {
		if instance.Status > remoteInstance.Status {
			prepareInstance = instance
		} else {
			prepareInstance = remoteInstance
		}
		status = prepareInstance.Status
	} else {
		if instance.Status <= INSTANCE_PREACCEPTED {
			logger.Warning("Instance %v not recognized by other replicas, committing noop", instance.InstanceID)
			instance.setNoop()
			status = INSTANCE_PREACCEPTED
			prepareInstance = instance
		} else {
			prepareInstance = instance
			status = INSTANCE_PREACCEPTED
		}
	}


	// for the first step that prepare takes (preaccept, accept, commit), it should use
	// the remote instance, since the remote instance may have newer deps and seq if it's
	// been accepted/committed. For all steps afterwards though, the local instance should
	// be used, since the remote instance will not be updated after each step
	// TODO: send back instances when a message/update is rejected, so the calling node can update itself

	// TODO: consider running the full prepare phase only for preaccepted responses, otherwise, just accept/commit locally
	acceptRequired := true
	var err error
	switch status {
	case INSTANCE_PREACCEPTED:
		// run pre accept phase
		m.statsInc("prepare.apply.preaccept.count", 1)
		logger.Debug("Prepare phase starting at PreAccept phase for %v on %v", instance.InstanceID, m.GetLocalID())
		acceptRequired, err = m.preAcceptPhase(prepareInstance)
		if err != nil {
			m.statsInc("prepare.apply.preaccept.error", 1)
			logger.Debug("Prepare PreAccept error for %v on %v: %v", instance.InstanceID, m.GetLocalID(), err)
			return err
		}
		prepareInstance = instance
		fallthrough
	case INSTANCE_ACCEPTED:
		// run accept phase
		if acceptRequired {
			m.statsInc("prepare.apply.accept.count", 1)
			logger.Debug("Prepare phase starting at Accept phase for %v on %v", instance.InstanceID, m.GetLocalID())
			// use the remote instance to initiate the new accept phase, otherwise
			// the existing (potentially incorrect) attributes will be used for the accept
			err = m.acceptPhase(prepareInstance)
			if err != nil {
				m.statsInc("prepare.apply.accept.error", 1)
				logger.Info("Prepare Accept error for %v on %v: %v", instance.InstanceID, m.GetLocalID(), err)
				return err
			}
			prepareInstance = instance
		}
		fallthrough
	case INSTANCE_COMMITTED, INSTANCE_EXECUTED:
		// commit instance
		m.statsInc("prepare.apply.commit.count", 1)
		logger.Debug("Prepare phase starting at Commit phase for %v on %v", instance.InstanceID, m.GetLocalID())
		// use the remote instance to initiate the new commit phase, otherwise
		// the existing (potentially incorrect) attributes will be committed
		err = m.commitPhase(prepareInstance)
		if err != nil {
			m.statsInc("prepare.apply.commit.error", 1)
			logger.Warning("Prepare Commit error for %v on %v: %v", instance.InstanceID, m.GetLocalID(), err)
			return err
		}
	default:
		m.statsInc("prepare.apply.error", 1)
		return fmt.Errorf("Unknown instance status: %v", remoteInstance.Status)
	}

	return nil
}

var managerPreparePhase = func(m *Manager, instance *Instance) error {
	start := time.Now()
	defer m.statsTiming("prepare.phase.time", start)
	m.statsInc("prepare.phase.count", 1)

	// check if the instance has been committed
	if instance.getStatus() >= INSTANCE_COMMITTED {
		return nil
	}

	responses, err := managerSendPrepare(m, instance)
	if err != nil { return err }

	err = managerPrepareApply(m, instance, responses)
	if err != nil {
		return err
	}

	return nil
}

// TODO: test alone
// attempts to defer the prepare phase to successor nodes
// returns a bool indicating if the prepare should proceed locally
//
// At instance creation time, a list of the other replica node ids is added to the instance, randomly ordered.
// After a commit timeout, the node waiting on the commit should send a message asking the node to return the
// result of a prepare phase. If the node doesn't respond in time, it continues down the successor list until
// it receives a response, or it's next in line.
// If a commit timeout occurs and the node it occurs in is the first in line, it will run the prepare phase.
// When non leader node calculates a commit timeout, it should use it's position in the successor list as
// a variable in computing it.
var managerDeferToSuccessor = func(m *Manager, instance *Instance) (bool, error) {
	start := time.Now()
	defer m.statsTiming("prepare.successor.time", start)
	m.statsInc("prepare.successor.count", 1)

	// make a map of replicas
	replicas := m.getInstanceReplicas(instance)
	replicaMap := make(map[node.NodeId]node.Node, len(replicas))
	for _, replica := range replicas {
		replicaMap[replica.GetId()] = replica
	}

	for _, nid := range instance.getSuccessors() {
		if nid == m.GetLocalID() {
			break
		}
		if replica, exists := replicaMap[nid]; exists {
			recvChan := make(chan *PrepareSuccessorResponse)
			errChan := make(chan bool)
			go func() {
				m.statsInc("prepare.successor.message.send.count", 1)
				msg := &PrepareSuccessorRequest{InstanceID: instance.InstanceID}
				logger.Debug("Prepare Successor: Sending message to node %v for instance %v", replica.GetId(), instance.InstanceID)
				if response, err := replica.SendMessage(msg); err != nil {
					logger.Warning("Error receiving PrepareSuccessorResponse: %v", err)
					errChan <- true
				} else {
					if preAccept, ok := response.(*PrepareSuccessorResponse); ok {
						logger.Debug("Prepare Successor: response received from node %v for instance %v", replica.GetId(), instance.InstanceID)
						recvChan <- preAccept
					} else {
						logger.Warning("Unexpected Prepare Successor response type: %T", response)
					}
				}
			}()
			var response *PrepareSuccessorResponse
			select {
			case response = <- recvChan:
				logger.Debug("Prepare Successor: response received from %v for instance %v", nid, instance.InstanceID)
				if response.Instance == nil {
					// successor is not aware of instance, go to the next successor
					logger.Debug("Prepare Successor: nil instance received from %v for instance %v", nid, instance.InstanceID)
					continue
				}
				m.statsInc("prepare.successor.message.receive.success.count", 1)

				if response.Instance.Status > instance.getStatus() {
					switch response.Instance.Status {
					case INSTANCE_ACCEPTED:
						if err := m.acceptInstance(instance, false); err != nil {
							return false, err
						}
						return false, nil
					case INSTANCE_COMMITTED, INSTANCE_EXECUTED:
						if err := m.commitInstance(instance, false); err != nil {
							return false, err
						}
						return true, nil
					}
				}
				return false, nil

			case <- errChan:
				// there was an error communicating with the successor, go to the next one
				// the send/receive go routine handles the logging
				m.statsInc("prepare.successor.message.receive.error.count", 1)

			case <- instance.getCommitEvent().getChan():
				// instance was committed while waiting for a PrepareSuccessorResponse
				m.statsInc("prepare.successor.message.receive.commit.count", 1)
				logger.Debug("Prepare Successor: commit event received for instance %v", instance.InstanceID)
				return true, nil

			case <- getTimeoutEvent(time.Duration(SUCCESSOR_TIMEOUT) * time.Millisecond):
				// timeout, go to the next successor
				m.statsInc("prepare.successor.message.receive.timeout.count", 1)
				logger.Info("Prepare Successor: timeout communicating with %v for instance %v", nid, instance.InstanceID)

			}
		}

	}
	return true, nil
}

// TODO: test alone
// determines a prepare phase should proceed for the given instance
// will block if waiting on a timeout / notification
func (m *Manager) prepareShouldProceed(instance *Instance) bool {

	// bail out if the instance has already been committed
	if instance.getStatus() >= INSTANCE_COMMITTED {
		return false
	}

	if time.Now().After(instance.getCommitTimeout()) {
		logger.Debug("Prepare: commit grace period expired. proceeding")
		// proceed, the instance's commit grace period
		// has expired
		m.statsInc("prepare.proceed.timeout.count", 1)
	} else {
		m.statsInc("prepare.proceed.commit.wait.count", 1)
		logger.Debug("Prepare: waiting on commit grace period to expire")
		select {
		case <- instance.getCommitEvent().getChan():
			logger.Debug("Prepare: commit broadcast event received for %v", instance.InstanceID)
			// instance was executed by another goroutine
			m.statsInc("prepare.proceed.commit.wait.broadcast.count", 1)
			return false
		case <- instance.getCommitTimeoutEvent():
			logger.Debug("Prepare: commit grace period expired for %v. proceeding", instance.InstanceID)
			// execution timed out

			if instance.getStatus() >= INSTANCE_COMMITTED {
				// continue if it was
				return false
			} else {
				m.statsInc("prepare.proceed.timeout.count", 1)
				m.statsInc("prepare.proceed.timeout.wait.count", 1)
			}
		}
	}
	return true
}

var managerPrepareInstance = func(m *Manager, inst *Instance) error {
	start := time.Now()
	defer m.statsTiming("prepare.instance.time", start)
	m.statsInc("prepare.instance.count", 1)
	instance, _ := m.getOrSetInstance(inst)
	instance.prepareLock.Lock()
	defer instance.prepareLock.Unlock()

	if !m.prepareShouldProceed(instance) {
		return nil
	}

	//
	deferred, err := managerDeferToSuccessor(m, instance)
	var commitEvent <- chan bool
	for (!deferred || err != nil) && inst.getStatus() < INSTANCE_COMMITTED {
		if commitEvent == nil {
			commitEvent = instance.getCommitEvent().getChan()
		}
		if err != nil {
			m.statsInc("prepare.instance.defer.error", 1)
			return err
		}
		m.statsInc("prepare.instance.defer.commit.wait", 1)
		select {
		case <- getTimeoutEvent(time.Duration(SUCCESSOR_CONTACT_INTERVAL)):
			m.statsInc("prepare.instance.defer.timeout", 1)
			// interval passed, contact successor again
		case <- commitEvent:
			// instance was committed
			m.statsInc("prepare.instance.defer.commit.wait.broadcast.count", 1)
			return nil
		}
		deferred, err = managerDeferToSuccessor(m, instance)
	}

	// double check we should proceed
	if !m.prepareShouldProceed(instance) {
		return nil
	}

	logger.Debug("Prepare phase started")
	err = managerPreparePhase(m, instance)
	logger.Debug("Prepare phase completed")
	return err
}

// TODO: rename to prepareInstance
// runs explicit prepare phase on instances where a command leader failure is suspected
// during execution,
func (m *Manager) preparePhase(instance *Instance) error {
	return managerPrepareInstance(m, instance)
}

// handles a prepare message from an instance attempting to take
// control of an instance.
func (m *Manager) HandlePrepare(request *PrepareRequest) (*PrepareResponse, error) {
	start := time.Now()
	defer m.statsTiming("prepare.message.response.time", start)
	m.statsInc("prepare.message.received.count", 1)

	logger.Debug("Prepare message received for instance %v, ballot: %v", request.InstanceID, request.Ballot)

	instance := m.getInstance(request.InstanceID)
	response := &PrepareResponse{}
	var responseBallot uint32
	if instance == nil {
		response.Accepted = true
	} else {
		response.Accepted = request.Ballot > instance.getBallot()
		if response.Accepted {
			m.statsInc("prepare.message.response.accepted.count", 1)
			if instance.updateBallot(request.Ballot) {
				if err := m.Persist(); err != nil {
					return nil, err
				}
			}
		} else {
			m.statsInc("prepare.message.response.rejected", 1)
			logger.Info("Prepare message rejected for %v, %v >= %v", request.InstanceID, instance.MaxBallot, request.Ballot)
		}
		if instanceCopy, err := instance.Copy(); err != nil {
			return nil, err
		} else {
			response.Instance = instanceCopy
		}
		responseBallot = response.Instance.MaxBallot
	}

	logger.Debug("Prepare message for %v replied with accept: %v (%v)", request.InstanceID, response.Accepted, responseBallot)
	return response, nil
}

// handles a message from a replica requesting that a prepare phase is executed on the given instance
func (m *Manager) HandlePrepareSuccessor(request *PrepareSuccessorRequest) (*PrepareSuccessorResponse, error) {
	start := time.Now()
	defer m.statsTiming("prepare.successor.message.response.time", start)
	m.statsInc("prepare.successor.message.received.count", 1)

	response := &PrepareSuccessorResponse{}
	if instance := m.instances.Get(request.InstanceID); instance != nil {
		if instanceCopy, err := instance.Copy(); err != nil {
			return nil, err
		} else {
			response.Instance = instanceCopy
		}
		go func(){
			if instance.getStatus() < INSTANCE_COMMITTED {
				successors := instance.getSuccessors()
				successorNum := len(successors)
				for i, nid := range successors {
					if nid == m.topology.GetLocalNodeID() {
						successorNum = i
					}
				}
				var commitEvent <- chan bool
				for i:=0; i<BALLOT_FAILURE_RETRIES; i++ {
					// refresh the local instance pointer and
					// assign the commit event so a goroutine is not
					// spun up for each attempt
					instance = m.instances.Get(instance.InstanceID)
					if commitEvent == nil {
						commitEvent = instance.getCommitEvent().getChan()
					}

					prepareStart := time.Now()
					defer m.statsTiming("prepare.successor.prepare.time", prepareStart)
					m.statsInc("prepare.successor.prepare.count", 1)
					m.statsInc(fmt.Sprintf("prepare.successor.%v", successorNum), 1)
					if err := m.preparePhase(instance); err != nil {
						if _, ok := err.(BallotError); ok {
							logger.Debug("Prepare failed with BallotError, waiting to try again")

							// wait on broadcast event or timeout
//							waitTime := BALLOT_FAILURE_WAIT_TIME * uint64(successorNum) * uint64(i + 1)
							waitTime := BALLOT_FAILURE_WAIT_TIME * uint64(successorNum)
							logger.Info("Prepare failed with BallotError, waiting for %v ms to try again", waitTime)
							timeoutEvent := getTimeoutEvent(time.Duration(waitTime) * time.Millisecond)
							select {
							case <- commitEvent:
								// another goroutine committed
								// the instance
								return
							case <-timeoutEvent:
								// continue with the prepare
							}
						} else {
							m.statsInc("prepare.successor.prepare.error", 1)
							logger.Error("Prepare successor failed with error: %v", err)
						}

					} else {
						return
					}
				}
			}
		}()
	}
	return response, nil
}



