package app

import (
	"context"
	"sync"
)

const peopleAgentQueueCap = 4

type peopleAgentJob struct {
	threadID  string
	messageID string
	body      string
	targetID  string
	hop       int
}

var (
	peopleAgentQueueMu    sync.Mutex
	peopleAgentBusy       = map[string]bool{}
	peopleAgentQueued     = map[string][]peopleAgentJob{}
	peopleAgentDropNotice = "上一句还在处理，这句没排上。"
)

func resetPeopleAgentQueueForTest() {
	peopleAgentQueueMu.Lock()
	defer peopleAgentQueueMu.Unlock()
	peopleAgentBusy = map[string]bool{}
	peopleAgentQueued = map[string][]peopleAgentJob{}
}

// enqueuePeopleAgentTurn returns started=true when this caller should run the
// offered job and then drain. The in-flight job is not stored in the waiting
// list, so the cap is four queued turns behind the current one. dropped is a
// user-visible notice when that waiting list is full.
func enqueuePeopleAgentTurn(threadID, messageID, body string) (job peopleAgentJob, started bool, dropped string) {
	return enqueuePeopleAgentJob(peopleAgentJob{threadID: threadID, messageID: messageID, body: body})
}

func enqueuePeopleAgentJob(job peopleAgentJob) (peopleAgentJob, bool, string) {
	peopleAgentQueueMu.Lock()
	defer peopleAgentQueueMu.Unlock()
	if peopleAgentBusy[job.threadID] {
		q := peopleAgentQueued[job.threadID]
		if len(q) >= peopleAgentQueueCap {
			return peopleAgentJob{}, false, peopleAgentDropNotice
		}
		peopleAgentQueued[job.threadID] = append(q, job)
		return peopleAgentJob{}, false, ""
	}
	peopleAgentBusy[job.threadID] = true
	return job, true, ""
}

func dequeuePeopleAgentTurn(threadID string) (peopleAgentJob, bool) {
	peopleAgentQueueMu.Lock()
	defer peopleAgentQueueMu.Unlock()
	q := peopleAgentQueued[threadID]
	if len(q) == 0 {
		delete(peopleAgentBusy, threadID)
		delete(peopleAgentQueued, threadID)
		return peopleAgentJob{}, false
	}
	job := q[0]
	peopleAgentQueued[threadID] = q[1:]
	return job, true
}

func (e *Engine) drainPeopleAgentQueue(ctx context.Context, threadID string) {
	for {
		job, ok := dequeuePeopleAgentTurn(threadID)
		if !ok {
			return
		}
		e.runPeopleAgentJob(ctx, job)
	}
}

func (e *Engine) notifyPeopleAgentDropped(ctx context.Context, threadID, notice string) {
	if e == nil || e.people == nil || notice == "" {
		return
	}
	if _, err := e.people.SendSystem(ctx, threadID, notice); err != nil {
		// Fall back to the first agent so the drop is still visible.
		if thread, peekErr := e.people.PeekThread(ctx, threadID); peekErr == nil {
			for _, agent := range peopleAgentMembers(thread) {
				_, _ = e.people.SendAs(ctx, agent.SubjectID, threadID, notice)
				return
			}
		}
	}
}
