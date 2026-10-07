# Design brief: nautilus as an edge collector

**Status: decisions settled 2026-10-07; building, in the order of §11.** Written from argonaut,
as argonaut's `docs/design/nautilus-seam.md` was, and moved here when nautilus took it up.
Grounded in a read of nautilus `main` at the argonaut pin. Decision behind it (James,
2026-10-07): **tentacle is deprecated in favour of nautilus**, and the edge collector is where
that lands first.

## 0. The shape, in one paragraph

A collector is a nautilus project with field drivers, a `sparkplug:` block and no tasks. It
browses its devices, publishes a **selected** subset of their tags as Sparkplug B, buffers
to disk while nobody is listening, and runs as one box or as a redundant pair or trio. It is
a **mode of nautilus, not a derivative project**: argonaut had to be a derivative because its
input is a site manifest, not a nautilus project; a collector's input *is* a nautilus project,
and adding logic later is adding a task to the same project. What makes it a product is the
tooling around it: discovery and selection, Sparkplug organisation, durable store-and-forward,
redundancy, a management interface, and one-command install on a Linux box.

## 1. What exists, and what is missing

| Piece | nautilus today | Gap |
|---|---|---|
| Drivers | `eip` (Logix), `modbus` (TCP), `snmp`, `redfish`, `sparkplug-host`, `prom`; `io.Multi` combines them with disjoint tag ownership | OPC UA, BACnet, PLCnext (tentacle has all three; §8); Modbus RTU |
| Discovery | `naut eip import`/`browse` (Logix symbol + template browse → `eip/codegen`, with `selectSymbols` patterns); SNMP walk + profiles; `naut redfish`, `naut prometheus import` | one shape across drivers; a selection that survives a re-browse (§2) |
| Sparkplug edge node | NBIRTH/NDATA/NDEATH, RBE classes with deadbands, templates (UDTs birthed as Template definitions, dependencies first, partial-template commands merged), primary-host STATE gating, bdSeq file | `device:` is **one** string: all driver tags ride one Sparkplug device whose health gate is *all* drivers healthy, so one dead PLC kills the DBIRTH of every other (§3) |
| Store-and-forward | bounded ring, by record count, drained rate-limited, replayed as historical | **in memory only**: lost on restart or power cut, which at the edge is the common case (§4) |
| Commands | NCMD/DCMD write every named tag; "a driver may propagate to the field" | **no write gate**: any host on the broker can write through to a PLC (§6) |
| Redundancy | `leader` (k8s Lease `Elector`); `oplog` (the majority-replicated log argonaut upstreamed) | nothing for an edge node: two publishers with one edge node ID is split-brain on the wire (§5) |
| Packaging | `naut build` embeds a project in one binary; `naut new --template` | a collector template, an install, a management interface (§7) |

## 2. Discovery, selection, and the committed artifact

Auto-discovery runs into two rules already written down. argonaut's HANDOFF: discovery may
*enrich*, never *define*, because a missing device is not discoverable. nautilus's
`tags.md` §6: the tag set is a committed artifact, never decided at boot. Both rules are
about the **published** set, not the **available** set, so the collector keeps them apart.

### 2.1 Four files, in layers

| File | Written by | What it is |
|---|---|---|
| **browse snapshot** | the collector | what the device had at a time: names, types, UDT definitions. A record, not a decision. Committed so CI and an editor can work offline |
| **generated rules** | codegen (VS Code, scripts) | include and exclude patterns per device, by name, path, type and scope (`Program:Mixer.*`, `*_HMI` excluded, every instance of UDT `Motor`), renames, folder placement (§3), compact in tentacle's idiom (`tags.md` §2.3). Never hand-edited |
| **overrides** | the web interfaces only | a tag unticked, renamed, moved, or made writable. Never written by codegen |
| **the lock** | resolution | generated rules, then overrides, resolved against the snapshot: the exact list of published tags with type and address. Committed, like `go.sum`. **This is what runs** |

The layering is what lets codegen and a non-programmer share a device: regenerating never
clobbers an override, and an override never needs to understand the generator (Kustomize's
base and overlay).

### 2.2 Who edits where

