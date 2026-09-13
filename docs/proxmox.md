---
title: Renting runner hosts from Proxmox VE
description: >-
  Let Zoomies clone a prepared VM template on your own Proxmox VE cluster when a
  pool has queued work and nowhere to run it, and destroy the machine again when
  the work is done — with scoped API credentials and verified ownership.
---

# Proxmox VE

Zoomies can rent its own hosts. When a pool has queued jobs and no host in the
fleet can run them, a **provider** clones a prepared VM template on your Proxmox
VE cluster, installs the Zoomies agent inside it, runs the work, and destroys
the machine again once it has been idle long enough.

The machine is an ordinary Zoomies host. It appears on the Hosts page, runs the
same runner backend, and is drained the same way. What is different is that
Zoomies created it, knows it did, and is the only thing allowed to delete it.

!!! warning "What is qualified, and what is not"

    One combination is supported: a **Linux cloud image with `qemu-guest-agent`
    and the Zoomies agent preinstalled**, running the **Docker** runner backend,
    cloned from a template you prepared. That is what has been tested end to
    end. Other operating systems, other backends and other bootstrap routes are
    not "probably fine" — they are untested, and this page will say so until
    they are not.

    Renting machines is off until you turn it on: `provider.enabled` defaults to
    `false`, and `provider.max_machines` defaults to **zero, which rents
    nothing** — a maximum of none is none, exactly as it is for a pool's
    `max_runners`. Set both in the same edit, or the fleet will look enabled and
    buy nothing. That is deliberate: the alternative reading, where an unset
    number means "as many as it takes", puts the one setting that decides the
    size of an invoice behind a value somebody can forget.

## What you need

