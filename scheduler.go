package main

import (
	"context"
	"time"
)

// ============================================================================
// Scheduler — 多級優先級排程器
// ============================================================================

// Priority levels
const (
	PriorityCritical = 0 // P0: context cancel, safety abort
	PriorityHigh     = 1 // P1: plan timeout, error escalation, wake inject
	PriorityNormal   = 2 // P2: CallModel, tool exec, guard checks
	PriorityLow      = 3 // P3: history compression, plan suggest, todos reminder
	PriorityIdle     = 4 // P4: self-learning, trajectory, strategy optimization
	NumPriorities    = 5
)

// TCBState represents the current state of a TCB
type TCBState int

const (
	TCBReady TCBState = iota
	TCBRunning
	TCBBlocked
	TCBDone
)

// TaskResult represents the result of executing a task
type TaskResult int

const (
	ResultContinue TaskResult = iota // task not done, re-enqueue
	ResultDone                       // task completed
	ResultBlock                      // task is blocked (waiting for external event)
	ResultYield                      // task voluntarily yields
)

// TaskHandler is the unified handler interface.
// All task handlers share this signature and receive the message list + channel.
type TaskHandler func(ctx context.Context, ml *MessageList, ch Channel) (TaskResult, error)

// TCB is a Task Control Block
type TCB struct {
	ID        string
	Name      string
	Priority  int
	State     TCBState
	TimeSlice time.Duration
	Handler   TaskHandler
}

// ============================================================================
// ReadyQueue — bitmap + 5 priority levels, O(1) enqueue/dequeue
// ============================================================================

type ReadyQueue struct {
	queues [NumPriorities][]*TCB
	bitmap uint8
}

// Enqueue adds a TCB to the queue at its priority level
func (rq *ReadyQueue) Enqueue(tcb *TCB) {
	pri := tcb.Priority
	if pri < 0 {
		pri = 0
	}
	if pri >= NumPriorities {
		pri = NumPriorities - 1
	}
	rq.queues[pri] = append(rq.queues[pri], tcb)
	rq.bitmap |= 1 << pri
}

// Dequeue removes and returns the highest priority TCB (FIFO within level)
func (rq *ReadyQueue) Dequeue() *TCB {
	for pri := 0; pri < NumPriorities; pri++ {
		if rq.bitmap&(1<<pri) != 0 {
			q := rq.queues[pri]
			tcb := q[0]
			rq.queues[pri] = q[1:]
			if len(rq.queues[pri]) == 0 {
				rq.bitmap &^= 1 << pri
			}
			return tcb
		}
	}
	return nil
}

// HasReady returns true if any queue has tasks
func (rq *ReadyQueue) HasReady() bool {
	return rq.bitmap != 0
}

// HasReadyExcept returns true if any queue except the given priority has tasks
func (rq *ReadyQueue) HasReadyExcept(pri int) bool {
	if pri < 0 || pri >= NumPriorities {
		return rq.bitmap != 0
	}
	mask := ^uint8(1 << pri)
	return (rq.bitmap & mask) != 0
}

// ============================================================================
// Scheduler
// ============================================================================

type Scheduler struct {
	ready    ReadyQueue
	tasks    map[string]*TCB
	exitFlag bool
	done     map[string]bool
	blocked  map[string]bool
	preempt  chan struct{}
}

// NewScheduler creates a new scheduler
func NewScheduler() *Scheduler {
	return &Scheduler{
		tasks:   make(map[string]*TCB),
		done:    make(map[string]bool),
		blocked: make(map[string]bool),
		preempt: make(chan struct{}, 1),
	}
}

// Register adds a task handler to the scheduler
func (s *Scheduler) Register(id, name string, priority int, handler TaskHandler) {
	s.tasks[id] = &TCB{
		ID:       id,
		Name:     name,
		Priority: priority,
		State:    TCBReady,
		Handler:  handler,
	}
}

// EnqueueTask places a registered task into the ready queue
func (s *Scheduler) EnqueueTask(id string) {
	tcb, ok := s.tasks[id]
	if !ok {
		return
	}
	tcb.State = TCBReady
	s.ready.Enqueue(tcb)
}

// HasReady returns true if any tasks are ready
func (s *Scheduler) HasReady() bool {
	return s.ready.HasReady()
}

// HasReadyExcept returns true if tasks exist outside the given priority level
func (s *Scheduler) HasReadyExcept(pri int) bool {
	return s.ready.HasReadyExcept(pri)
}

// IsTaskBlocked returns true if the task is in blocked state
func (s *Scheduler) IsTaskBlocked(id string) bool {
	return s.blocked[id]
}

// IsTaskDone returns true if the task has completed
func (s *Scheduler) IsTaskDone(id string) bool {
	return s.done[id]
}

// GetExitFlag returns the exit flag (set when branch_none completes normally)
func (s *Scheduler) GetExitFlag() bool {
	return s.exitFlag
}

// SetExitFlag signals that the main loop should exit
func (s *Scheduler) SetExitFlag() {
	s.exitFlag = true
}

// UnblockTask moves a task from blocked back to ready
func (s *Scheduler) UnblockTask(id string) {
	delete(s.blocked, id)
	s.EnqueueTask(id)
}

// ResetDone clears the done flag for a task (for re-use across iterations)
func (s *Scheduler) ResetDone(id string) {
	delete(s.done, id)
}

// Tick dispatches the highest-priority ready task and executes it.
// Returns the handler's error or nil.
func (s *Scheduler) Tick(ctx context.Context, ml *MessageList, ch Channel) error {
	select {
	case <-s.preempt:
		// Preempt signal received — current task was already re-enqueued
	default:
	}

	tcb := s.ready.Dequeue()
	if tcb == nil {
		return nil
	}

	tcb.State = TCBRunning
	result, err := tcb.Handler(ctx, ml, ch)

	switch result {
	case ResultContinue:
		s.blocked[tcb.ID] = false
		s.done[tcb.ID] = false
		s.ready.Enqueue(tcb)
	case ResultDone:
		tcb.State = TCBDone
		s.done[tcb.ID] = true
		delete(s.blocked, tcb.ID)
	case ResultBlock:
		tcb.State = TCBBlocked
		s.blocked[tcb.ID] = true
		s.done[tcb.ID] = false
	case ResultYield:
		tcb.State = TCBReady
		s.blocked[tcb.ID] = false
		s.done[tcb.ID] = false
		s.ready.Enqueue(tcb)
	}

	return err
}