The fact that decides it: **only the collector can reach the devices.** CI cannot browse a
PLC, nor can a laptop, nor argonaut on the IT side. So a browse always runs on the collector,
and every other surface works from its snapshot.

| Surface | Does |
|---|---|
| VS Code | codegen; schema completion through the nautilus LSP; preview against the snapshot |
| the collector's own web interface | live browse; override editor with a live preview; works with nothing but the box |
| argonaut | the same override editor across every collector: central management |
| CI | resolve against the snapshot; name collisions, Sparkplug validity, `naut check`, `naut test`. Never browses |

**Central management without reaching into the control network.** Both sides already connect
*out* to the broker, so the broker carries signals and git carries config:

1. argonaut requests a browse on a request/reply topic (plain MQTT, outside the Sparkplug
   namespace); the collector answers with the snapshot.
2. The edit becomes a commit to the overrides, or a PR where the site wants review.
3. argonaut signals "commit X is ready"; the collector pulls (outbound, as `naut pull` does
   now), resolves and applies.
4. The collector reports its running commit as a node metric, so drift is visible centrally.

Nothing connects into the OT side. Where OT cannot reach git, argonaut relays the commit
over the broker as a fallback.

### 2.3 When the device changes: `follow:`

Settled with James, 2026-10-07. The collector re-browses periodically and compares against
the lock. Example: the rule is `Program:Mixer.*`, the lock holds 40 tags, and someone adds
`Mixer.NewValve` in an online edit.