| | |
| --- | --- |
| Proxmox VE | 8.0 or later, reachable from the controller over HTTPS on port 8006 |
| A prepared template | a Linux VM template, described [below](#preparing-the-template) |
| An API token | scoped, described [below](#the-api-token) |
| A VMID range | a block of VM identifiers Zoomies may use and nothing else may |
| Storage and a bridge | the storage the clone's disk lands on, and the network bridge it attaches to |

Zoomies never needs SSH to the hypervisor, a root password, or a shell on a
node. Everything below is the Proxmox API and nothing else.

## The API token

Create a dedicated user and an API token for it. Do not reuse `root@pam`: the
whole point of the privilege list below is that a mistake in Zoomies, or a
compromise of the controller, cannot reach beyond the VMs it rents.

In the Proxmox UI, **Datacenter → Permissions → Users** to add
`zoomies@pve`, then **API Tokens** to add a token to it. Leave *Privilege
Separation* on — the token then has only the privileges you grant it
explicitly, which is what the next step does.

Grant the token these privileges, on these paths:

| Privilege | Path | What it is for |
| --- | --- | --- |
| `VM.Clone` | the template's `/vms/<template vmid>` | cloning the template |
| `VM.Allocate` | `/vms` | creating and destroying the clones |
| `VM.Audit` | `/vms` | inspecting a machine, and the ownership sweep |
| `VM.Config.Disk`, `VM.Config.CPU`, `VM.Config.Memory`, `VM.Config.Network`, `VM.Config.Options` | `/vms` | sizing the clone and stamping ownership on it |
| `VM.PowerMgmt` | `/vms` | starting and shutting down |
| `VM.GuestAgent.Unrestricted` | `/vms` | installing the agent inside the guest, and reading back what it said if that failed |
| `Datastore.AllocateSpace` | `/storage/<your storage>` | the clone's disk |

`VM.GuestAgent.Unrestricted` is the one worth pausing over: it lets the token
write files into, and run commands inside, any VM on those paths. That is how
the enrolment credential reaches the guest without ever being written into VM
metadata, and it is the reason to scope the token to a resource pool containing
only Zoomies' own VMs if your cluster runs anything else you care about.

Zoomies checks all of this before it creates anything. **Providers → your
provider → Check** asks the token what privileges it holds — Proxmox lets a
token read its own permissions — and names each missing one rather than
failing at the first clone. Run it after any change to the token.

Paste the token into the provider form as
`user@realm!tokenid=secret`, exactly as Proxmox printed it. It is sealed with
the instance encryption key before it reaches the database, never appears in
the API, an audit row, a diagnostics bundle or a log line, and never reaches a
guest. See [Security](security.md).

### TLS

A fresh Proxmox cluster serves its own certificate, signed by the cluster's own
CA. Paste that CA — `/etc/pve/pve-root-ca.pem` on any node — into the
provider's **CA certificate** field and the connection is verified against it.

Take the certificate, not the key: `pve-root-ca.key` next to it is the thing
that signs certificates, and Zoomies will tell you if you paste it by mistake.

The alternative, `insecure_skip_verify`, turns off verification entirely and is
a warning on the provider and in the problems drawer for as long as it is on.
Anybody who can get between the controller and the hypervisor can then read the
API token in flight. It exists for a first ten minutes, not for a deployment.

## Preparing the template

The template is the part you own. Zoomies clones it and installs nothing that
is not already there, so what is in the image is what a runner gets.

Start from a distribution cloud image — Ubuntu 24.04 LTS is what has been
qualified — and, in one VM you then convert to a template:

1. **Install the Docker engine**, or whichever runner backend the machines will
   offer, and make sure the service starts at boot.
2. **Install `qemu-guest-agent`** and enable it. This is how Zoomies reaches
   inside the guest to enrol it, and a template without it is a machine that
   boots, costs money and never joins. Set `agent: enabled=1` on the VM.
3. **Install the Zoomies agent binary** at `/usr/local/bin/zoomies` and its
   systemd unit — `zoomies agent install --no-start` writes both — and leave
   the unit **disabled**. Zoomies enables it once the machine has a credential.
4. **Remove any agent state.** Delete `/var/lib/zoomies/agent.json` if one
   exists.
5. Shut the VM down and **convert it to a template**.

!!! danger "Never leave an enrolled agent in the template"

    `agent.json` holds one host's identity. A template containing one clones
    that identity into every machine made from it, and two agents then share a
    host row and split its tasks between them — which shows up as work
    vanishing, not as an error.

    Nothing can check this for you, and it is worth being plain about why: the
    template is powered off, so its guest agent is not running and no API call
    can read a file inside it. Zoomies sees the *consequence* — it raises a
    problem when a host's agent session alternates between two identities — but
    by then you are debugging vanishing work rather than preparing an image.
    Check it before you convert the VM.

Zoomies never installs an operating system, applies updates, or reboots a
machine it rents. If the image needs patching, rebuild the template: machines
are disposable, which is what makes that cheap.

## The provider

**Providers → Add provider → Proxmox VE**. The form asks for what the
credential can actually see — the nodes, storages and bridges come from the
cluster, not from a text box — and shows you the consequence of each choice
before you commit to it.

| Setting | What it means |
| --- | --- |
| Endpoint | `https://pve.example.com:8006`. Plain `http://` is refused: the API token would cross in the clear. |
| Nodes | Which nodes clones may be made on. Give more than one and machines are spread across them, each new clone going to the least loaded. |
| Template VMID | The template prepared above. The preflight checks it exists and is a template. |
| Storage | Where the clone's disk lands. Must accept disk images. |
| Bridge | The network bridge the machine attaches to. It must reach the controller. |
| VMID range | The block of identifiers Zoomies may allocate from, and nothing else may. |
| Machine shape | CPUs, memory and disk for each machine, and the labels, capacity and backend the host will report. |
| Maximum machines | How many this provider may run at once. |

The **VMID range** is worth setting deliberately. Zoomies allocates the lowest
free identifier inside it, so the range is both a budget and a blast radius: a
machine outside it is, by construction, not one of ours. Give Zoomies a block
nothing else uses — `9000–9099`, say — and keep it out of whatever your other
tooling allocates from.

A machine's shape is one shape per provider. If you want two sizes, make two
providers: a pool asks for the machine it fits, and having one row mean two
different machines makes the accounting ambiguous in exactly the place it has
to be exact.

## What happens when a pool runs out of hosts

```mermaid
sequenceDiagram
    autonumber
    participant P as pool with queued jobs
    participant Z as Zoomies controller
    participant PVE as Proxmox VE
    participant M as the machine

    P->>Z: 3 jobs queued, no host can run them
    Z->>Z: write the machine row -- identity first, always
    Z->>PVE: clone the template into the allocated VMID
    PVE-->>Z: UPID -- the task to follow
    Z->>PVE: size it, stamp ownership, start it
    Z->>PVE: wait for the guest agent to answer
    Z->>M: write the enrolment file, enable the agent
    M->>Z: join, using a token good for this machine alone
    Z->>Z: the machine is a host; the scheduler places runners
    Note over M: the jobs run
    Z->>Z: no runners for 15 minutes, sustained
    Z->>M: drain the host
    Z->>PVE: verify ownership, then delete
    Z->>PVE: inspect until the VM is really gone
```

Two of those steps are the whole design and are worth stating plainly:

* **The row exists before the VM does.** Zoomies writes the machine's identity —
  which node, which VMID, which name — before it asks Proxmox for anything.
  When a clone request times out, that is not evidence the clone did not happen,
  and the answer is to go and look for the identity we already chose rather than
  to ask for a second machine. A controller killed at any point in the sequence
  resumes by looking, never by creating.
* **A delete needs four agreeing facts.** The row, a fresh inspection of the VM,
  this controller holding the fleet's lease, and a state that permits it. If the
  VM at that VMID does not carry the ownership record this machine stamped on
  it, nothing is deleted and the machine is **quarantined** for a person to look
  at. A recycled VMID is a realistic accident; destroying somebody else's VM
  because of one is not a realistic mistake to recover from.

## Day to day

**Pausing.** The Hosts page has a switch that stops new machines being created.
Drains, deletions, recovery and ownership checks all continue — a pause that
stopped those too would leave VMs running with nothing tending them. It is a
row, so it survives a restart, and it is audited.

**A machine that will not come up.** Its page carries the failure in the words
whatever refused it used: Proxmox's own task error for a clone, and the guest's
own standard error for a bootstrap. Three failures in a row stand the provider
down for a widening interval, so a bad template costs a few machines rather
than fifty.

**Orphan review.** **Providers → your provider → Orphans** has three lists:

* *Untracked resources* — VMs wearing Zoomies' name or marks with no row behind
  them. Zoomies never deletes one. They are what a database restored from
  backup, or a row pruned too early, leaves behind, and they are real machines
  costing real money, so they are shown until somebody deals with them.
* *Machines with no resource* — rows whose VM has gone. Usually somebody deleted
  it in the Proxmox console.
* *Ownership unverified* — the quarantine. A row and a VM that disagree about
  who owns what.

For a quarantined machine, **Release** forgets the row without touching the VM.
That is deliberately the only escape hatch: if the two disagree, Zoomies is the
one that should stand down, and deleting the VM is a decision for whoever can
see both sides.

## Recovery

**A controller restart** needs nothing. The machine loop reads the rows, asks
Proxmox about every operation still in flight, and carries on. There is no
separate startup path, because a restart is simply the pass where nothing was
in memory.

**A restored database** is treated as possibly describing somebody else's
machines — because it might: two controllers restored from one backup would
both believe they own the same VMs. Restoring sets the fence, and while it is
set Zoomies will not create or delete anything at all, though it keeps
inspecting and reporting. Lifting the fence marks every machine's ownership
unverified, and each one has to be re-proved against the hypervisor before it
can be acted on. See [Backup and restore](backup-and-restore.md).

**A hypervisor restart** looks like a provider that cannot be reached, which is
never evidence about a resource: nothing is created, failed or deleted on the
strength of it. When the cluster comes back the sweep reconciles what is
actually there against the rows.

## Qualifying your own cluster

Fixture tests prove the logic; they prove nothing about your cluster. Before
this is load-bearing for you, run the procedure below against **disposable**
resources and record what happened. This is the same procedure Zoomies'
own release qualification uses, and
[the record it produced](https://github.com/eyupio/zoomies/blob/main/roadmap/validation/proxmox-qualification.md)
is in the repository.

1. Twenty full cycles — create, enrol, run a real workflow job, drain, delete.
2. Scale from zero: no hosts at all, then queued work.
3. A burst across two pools at once.
4. A controller restart in the middle of a create.
5. A deliberately broken template, to see the bootstrap failure reported.
6. A deletion that fails once, to see it retried and confirmed.
7. Finally, reconcile the VM and storage inventory against the machines table.
   **No owned resource may be left unexplained.**

Record the Proxmox version, the template, the limits, the timings and the
cleanup evidence. A cycle that worked without evidence is a cycle nobody else
can check.
