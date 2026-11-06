package controller

import (
	"github.com/eyupio/zoomies/internal/agent"
)

// QueueMergeConflict asks one host to run the bounded Git worker against the
// repository an attempt holds, and reports whether the task was taken.
//
// The repository does not travel in the task: the agent pulls it from the
// controller by attempt id, which is what keeps a GitHub credential out of the
// host and a megabyte out of the queue. The three pinned commit ids do travel,
// because they are the request the worker is given, and they are already public
// to anyone who can read the pull request. A false return means the host already
// has that attempt in flight, or has gone away.
func (c *Controller) QueueMergeConflict(hostID string, prepared MergePrepared) bool {
	if hostID == "" || prepared.ID == "" {
		return false
	}
	return c.enqueue(hostID, agent.Task{
		Kind:          agent.TaskMergeConflict,
		MergeID:       prepared.ID,
		MergeImage:    prepared.Image,
		MergeAncestor: prepared.Ancestor,
		MergeBase:     prepared.Base,
		MergeHead:     prepared.Head,
	})
}