- **`follow: true`** (the `collector` template's default; James expects to use it most): the
  new tag publishes at once. Sparkplug declares every metric in the birth, so this is a
  **rebirth of that device only**. Running is now ahead of committed, so the collector
  reports the drift and proposes the lock change as a commit.
- **`follow: false`**: a notice, "1 new tag matches your rules, not published", with Accept
  on every surface that edits. Accepting is a lock commit; nothing changes on the wire before.

Never followed, whatever the setting, because they break consumers or hide a loss:

- **removed**: the tag goes Bad and alarms (absence is not a value);
- **retyped** (DINT to REAL): the birth's datatype would change; Bad until accepted;
- **a UDT gains or loses a member**: the Template definition changes and every instance
  rebirths; treated as a retype.

### 2.4 Discovery by protocol

The tooling says how good each browse is rather than hiding it: Logix and OPC UA browse
fully, with types; SNMP walks with profiles; Redfish walks; **Modbus has no discovery at
all**, so its "browse" is a device profile (a register map per make and model;
`devices.yaml` in `examples/remote-fleet/sites/booster-1` is the start of one).

## 3. Organising into Sparkplug

| Concern | Proposal |
|---|---|
| Topology | group = the site or line, edge node = the collector, **device = one per driver connection** (one PLC, one meter), each with its own DBIRTH/DDEATH from that driver's health. Replaces the single `device:` string |
| Naming | metric names from folder placement plus rename rules; the Sparkplug name is independent of the field address, so a PLC re-tag is a rule change, not a consumer change |
| Native UDTs | controller UDTs (Logix templates, OPC UA structured types) publish as Sparkplug Templates; already supported downstream of a struct tag |
| Composed UDTs | for flat sources (Modbus registers, SNMP OIDs), a mapping that assembles members into a declared UDT instance: four registers become one `Meter` with `KW`, `KWh`, `PF`, `Online`. The same `hw.Binding` shape argonaut's bindings use, so one vocabulary covers both |
| Properties | engineering units, ranges, descriptions and the per-tag RBE class carried as Sparkplug metric properties, so a host like Ignition gets them in the birth |
| Scaling | raw-to-engineering scaling and simple transforms on the binding; anything more is a task, which is the point of being nautilus |

## 4. Store-and-forward

Today's ring is right in shape and wrong in medium. Proposed:

- **on disk**, an append-only segment log, sized by **bytes and age** (keep 7 days or 2 GB,
  whichever first), oldest dropped first as now;
- survives restart and power loss; a torn last record is truncated on open, not fatal;
- replay keeps original timestamps, is marked historical as now, and is rate-limited as now;
- buffer depth, oldest record age and drain rate are status values the management interface
  shows and an alarm can watch.

## 5. Redundancy

Sparkplug defines host redundancy, not edge redundancy. An edge node ID must have **one**
live publisher, or hosts see interleaved `seq` and two bdSeqs. **Split brain, precisely, is
two publishers of one edge node ID both connected to the broker.** A collector cut off from
the broker cannot cause it, so the broker's view is the one that counts.

### 5.1 Hot/standby, one edge node ID (first)

Two collectors; one holds the role and publishes, the other stands by with its own device
connections and a short buffer, so on promotion it publishes the failover gap as historical.
(`standby: cold`, no device connections, for controllers with a tight CIP connection budget,
is a later option that accepts a gap.)

### 5.2 The third vote: a witness on the broker's host

Settled with James, 2026-10-07, subject to seeing it behave under test (§5.4).

Rejected:

- **the broker alone.** MQTT has no compare-and-swap. Reusing one client ID makes the
  broker kick the older session, so a returning ex-active kicks the new one and they flap;
  "take over on NDEATH" races when both see it. Right most of the time is not fencing.
- **argonaut.** It would put an IT monitoring application in the OT data path: argonaut
  down (with Ceph, say) means the active cannot renew, steps down, and data stops because
  monitoring failed. It needs OT-to-IT connections, and a collector must work at sites
  with no argonaut at all.

Chosen: **`naut witness`**, a small process holding no data, run on the broker's host. Three
voters (A, B, witness) by majority, on `oplog`'s consensus or the same rules:

- the witness alone dying is no outage: A and B are still a majority;
- its failures correlate with the broker's, which is wanted. If that host is down nobody can
  publish anyway; a collector that reaches the broker almost certainly reaches the witness,
  so the vote follows the path split brain needs.

In Kubernetes the k8s Lease (`leader.Elector`) can stand in for the vote; the rules below
are the same.

### 5.3 Fencing and handover

- The active renews every few seconds. **If it cannot renew, it publishes NDEATH and
  disconnects before its lease could be granted elsewhere** (step-down time shorter than
  lease duration; an MQTT 5 disconnect with the will, or an explicit NDEATH, since a clean
  disconnect does not fire the will). argonaut's "the minority goes read-only" rule.
- The standby takes the role only after the full lease duration plus a margin, and births
  with its own bdSeq. A host sees an ordinary death and rebirth.
- Durations only, measured locally; never wall-clock times compared across hosts.
- **Device health counts.** An active that has lost its controller while the standby still
  sees it hands over voluntarily, after a hold-off so a flapping link does not ping-pong.
- **A returning ex-active comes back as standby**, and never replays what it buffered while
  cut off under the edge node ID: that is split brain by the back door. It discards the
  buffer, or hands it to the active, which publishes it as historical.
- Each collector has its own MQTT client ID; the role, not the session, is the lock.
- Config travels with the role: every member runs the same commit (§2.2), and a member on a
  different commit refuses the role.

### 5.4 Proving it

James wants to see the behaviour as test results before trusting it, so the redundancy work
ships with a partition matrix run in virtual time, in nautilus's acceptance style: every
pair of {A, B, witness, broker, controller} cut and healed, in each order, asserting **at
most one birth-holder on the broker at every instant**, the gap published once and only
once, and no flapping. The failover timeline per case (detect, step down, take over,
rebirth) is part of the report.

### 5.5 Parallel, distinct edge node IDs (later)

Every collector publishes as itself (`Line1-A`, `Line1-B`); consumers merge. No election and
no split brain, but double the device load and the merge is every consumer's problem.
Ignition does not merge; argonaut can (the "vantage" idea in argonaut's `nautilus-seam.md` §5). For when
the two network paths are themselves what is being watched.

## 6. Read-only by default

A collector ignores NCMD/DCMD writes except Rebirth. Writes are opt-in per tag (an allowlist
in the rules, `writable:` on the binding), and a write to a tag that is not on it is logged
and refused. A collector that later gains logic keeps the same gate: commands reach tags,
and only tags that say so.

## 7. Packaging and the management interface

- `naut new --template collector`: asks for broker, group ID and edge node ID; writes a
  project with no tasks, durable store-and-forward on, writes off, `follow: true`.
- `naut collector add <driver> --host …`: browse, commit the snapshot, write starting rules,
  resolve the lock, add the driver and its Sparkplug device. `naut collector sync`: §2.
- **Install**: one binary from `naut build` plus a systemd unit, by one command; a container
  image for sites that prefer one. Headless by default; the interface below is optional.
- **Components**, published as a package beside `nautilus-hmi-3d`: device list with
  connection state; tag browser showing *available against selected* with live values;
  override editor with a live preview of the lock diff; Sparkplug tree (group, node, devices,
  templates) as a host will see it; publish status (broker, primary host STATE, `seq`,
  bdSeq, buffer depth and age); redundancy status (role, peers, last transition); running
  commit against the repo's. argonaut uses the same components for central management.
- **An example application** built from those components: the reference collector UI, the
  same role `examples/` plays for the PLC side. Edits made in it write the overrides file only
  (§2.1), so the committed artifact stays the truth (the pattern `online-edits` set for program changes).

## 8. tentacle parity, and Ignition

Deprecating tentacle means a collector covers what tentacle-based edges do. **Only OPC UA is
pressing** (James, 2026-10-07): it is the default protocol for anything not Rockwell, and it
is how argonaut watches **Ignition** until argonaut replaces it. Ignition's gateway runs an
OPC UA server, so a collector with an OPC UA client publishes Ignition's tags and its device
connection status as Sparkplug; the gateway's own health is an IT service on the Prometheus
side (a blackbox probe on its status endpoint; `/StatusPing` is believed to be it, to be
confirmed). One integration per domain holds with no special case.

| tentacle | nautilus | Note |
|---|---|---|
| ethernetip, modbus, snmp, mqtt | `eip`, `modbus`, `snmp`, sparkplug | parity, bar Modbus RTU |
| opcua | none | **the one to port now**: browse with types, subscriptions, structured types as Templates |
| bacnet, plcnext | none | when a site needs them |
| history | `hist` | check feature parity before retiring |
| gateway, orchestrator, web | §7 | the orchestrator's install and update flow is prior art for the install command |
| network, nftables | none | box network configuration; probably an OS concern, not nautilus |

## 9. Where argonaut meets it

argonaut's integration principle becomes **one integration per domain**: Prometheus for IT
equipment, Sparkplug B for industrial equipment. argonaut gains a `sparkplug` source kind:
group, edge node and device IDs as the identity labels a manifest matches on; DBIRTH/DDEATH
as `__Online`; bindings mapping metric names to UDT members; the `sparkplug-host` driver
under `io.Multi` beside `promdriver`. Nothing in argonaut polls a PLC.

## 10. Decisions

Settled with James, 2026-10-07:

1. **`follow:`** as §2.3: additions follow when on (the template default), removals,
   retypes and UDT changes never do.
2. **The third vote** is a witness on the broker's host (§5.2), confirmed by the partition
   matrix (§5.4) before it is trusted.
3. **Protocols:** OPC UA only, to start.
4. **Where things live:** nautilus and its modules: the management package beside `hmi` and
   `hmi-3d`, the example application in `examples/`.
5. **Redundancy:** hot/standby first; parallel later.

Open: none blocking. Details to settle in the building: the request/reply topic layout,
re-browse interval and change detection per protocol, lease and renew durations.

## 11. Order of work

Each step useful on its own, smallest and most unblocking first:

1. Per-driver Sparkplug devices and the write gate (§3, §6). Small; a collector is safe to
   deploy only after these.
2. Durable store-and-forward (§4).
3. The OPC UA driver, with browse (§8). Also argonaut's route to Ignition.
4. Snapshot, rules, overrides and lock for `eip` and OPC UA, with `naut collector add`,
   `sync` and `follow:` (§2); then Modbus profiles, SNMP, Redfish.
5. The `collector` template and the install (§7).
6. argonaut's `sparkplug` source kind (§9); the first end-to-end use.
7. Hot/standby with the witness, and the partition matrix (§5.1 to §5.4).
8. The component package and the example application (§7); then argonaut's central
   override editor over the broker (§2.2).
9. Parallel collectors (§5.5).
