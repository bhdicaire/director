# Workspace mode: route events to the repo they concern

**Date:** 2026-10-03

**Status:** proposed, not implemented. Open questions at the end.

## Problem: one session, many repos, one log

Director keys every log by the git repository of the session's working
directory. `identity.RepoKey` runs `git rev-parse --show-toplevel`, derives the
key once from the `upstream` remote, else `origin`, else the first remote, and
persists it in `<toplevel>/.director/repo-key`. A workstream is one repo plus
one branch. Every `emit`, `render` and hook resolves from there.

That model assumes a session works in one repository. Real sessions often
don't: an operator coordinating infrastructure works across a control-plane
repo, an infrastructure repo, a DNS repo, dotfiles and a wiki in one sitting.
Dogfooding on 2026-09-30 to 2026-10-03 showed three failures:

- **Misfiling.** Sessions started in a website repo (`bhdicaire-com`) did most
  of their work in other repositories. An audit found about 70 of 86 events in
  that log belonged elsewhere. Re-filing took a manual pass: 21 open items
  (16 re-emitted in five other logs, 5 closed as done) and 31 decisions
  re-emitted and superseded with one pointer decision
- **No home.** A session started in the folder that holds the repositories
  (`/Volumes/Tarmac/d5e`, not itself a repository) gets
  `render: git rev-parse --show-toplevel: exit status 128`. No digest is
  injected at session start, and `emit` has nowhere to write
- **Nothing checks.** The stop hook's emit-guard asks whether a decision or
  open loop was recorded. It cannot tell whether it was recorded in the right
  log

The stopgap is an instruction file (`CLAUDE.md` / `AGENTS.md` at the workspace
root) telling agents to `cd <repo> && director emit ...`. It works only as
well as the agent's discipline, and the guard cannot verify it.

## Decision (proposed)

Introduce a **workspace**: a declared set of member repositories. Inside a
workspace, each event is routed to the member it concerns, wherever the
session started. A session outside any workspace behaves exactly as today.

### 1. Workspace manifest

```yaml
# <workspace root>/.director-workspace.yaml  (location: see open questions)
name: d5e
default: ecosystem            # where cross-cutting decisions land
members:
  infra:
    path: infra               # relative to the workspace root, or absolute
    key: git-ssh.d5e.dev-d5e-infra
    areas: [infra/*, maestro, netbox, 1password]
  ecosystem:
    path: ecosystem
    key: git-ssh.d5e.dev-d5e-ecosystem
    areas: [ecosystem/*, director, governance]
  dns:
    path: dns
    key: git-ssh.d5e.dev-d5e-dns
    areas: [dns/*]
  sabrina:                    # a member outside the root folder
    path: /Volumes/Tarmac/web/sabrina-dicaire.com
    areas: [sabrina/*]
```

- `key` pins the repo-key. A remote rename or a new remote then cannot split a
  member's history. Dogfooding hit this twice (`dnsControl` renamed to `dns`,
  and a repo first keyed from its GitHub mirror, then from Forgejo). When `key`
  is omitted, Director derives it as today and writes it back
- `areas` declares which event areas a member owns. Globs are allowed

### 2. Routing an emit

`director emit` resolves the target member in this order:

1. `--repo <member>`, explicit
2. the event's `--area`, matched against members' `areas`; exactly one match
3. the member that contains the working directory
4. otherwise **refuse** with an error listing the members. Never guess

When the area belongs to a member other than the one containing the working
directory, emit warns and names the owning member. That warning alone would
have prevented the misfiling above. `resolve`, `show` and `open-items` accept
`--repo` the same way.

### 3. Session start at the workspace root

The start hook detects the manifest. Instead of failing, it injects a compact
summary with one block per member: open items, items needing the human,
escalations, the latest handoff. A member's full digest loads on demand
(`director render --repo infra`), or automatically once the session works
inside that member. Per-member summaries keep the injection inside the
existing budget even when a member has dozens of open items.

### 4. A routing-aware stop guard

At the end of a turn, the guard lists the members whose working tree or `HEAD`
changed during the turn and compares them with the members that received
events. A changed member with no event gets a pointed reminder: "infra
changed; nothing was recorded in infra's log". This compares what the session
did with what it recorded, which is the enforcement the instruction file
cannot give.

### 5. Cross-member events

`director emit --also dns,wiki ...` writes the event to its target member and a
short pointer note, citing the event's ULID, in each listed member. The
dogfooding convention ("decision in the control plane, pointer notes where it
constrains") becomes one command.

### 6. Moving events

```text
director move <ulid> --to <member>
```

It re-emits the event in the target member's log, keeping type, area, risk and
body, and adds provenance: the source ULID, source key and original timestamp.
Then it retires the original: it resolves an open item, and supersedes a
decision with a pointer decision via `--refs`. It refuses handoffs, because a
resume point belongs to its workstream. This replaces the manual re-filing
procedure used on 2026-10-03.

### 7. Hub persistence

When the hub (`~/.director`) is a git repository, the session-end hook commits
`projects/` and pushes to every configured remote. Recommend a second remote
outside the infrastructure the logs describe, so the record survives the
failure it documents.

## Minimum viable version

Parts 1, 2 and 6. Misfiling becomes an error instead of a silent default, and
cleaning up is one command. Parts 3 and 4 add comfort and enforcement. Parts 5
and 7 are refinements.

## What does not change

- The event schema and its four kinds, the fold, and the log format
- Single-repo sessions with no manifest: identical behaviour and output
- Repo-keys already persisted in `.director/repo-key`

## Open questions

1. **Where the manifest lives.** In the workspace root is simple and visible,
   but the root is often not versioned (in dogfooding it is a plain folder).
   In the hub (`~/.director/workspaces/<name>.yaml`) it is versioned and
   travels with the logs, but is further from the repositories. Or in a member
   repo (the control plane), symlinked to the root
2. **How much to infer from areas.** Area routing assumes consistent area
   names; dogfooding logs mix `d5e/infra`, `infra` and `maestro` for one
   member. Should each member declare its area vocabulary in its CHARTER, with
   emit rejecting unknown areas?
3. **Workstreams across members.** A workstream is one repo plus one branch.
   Does a workspace session get one workstream per member it touches (handoffs
   stay per repo), or one workspace-level workstream (one resume point, but a
   new identity kind)?
4. **Members outside the root.** Absolute paths (above), or nested workspaces
   (`web`, `projects`) referenced from a parent manifest?
5. **Strictness.** Should the guard in part 4 warn, or block the turn until the
   changed member has an event?
