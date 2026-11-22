---
icon: material/server-outline
title: Renting runner hosts from Proxmox VE
description: >-
  Let Zoomies clone a VM template on your Proxmox VE cluster when a pool has
  queued work, and destroy the machine afterwards, with scoped API credentials.
---

# Proxmox VE

Zoomies can rent its own hosts. When a pool has queued jobs and no host in the
fleet can run them, a **provider** clones a prepared VM template on your Proxmox
VE cluster, installs the Zoomies agent inside it, runs the work, and destroys
the machine again once it has been idle long enough.

Two settings on each provider decide how much it keeps running between bursts.
**Idle timeout** (default 15 minutes) is how long a machine's host must have had
no runner on it before the machine is deleted; `0` deletes it as soon as nothing
needs it. **Warm machines** (`min_machines`; the wizard suggests 1, the API's
default is 0, and neither `zoomies providers add` nor `edit` has a flag for it)
is a floor: the provider buys up to that many with no work waiting, so the first
job finds a machine already up, and never deletes an idle machine below it. A
minimum counts against the provider's maximum, so it cannot be set higher, and a
provider that is disabled, paused or standing down keeps no floor at all.

The machine is an ordinary Zoomies host. It appears on the Hosts page, runs the
same runner backend, and is drained the same way. What is different is that
Zoomies created it, knows it did, and is the only thing allowed to delete it.

