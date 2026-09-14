// Package proxmox is the live qualification harness for the Proxmox VE
// provider: twenty create, enrol, run, drain and delete cycles against a real
// hypervisor, the five fault cases the roadmap names, and a closing inventory
// reconciliation that fails on any owned resource nobody can account for.
//
// It needs a disposable Proxmox VE cluster -- a VMID range reserved for this
// and nothing else, an API token, a prepared template, a storage -- and a
// GitHub installation that can queue real jobs, because a machine that never
// ran a job has not been qualified for anything. Without those it skips itself,
// naming every one that is missing; see README.md for the variables and
// roadmap/validation/proxmox-qualification.md for the procedure and the
// evidence it produces.
//
// **Fixture success is not live qualification.** The fixture suite -- the whole
// machine lifecycle against provider.Fake, and the Proxmox client against an
// httptest server -- proves the logic and the wire format and proves nothing
// whatever about a hypervisor. Only a green run of this harness against a real
// cluster, with its evidence filled into the qualification record, is that.
//
// Only the scenarios themselves are behind the "e2e" build tag. The budget, the
// ledger, the preflight, the inventory check and the evidence record are
// ordinary code with ordinary tests, so that a harness that cannot be run here
// is still compiled, vetted and tested here: the failure designed out is a
// qualification harness that has quietly stopped building, discovered by the
// one person who had a cluster and an afternoon.
package proxmox
