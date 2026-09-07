// Package drill is the runtime tier: it runs the built binary, as a real
// controller and a real remote agent, against an in-process fake GitHub, and
// watches a workload actually appear on this machine and go away again.
//
// It is behind the "drill" build tag. Everything else in this repository tests
// the code in the test process; this is the only place that qualifies the
// product as the operator gets it -- two processes, a join token, a backend
// touching the host -- and the only place ZF-302's fault drills can run
// repeatably, because the GitHub side is a fake rather than somebody's real
// organisation.
//
// See README.md in this directory for what it needs and what it asserts.
package drill
