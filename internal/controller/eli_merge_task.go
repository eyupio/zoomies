package controller

import (
	"github.com/eyupio/zoomies/internal/agent"
)

// QueueMergeConflict asks one host to run the bounded Git worker against the
// repository an attempt holds, and reports whether the task was taken.
//
// The repository does not travel in the task: the agent pulls it from the
// controller by attempt id, which is what keeps a GitHub credential out of the
// host and a megabyte out of the queue. A false return means the host already has
// that attempt in flight, or has gone away.
func (c *Controller) QueueMergeConflict(hostID, attemptID, image string) bool {
	if hostID == "" || attemptID == "" {
		return false
	}
	return c.enqueue(hostID, agent.Task{
		Kind:       agent.TaskMergeConflict,
		MergeID:    attemptID,
		MergeImage: image,
	})
}