!!! warning "What is qualified, and what is not"

    One combination is supported: a **Linux cloud image with `qemu-guest-agent`
    and the Zoomies agent preinstalled**, running the **Docker** runner backend,
    cloned from a template you prepared. That is the combination the
    qualification harness at the end of this page is written for, and the
    harness has not yet been run against a cluster, so nothing on this page
    has been tested end to end: the lifecycle logic and the wire format are
    proved against fixtures, and a hypervisor is not. Other operating systems,
    other backends and other bootstrap routes are untested too, not "probably
    fine", and this page will say so until they are not.

    **Containers** are an experimental prototype and a separate case: see
    [Containers](#containers-experimental), which says what is proved and what
    is not.

    Renting machines is off until an administrator turns it on. The Providers
    page says so at the top and offers a button that switches it on, and the
    setting applies at once, with no restart. Two settings decide it, and both
    must be set: `provider.enabled` defaults to `false`, and
    `provider.max_machines` defaults to **zero, which rents nothing**, because a
    maximum of none is none, exactly as it is for a pool's `max_runners`. A fleet
    with one and not the other looks ready and buys nothing. That is deliberate:
    the alternative reading, where an unset number means "as many as it takes",
    puts the one setting that decides the size of an invoice behind a value
    somebody can forget.

## What you need

| | |
| --- | --- |
| Proxmox VE | 8.0 or later; automatic setup uses Tailcat to reach the local HTTPS API on port 8006 |
| A prepared template | created by automatic setup, or a Linux VM template prepared [manually](#preparing-the-template) |
| An API token | created automatically by the setup command, or [managed manually](#the-api-token) |
| A VMID range | a block of VM identifiers Zoomies may use and nothing else may |
| Storage and a bridge | the storage the clone's disk lands on, and the network bridge it attaches to |

Run the setup command once as root on the Proxmox node. After setup, Zoomies
uses the Proxmox API over the private gateway; it needs no SSH connection or
root password. See [the provider wizard](#the-provider) for automatic setup.

## The API token

Automatic setup creates this for you. The following steps are for
**Configure connection manually**.

Create a dedicated user and an API token for it. Do not reuse `root@pam`: the
whole point of the privilege list below is that a mistake in Zoomies, or a
compromise of the controller, cannot reach beyond the VMs it rents.

In the Proxmox UI, **Datacenter → Permissions → Users** to add
`zoomies@pve`, then **API Tokens** to add a token to it. Leave *Privilege
Separation* on; the token then has only the privileges you grant it
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
| `SDN.Use` | `/sdn/zones/localnetwork` | seeing the node's bridge and attaching the clone to it; without it the token is shown no bridges |

`VM.GuestAgent.Unrestricted` is the one worth pausing over: it lets the token
write files into, and run commands inside, any VM on those paths. That is how
the enrolment credential reaches the guest without ever being written into VM
metadata, and it is the reason to scope the token to a resource pool containing
only Zoomies' own VMs if your cluster runs anything else you care about.

Zoomies checks all of this before it creates anything. **Providers → your
provider → Check** asks the token what privileges it holds (Proxmox lets a
token read its own permissions) and names each missing one rather than
failing at the first clone. Run it after any change to the token.

Paste the token into the provider form as
`user@realm!tokenid=secret`, exactly as Proxmox printed it. It is sealed with
the instance encryption key before it reaches the database, never appears in
the API, an audit row, a diagnostics bundle or a log line, and never reaches a
guest. See [Security](security.md).

### TLS

A fresh Proxmox cluster serves its own certificate, signed by the cluster's own
CA. Paste that CA, `/etc/pve/pve-root-ca.pem` on any node, into the
provider's **CA certificate** field and the connection is verified against it.

Take the certificate, not the key: `pve-root-ca.key` next to it is the thing
that signs certificates, and Zoomies will tell you if you paste it by mistake.
Both certificate boxes take certificates only, and a private key is refused when
you save, with the box it was pasted into named.

That certificate is for the connection from the controller to the cluster, and
only that. A machine the provider builds has a different question to answer:
which certificate to trust for the controller. By default it uses its own trust
store, which is right when the controller's certificate comes from a public
authority. If your controller serves a certificate from a private authority,
paste that authority into **Certificate machines trust for the controller** on
the Placement step, beside the address the machines use to reach it. It is
written to each machine, which then accepts only that certificate from the
controller. A provider set up before the two were separated was given its
cluster's certificate in that box as well, because that is what its machines were
told; if your controller is on a public certificate, empty the box.

The alternative, `insecure_skip_verify`, turns off verification entirely and is
a warning on the provider and in the problems drawer for as long as it is on.
Anybody who can get between the controller and the hypervisor can then read the
API token in flight. It exists for a first ten minutes, not for a deployment.

## Preparing the template

For manual connections, prepare the template yourself. Automatic setup creates
one as described [below](#the-provider). Zoomies clones it and installs nothing that
is not already there, so what is in the image is what a runner gets.

Proxmox ships no templates, and the standard answer is a distribution's cloud
image: a disk image with cloud-init in it, published for exactly this purpose.
Start from Ubuntu 24.04 LTS (it is what the harness is written for) and, in one VM
you then convert to a template:

1. **Install the Docker engine**, or whichever runner backend the machines will
   offer, and make sure the service starts at boot.
2. **Install `qemu-guest-agent`** and enable it. This is how Zoomies reaches
   inside the guest to enrol it, and a template without it is a machine that
   boots, costs money and never joins. Set `agent: enabled=1` on the VM.
3. **Install the Zoomies agent** and its systemd unit, joined to nothing:
   `zoomies agent install` writes the unit so that it reads the enrolment file
   the controller drops into each clone, and leaves it **disabled**. Zoomies
   enables it inside a clone once the clone has a credential of its own.
4. **Remove any agent state.** `zoomies agent install` refuses to run on a
   machine that has already joined, because `/var/lib/zoomies/work/agent.json`
   holds one host's identity; if you are converting a host that once joined,
   delete that file first.
5. Shut the VM down and **convert it to a template**.

The same recipe as commands, on a node as root; the wizard's *Before you
start* panel carries them too, with a copy button:

```sh
wget https://cloud-images.ubuntu.com/noble/current/noble-server-cloudimg-amd64.img
qm create 9000 --name zoomies-template --memory 4096 --cores 2 --cpu host --ostype l26 \
  --net0 virtio,bridge=vmbr0 --scsihw virtio-scsi-pci --agent enabled=1 --serial0 socket --vga serial0
qm set 9000 --scsi0 local-lvm:0,import-from=$PWD/noble-server-cloudimg-amd64.img --boot order=scsi0
qm set 9000 --ide2 local-lvm:cloudinit --ciuser ubuntu --sshkeys ~/.ssh/id_ed25519.pub --ipconfig0 ip=dhcp
qm resize 9000 scsi0 32G && qm start 9000
```

`local-lvm` is the storage name on a default install. If yours has another (the
Datacenter tree lists them), use that name in both `qm set` lines, and make sure
it holds **Disk image** content. If the `scsi0` step fails the VM is still
created, with no disk, and it will boot to a network prompt and nothing else;
the provider's **Check** reports that as `proxmox.template_no_disk`.

Then, inside the guest over SSH:

```sh
curl -fsSL https://get.docker.com | sh && sudo apt-get install -y qemu-guest-agent
curl -fsSL https://zoomies.sh/install.sh | sh -s -- --no-init && sudo zoomies agent install
sudo cloud-init clean && sudo poweroff
```

And back on the node, `qm template 9000`. Give the template a VMID outside the
block you hand to Zoomies: it must never be one of the identifiers a clone can
be allocated.

!!! danger "Never leave an enrolled agent in the template"

    `agent.json` holds one host's identity. A template containing one clones
    that identity into every machine made from it, and two agents then share a
    host row and split its tasks between them, which shows up as work
    vanishing, not as an error.

    Nothing can check this for you, and it is worth being plain about why: the
    template is powered off, so its guest agent is not running and no API call
    can read a file inside it. Zoomies sees the *consequence* (it raises a
    problem when a host's agent session alternates between two identities) but
    by then you are debugging vanishing work rather than preparing an image.
    Check it before you convert the VM.

Zoomies never installs an operating system, applies updates, or reboots a
machine it rents. If the image needs patching, rebuild the template: machines
are disposable, which is what makes that cheap.

### Keeping the runner's agent current

The agent inside a machine is the one the template carried when the machine was
cloned. Zoomies installs nothing of its own in a rented machine, so that image is
the whole of what a machine runs, and a controller that has moved on to a newer
release leaves every machine it has rented behind. There is no command to run
inside one to catch it up either: nobody logs into a clone, and a machine is
destroyed once it has been idle. Preparing the template again is what changes
what the machines built next will run.

* **A template Zoomies prepared** records the release it was prepared from, in
  the guest's description beside the marker Zoomies wrote when it made it.
  **Prepare the template again** on the provider's page gives the command to run
  on the Proxmox node: it prepares an image from the release this controller
  runs and puts it in place of the template's disk, at the same VMID and the same
  name, so the provider row that points at it needs no change and no machine is
  touched. Run it on the node the template lives on: a template on another node
  is left alone, and the command says which node to run it on instead.

  The disk it replaced is left unattached rather than deleted. The command names
  the slot it moved to, and it is yours to remove once the machines built from
  the new image have proved it:

  ```sh
  qm set 9100 --delete unused0
  ```

  A machine being built at that moment may fail and be retried; nothing else is
  affected, because a clone has its own copy of the disk.
* **A template you prepared by hand** is left exactly as it is found. It carries
  no marker, so nothing Zoomies has can tell what is in it or prepare it again:
  rebuild the image as [above](#preparing-the-template) when its release moves
  on.

Either way, the machines running now keep the release they were cloned with and
are replaced as they go idle, so a fleet catches up over the next while rather
than at the moment the template changes. A controller built from main is the
awkward case on both sides: two builds from main cannot be ordered against each
other, so Zoomies cannot tell a machine's build from the controller's and says so
instead of guessing which is newer.

## Containers (experimental)

A provider can build **LXC containers** instead of virtual machines. A container
shares the node's kernel, so it starts in seconds and costs a fraction of a VM's
memory and disk, and no disk is copied to make one. It also has none of a VM's
isolation: whatever escapes the container is running on the node. Choose
containers for a cluster whose jobs are your own, and VMs where they are not.

!!! warning "Not yet run on a cluster"

    The container path is proved against a fake cluster and nothing else. These
    are the things a first real run has to establish, and this page says so
    until they are:

    * **The privileges.** The list below is what the code asks for and what
      Proxmox's own sources suggest. It has not been checked against a
      restricted token, and **Check** will name whatever turns out to be missing.
    * **Docker inside the container.** The container is unprivileged with
      `nesting=1`, because that is all an API token may set (`keyctl`, `fuse`,
      `mount` and a privileged container need `root@pam`). Proxmox's own
      documentation also names `keyctl` for Docker. Whether `nesting` alone is
      enough depends on the image and the kernel, and has not been tried.
    * **The enrolment route** described below, which is new.

Containers are chosen per provider, with **Machine type** set to `lxc`. Nothing
about an existing provider changes: a provider with no machine type builds VMs
exactly as before, and a machine's kind is stored with its identifier
(`lxc:9042`), so changing the setting later never makes Zoomies look for a
container at a VM's number or the other way round.

| | Virtual machine | Container |
| --- | --- | --- |
| Made from | a clone of a VM template | a template archive, such as `local:vztmpl/zoomies-runner.tar.zst` |
| Setting that names it | Template VMID | Container template |
| Disk | a clone of the template's, grown to the shape | a root filesystem on a storage that holds **Container** content, 32 GiB when the shape names none |
| How it enrols | Zoomies writes the credential through the guest agent | the agent inside asks for it, see below |
| Privileges | the [table above](#the-api-token) | the same, less `VM.Clone` and `VM.GuestAgent.Unrestricted`, plus `Datastore.Audit` on the storage holding the template |

The container template archive has to be on **the same storage name on every
node machines are placed on**, because each node reads it from its own view of
the storage; **Check** looks on each node and names the ones that lack it.

### How a container enrols

Proxmox has no API for running a command inside a container, the way the guest
agent does for a VM, and a credential written into the container's description
would be readable by anyone who can audit the cluster. So nothing is pushed in.
The template carries an agent that has no credential and **asks for one** on
first boot, and the controller answers only if both of these hold:

* the request names a machine this controller is waiting on, which it does by
  the container's hostname, set by Zoomies to the machine's own name; and
* the request comes **from the address Proxmox reports for that container**,
  which the controller reads from the node itself, not from anything the
  container says.

What it is then given is exactly what a VM is given: a single-use join token,
valid for minutes, that enrols one host under that one name and nothing else.
Every refused claim gets the same answer whatever the reason, so the endpoint
cannot be used to find out which machines exist, and the reason is in the
controller's log.

The consequence to plan for is that **the controller must see the container's
own address**. A controller behind NAT, or behind a proxy that does not pass the
client address on, sees another one and every claim is refused. Put the
controller where the containers' bridge reaches it directly, or set the
provider's **Controller URL for machines** to an address that does. A container
that reaches the controller over IPv6 only is also refused: the address Proxmox
reports is the first IPv4 one.

### Preparing the container template

On a node as root, make a container from a distribution template and, inside it,
install what a runner needs. `local` here is a storage that holds **Container
template** content; `pveam available` lists what Proxmox offers.

```sh
pveam update && pveam download local ubuntu-24.04-standard_24.04-2_amd64.tar.zst
pct create 9200 local:vztmpl/ubuntu-24.04-standard_24.04-2_amd64.tar.zst \
  --hostname zoomies-template --unprivileged 1 --features nesting=1 \
  --rootfs local-lvm:32 --net0 name=eth0,bridge=vmbr0,ip=dhcp
pct start 9200 && pct enter 9200
```

Then, inside the container:

```sh
curl -fsSL https://get.docker.com | sh
curl -fsSL https://zoomies.sh/install.sh | sh -s -- --no-init && zoomies agent install
mkdir -p /etc/zoomies && cat > /etc/zoomies/zoomies.env <<'EOF'
ZOOMIES_CONTROLLER_URL=https://zoomies.example.com
ZOOMIES_JOIN_TOKEN=claim
ZOOMIES_AGENT_BACKEND=docker
ZOOMIES_WORK_DIR=/var/lib/zoomies/work
EOF
chmod 600 /etc/zoomies/zoomies.env
systemctl enable zoomies-agent
```

`ZOOMIES_JOIN_TOKEN=claim` is not a credential. It is the word that tells the
agent to ask for one, and it is why the agent takes the container's hostname as
its own name whatever `ZOOMIES_AGENT_NAME` says. The controller address is the
one containers on this bridge use to reach it. The machine's capacity and labels
come from the provider, as for a VM; its backend is the one written here.

Leave the container's disk exactly like that, with no `agent.json` in it (the
same rule as for a [VM template](#preparing-the-template)), then stop it and
turn it into a template archive on the node:

```sh
pct stop 9200
vzdump 9200 --mode stop --compress zstd --dumpdir /var/lib/vz/template/cache
mv /var/lib/vz/template/cache/vzdump-lxc-9200-*.tar.zst /var/lib/vz/template/cache/zoomies-runner.tar.zst
pct destroy 9200
```

Then add the provider with the machine type and the archive's volume id. There
is no template VMID to give:

```sh
zoomies providers add proxmox --name proxmox-lxc \
  --endpoint https://pve.example.com:8006 --endpoint-ca-file pve-root-ca.pem \
  --nodes pve1 --storage local-lvm --bridge vmbr0 --vmid-range 9200-9299 \
  --setting machine_type=lxc --setting ostemplate=local:vztmpl/zoomies-runner.tar.zst
```

The storage for the root filesystem must hold **Container** content (`rootdir`),
which `local-lvm` does on a default install; **Check** says so when it does not.
Containers need none of the QEMU guest agent, so a template with none of it is
not a problem. The wizard does not yet offer the machine type, so use the
command line or `--setting` on `zoomies providers edit` for now.

## The provider

**Providers → Add provider → Proxmox VE → Generate setup command**.
Copy the command and run it as root on your Proxmox host. Return to the wizard;
the connection appears automatically. You do not need to enter a name, API token,
certificate, endpoint or Tailcat address.

The wizard shows a short command: two lines that fetch
[`connect-proxmox.sh`](https://zoomies.sh/connect-proxmox.sh) from this site and
run it, so you can read what it does before you run it. **Show the full command**
reveals the same setup written out step by step, for a Proxmox host that cannot
reach zoomies.sh.

The command downloads a separate Zoomies binary, holds the release's checksums to
the release signing key and the binary to those checksums, detects the local
API certificate, creates a dedicated Proxmox user and privilege-separated token,
and installs a persistent Tailcat gateway to the local API on port 8006. It sends
the connection back over the controller's private Tailcat listener using a
connection-bound capability valid for one hour. Credentials and gateway addresses are
sealed in the controller database and never returned to the browser.

An existing Zoomies runner agent keeps its binary, service, state and registration.
The provider has its own service, `zoomies-proxmox-<controller-key>.service`, and
state under `/var/lib/zoomies-proxmox/<controller-key>`. The service is installed
with `Restart=always` and its start rate limit off, because it is the thing that
reconnects: a boot that beats the machine's own internet, a relay interruption, a
crash and a manual kill all end the process, and in every one of them the service
manager brings the private connection back on its own within seconds. On the
controller's side, the provider keeps dialling through its sealed gateway address;
a gateway that has restarted is answered again on the next provider call, and
`provider.unreachable` in the problems list clears by itself once the connection
returns. Running a new setup command
for the same controller reuses the saved token and gateway identity. It does not
join the machine as another runner host. Interrupted setup can be retried; a lost
controller response is acknowledged when the identical connection is sent again.
After success, a root-only local receipt lets the original command be run again
even after its enrolment capability expires, without downloading an older binary.

`sudo zoomies upgrade` on a runner host also updates and restarts its dedicated
Proxmox gateways, preserving API credentials and Tailcat identities. A provider-only
host can run `sudo /var/lib/zoomies-proxmox/<controller-key>/zoomies upgrade`.
`--check` previews these updates without changing gateway files or services.
Controller upgrades do not remotely upgrade a Proxmox host.

For an existing gateway, choose **Use an existing Tailcat gateway** on the Connect
step and enter its private address. **Configure a direct connection** remains
available for an API address Zoomies can reach.

The controller needs private connections enabled. The Proxmox host needs outbound
internet access and systemd. Automatic setup reuses `zoomies-template` if it exists.
Otherwise it installs `libguestfs-tools` on the Proxmox host and prepares an Ubuntu
24.04 cloud image with Docker, the QEMU guest agent and a disabled Zoomies agent
service, using the setup binary. The image checksum is checked before preparation.
It imports the image into active disk-image storage with at least 33 GiB free,
chooses an active bridge (preferring `vmbr0`) and creates a 32 GiB template at a
free VMID starting at 9100, outside the default runner block. The first build can
take several minutes and needs access to `cloud-images.ubuntu.com` and Ubuntu
package mirrors. Automatic image preparation requires an x86_64 host.

The bridge it chose is also offered to the provider, so a node without `vmbr0` is
saved with its own bridge; one you pick on the Placement step is never replaced.

Before it reports back, the script runs the same check as the **Check** button, on the
host and as the new API token. If the token cannot do what the provider needs, it
makes one explicit grant on the node and checks again; if that is not enough it
stops, says why and saves nothing, so a provider that could not work never appears.

Choose the storage and network for clones on the Placement step. Choices
come from the cluster; anything with one answer is already selected. Review the
machine shape and limits, then save. The limits step opens on a recommendation
worked out from the nodes you chose and the size of the template's machine: as
many machines of that size as the nodes hold after keeping memory for Proxmox (at
least 2 GB, or a fifth of each node's memory, whichever is larger), up to a first
ceiling of eight, with one kept warm so the first job after a quiet spell does
not wait for a clone. That counts each node's total memory, not what other guests
are using, so a node shared with other guests holds fewer machines than the
recommendation counts. The sentence beside it says where the figures came from,
and **Use recommendation** puts them back after you have typed your own. A
cluster that does not report the size of its nodes gets two and one instead.
Nothing is created by saving: machines are rented when a pool has work with
nowhere to run, and the warm machine is bought on the next pass.
Run a test machine from the provider's page (below) to see one build and join
before a job depends on it. The setup callback reports QEMU templates,
with their VMIDs and nodes. The Zoomies template's VMID and node are preselected
even when the cluster has other templates. The script prints these identifiers at
completion. Interrupted template imports can be retried; setup resumes only a VM
marked as belonging to that setup and refuses to change an unrelated guest.

For a direct network connection or credentials you manage yourself, choose
**Configure connection manually**. The table and terminal command below describe
that alternative.

| Setting | What it means |
| --- | --- |
| Endpoint | `https://pve.example.com:8006`. Plain `http://` is refused: the API token would cross in the clear. |
| Connection | Direct, or private through a `zoomies gateway` running beside the cluster when it lives on a network the controller cannot reach; a home lab behind a router, typically. See [Private providers](private-hosts.md#private-providers). |
| Nodes | Which nodes clones may be made on. Give more than one and machines are spread across them, each new clone going to the least loaded. |
| Machine type | `qemu` (the default, and what a provider with none means) or `lxc`. See [Containers](#containers-experimental). |
| Template VMID | The template prepared above, for virtual machines. The preflight checks it exists and is a template. |
| Container template | For containers: the archive every container is made from, as `storage:vztmpl/file`. The preflight checks every node holds it. |
| VLAN tag | An optional tag put on every machine's network interface, for either machine type. |
| Storage | Where the clone's disk lands. Must accept disk images, or Container content for containers. |
| Bridge | The network bridge the machine attaches to. It must reach the controller. |
| VMID range | The block of identifiers Zoomies may allocate from, and nothing else may. |
| Machine shape | CPUs, memory and disk for each machine, and the labels, capacity and backend the host will report. Left to the template (the form's placeholder reads "the template decides"), the disk is the template's boot disk: the check reads it and uses it to judge the machines until one has joined and measured itself. The check warns (`provider.machine_disk_small`) when a shape is too small for its slots. |
| Maximum machines | How many this provider may run at once. The wizard recommends a figure from the nodes' size. |
| Warm machines | The fewest kept running when nothing is waiting. The wizard recommends one. |

The settings in that table, except **Warm machines**, are one line in a
terminal, for a machine with no browser or a setup somebody wants to keep in a
script:

```sh
zoomies providers add proxmox --name proxmox-lab \
  --endpoint https://pve.example.com:8006 --endpoint-ca-file pve-root-ca.pem \
  --nodes pve1 --template 9000 --storage local-lvm --bridge vmbr0 \
  --vmid-range 9000-9099 --max-machines 4
```

It asks for the API token without echoing it (or reads it from standard
input, or from `--credential-file`) checks the answers before saving anything,
and then runs the same check the **Check** button does, so the first thing it
prints after "created" is what the cluster would refuse. The nodes, storage and
bridge have to be typed rather than picked from a list, because the list comes
from the credential and the credential is not saved until the provider is;
`zoomies providers check` names anything the cluster does not recognise.
`zoomies providers edit proxmox-lab --max-machines 8` raises the ceiling later,
and `--setting key=value` reaches the advanced settings the form keeps behind a
disclosure; `zoomies providers kinds` lists them. See [the CLI](cli.md#zoomies-providers).

The command line cannot set **Warm machines**. Neither `add` nor `edit` has a
flag for it, and it is a field of the provider itself rather than one of the
driver's settings, so `--setting` does not reach it. A provider added from a
terminal therefore keeps no machine warm, and an edit leaves a floor set
elsewhere as it was. Set it under **Providers → your provider → Settings** with
**Edit these settings**, or send `min_machines` to `PATCH /api/v1/providers/{id}`
(see [the API](api-surface.md#providers-and-machines)).

A cluster on a home network usually has no address a controller in the cloud
can reach. Run `zoomies gateway --target <node-ip>:8006` on the node, or on any
machine beside it, choose **Private connection** in the form and paste the
address it prints. The endpoint stays `https://…:8006` and the certificate is
still verified end to end: the gateway forwards bytes and reads none of them.

The **VMID range** is worth setting deliberately. Zoomies allocates the lowest
free identifier inside it, so the range is both a budget and a blast radius: a
machine outside it is, by construction, not one of ours. Give Zoomies a block
nothing else uses (`9000–9099`, say) and keep it out of whatever your other
tooling allocates from.

A machine's shape is one shape per provider. If you want two sizes, make two
providers: a pool asks for the machine it fits, and having one row mean two
different machines makes the accounting ambiguous in exactly the place it has
to be exact.

## Proving it works: the test machine

**Check** asks Proxmox what it would refuse and changes nothing. It cannot tell
you that a machine really clones, boots, installs its agent and joins the fleet,
and those are the steps a first job would find broken. **Test machine**, on the
provider's page, builds one real machine and watches it go through each:

1. cloned from the template,
2. powered on,
3. agent installed,
4. enrolled as a host,
5. reported ready.

The machine is released as soon as it reports ready, so it is paid for for a few
minutes. A test that fails stops at the step that did not happen and shows what
the machine itself said; the machine is left in place so that can be read, and
the page offers the release. A test machine is the one machine you ask for by
hand, so it is held to the rules every machine is: it counts against the
provider's maximum and the fleet's, it is refused while renting is off or the
provider is paused, disabled or standing down, and only one runs at a time. It
never counts towards the warm minimum and is never drained for being idle.
Starting one needs the operator role and is audited as `provider.test_machine`.

## Which pools use a provider

A pool never names a provider, and a provider is not attached to a pool. Renting
is the answer to one situation: a pool has queued work and **no host can run
it**. Then the controller asks the providers that may rent for that pool to
build a machine, and the machine becomes an ordinary host.

Spending money is the reason both sides get a say, the way a pool's
`host_selector` and a host's labels both have to agree before a runner lands.
A machine is rented for a pool only when all three of these hold:

1. **The provider allows the pool.** Its **pool selector** (`--pool-selector`)
   is matched against the pool: a key with no value asks that the pool carries
   that label (`gpu`), `tier=large` asks for the label `tier=large`, and `name`
   and `backend` match the pool's own. Empty means every pool.
2. **The pool allows the provider.** Its **provider selector**
   (`--provider-selector` on `zoomies pools`, or **Providers** in the pool
   editor) is matched against the provider's `name`, its `kind` and its machine
   labels. Empty means any provider.
3. **The machine suits the pool.** The backend, operating system and
   architecture, the pool's host selector against the provider's machine
   labels, and the size are all checked against the machine the provider would
   build, before anything is paid for.

The size is judged the way placement judges a host: after the host's reserve,
cut into the provider's machine capacity, with a docker-in-docker runner's
slot big enough that the thinner of the runner and its daemon still gets the
pool's minimum. A daemon given 15% of a slot's memory under a 1 GB minimum
needs a slot of about 6.7 GB, so a 4 GB machine is too small for that pool
however many of them are rented.

When the **Machine shape** leaves CPUs and memory empty, each clone is the
template's size, and the controller judges it at the size it was last seen
to be: the template's, read by the connection check, and then what a machine
built from it measured itself as once its agent joined. Run the check after
changing the template, so the next machine is judged at the new size. A
provider whose size nothing has said yet is judged as a host that measured
nothing, which fits by slots alone; its first machine is how the size is
learnt.

A provider whose machines are too small for a pool rents nothing for it, and
says so with the size they would have to be: "Give proxmox-lab's machines at
least 2 CPUs and 8 GB of memory (set its machine size, or give the template it
clones more processors and memory), or lower this pool's minimum runner size."
It never suggests a higher maximum, since every machine that would buy is the
one that is too small.

Both selectors are empty by default, which is what every pool and provider
meant before they could say otherwise, so nothing changes until one is set.
Every entry has to be satisfied, and case does not matter, as
it does not for GitHub labels.

The selectors decide **what is bought**, not where a runner goes afterwards:
once the machine is a host, the pool's own `host_selector` is what keeps other
pools off it.

To see the answer for every pair, open a provider's **Pools** tab, or a pool's
**Providers** section. Each says which provider would rent for which pool and,
for the ones it would not, whose selector said so and what to change. The
Providers page's cards say whether any pool can use a provider at all, which is
the first thing to check when a new one rents nothing: a ceiling above zero is
not enough if no pool allows it. The same answer is `GET
/api/v1/providers/pairings`, and when a pool has work waiting and a selector is
the only thing in the way, the controller logs it once, naming the pool, the
setting and the fix.

Eli and any [MCP client](connect-claude.md#asking-about-providers-and-machines)
can read the same answers with `provider_pairings`, and the providers and
machines behind them with `list_providers` and `list_machines`. A viewer cannot
read `provider.enabled`, so `list_providers` carries `renting` as well: whether
renting is switched on, the most the fleet may rent, and for each pool with work
waiting what to change. `providers_available` alone is not the whole story, and
`provider.paused` is still worth a look when nothing there explains a pool that
buys nothing.

## Why is no machine rented?

A pool with work waiting and no machine coming for it looks the same on every
screen whatever the reason, and the reasons call for opposite things. The
controller keeps one verdict per pool from its last machine pass and shows that
same sentence everywhere, with the one thing to change. The verdict is kept even
while renting is switched off, so it can say what would have been rented.

| State | What it means | What to change |
| --- | --- | --- |
| `renting` | The pass is renting machines for this pool, all it is short of or as many as the ceilings allow. When a purchase is shared by several pools and only part of it is made, the sentence says the rest is held back. | Nothing, unless the sentence says the rest is held back, in which case raise the ceiling it names. |
| `on_its_way` | Machines already being built cover the pool, so the next runners start there. | Nothing; wait for them. |
| `waiting` | The scheduler could not place the pool for a reason a machine does not answer: a host that could run it is cordoned, silent, incompatible, held or still starting runners. A machine is rented only once every host that could run the pool is full. | Wait for those hosts to clear, or uncordon, repair or remove the ones that are unavailable. |
| `ceiling` | A limit stopped the purchase: the fleet's maximum or a provider's own. The sentence names which. | Raise the limit it names. |
| `unserved` | No provider will rent for this pool: a selector says no, or no provider builds a machine the pool could use. A machine too small for the pool is said as that, with the size it would have to be and the machines the provider already owns at that size. | Change the selector the sentence quotes, give the provider's machines the size the sentence names, or change the pool. |
| `held` | A provider may not buy at the moment, for the reason the sentence gives. | Clear the reason, for example resume a paused provider. |
| `off` | Renting is switched off, so nothing is rented. The sentence says what would have been rented, or that a machine being built is not being finished. | Turn on **Rent machines** in Settings, and set **Machines the fleet may rent** to the most you are willing to pay for. |

The verdict is shown on the Providers page, as a notice at the top and in each
provider's **Pools** tab; on the pool's page, as its capacity problem; in the job
drawer, in the explanation of why a job is waiting; and in the problems drawer,
as `provider.renting_off` while renting is switched off with work waiting. Eli
and any MCP client read it as the `renting` key of `list_providers`, and it is
`GET /api/v1/providers/renting`.

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

A ready machine that no enabled pool's runner fits on (too small, or the wrong
backend, platform or labels for every pool) is not kept for the next burst,
because nothing that arrives will run on it. It is released once the scale-down
cooldown has passed, without waiting out the idle timeout as well, so it does
not hold the provider's maximum while work is queued.

Two of those steps are the whole design and are worth stating plainly:

* **The row exists before the VM does.** Zoomies writes the machine's identity
  (which node, which VMID, which name) before it asks Proxmox for anything.
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
Drains, deletions, recovery and ownership checks all continue; a pause that
stopped those too would leave VMs running with nothing tending them. It is a
row, so it survives a restart, and it is audited.

**A machine that will not come up.** Its page carries the failure in the words
whatever refused it used: Proxmox's own task error for a clone, and the guest's
own standard error for a bootstrap. Three machines failing in a row stand the provider
down for a widening interval (a machine that keeps retrying counts once), so a bad template costs a few machines rather
than fifty.

**Clearing out failed machines.** A failed machine stays in the list until it is
deleted, and a bad template or address leaves several. On **Providers**, filter
to *Failed*, tick the box at the top and delete them together; the
[Providers page](ui.md#providers) says what the confirmation asks. A failed
machine that never got a VM costs nothing to delete. One that did has its VM
destroyed, so the dialog asks you to type how many machines you selected. The
same is `POST /api/v1/machines/bulk` in the [API](api-surface.md).

**Orphan review.** **Providers → your provider → Orphans** has three lists:

* *Untracked resources*: VMs wearing Zoomies' name or marks with no row behind
  them. Zoomies never deletes one. They are what a database restored from
  backup, or a row pruned too early, leaves behind, and they are real machines
  costing real money, so they are shown until somebody deals with them.
* *Machines with no resource*: rows whose VM has gone. Usually somebody deleted
  it in the Proxmox console.
* *Ownership unverified*: the quarantine. A row and a VM that disagree about
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
machines, because it might: two controllers restored from one backup would
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

Fixture tests prove the logic; they prove nothing about your cluster, and so
far nothing about anyone's. The procedure below is implemented as a harness,
`make test-e2e-proxmox`, that has not yet been run against a cluster, and
[the record it fills in](https://github.com/eyupio/zoomies/blob/main/roadmap/validation/proxmox-qualification.md)
says `not run` on every row. Before this is load-bearing for you, run it against
**disposable** resources and record what happened, and if you do, send the
rows it wrote, because a run on a real cluster is the evidence this page is
waiting for.

1. Twenty full cycles: create, enrol, run a real workflow job, drain, delete.
2. Scale from zero: no hosts at all, then queued work.
3. A burst across two pools at once.
4. A controller restart in the middle of a create.
5. A deliberately broken template, to see the bootstrap failure reported.
6. A deletion that fails once, to see it retried and confirmed.
7. Finally, reconcile the VM and storage inventory against the machines table.
   **No owned resource may be left unexplained.**

For a provider that builds [containers](#containers-experimental), also record,
on the same cluster:

8. The privileges a token held when it could create, configure, start, inspect
   and destroy an unprivileged container, and the ones **Check** asked for that
   it did not need.
9. Whether a real workflow job runs on the Docker backend inside the container
   with `nesting=1` alone, with the image and kernel it ran on, or whether it
   needed `keyctl`.
10. A claim from the wrong address (refused), a controller restart between the
    container starting and its agent claiming, and a template whose agent never
    claims (the machine fails with the enrolment timeout, and is deleted).
11. A VMID that a VM holds when a container is allocated one, and a container
    left behind by a failed create: nothing unexplained may remain.

Record the Proxmox version, the template, the limits, the timings and the
cleanup evidence. A cycle that worked without evidence is a cycle nobody else
can check.
