# scribe

> **This is a learning project.** The app is small on purpose. The point is
> everything around it: how it gets built, deployed, watched, broken, and
> recovered.

Give it a link to a video or podcast. Get back searchable text.

Transcription is not the interesting part — Whisper already exists and is not
written here. The interesting part is running a service properly.

---

## Why this app

It was chosen for one reason:

> **The work takes longer than a request.**

A transcript takes about twenty minutes. You cannot make someone wait for that,
so you need a queue. Once you have a queue you need retries, and jobs that
survive a worker being killed, and a way to know if something is stuck or just
slow, and a way to add workers when the queue grows.

That is where most real operations problems live. Apps that answer in
milliseconds never teach any of it.

---

## What it is used to practise

Each one is added when something breaks, not because it is on a list.

| Area | Practised by |
|---|---|
| **Containers** | hand-written multi-stage Dockerfiles, size limits, no root, scanned for CVEs |
| **CI** | build, test, scan, sign, push. CI never deploys. |
| **GitOps** | Argo CD deploys from Git. Nothing is applied by hand. |
| **Databases** | CloudNativePG. Backups, failover, and point-in-time restore that is actually tested. |
| **Object storage** | RustFS, because audio files have nowhere else to live |
| **Storage** | Longhorn, so a pod can move to another node and still find its data |
| **Autoscaling** | KEDA scale-to-zero. Workers idle for hours, so turning them off is obviously right. |
| **Monitoring** | Prometheus, Grafana, Loki. One SLO that measures what a user feels. |
| **Failure drills** | kill a worker mid-job · fill the disk · fail the database over · restore from backup |
| **Policy** | Kyverno. No root, no `latest` tags, limits required. |

---

## Where it runs

```
Hetzner dedicated server
  └─ Proxmox
      └─ 3 × Talos VMs  →  Kubernetes + Cilium
          └─ Argo CD  →  this app
```

All of it is built from Git. The machine, the cluster, and the app.

Reachable only over Tailscale. Nothing is exposed to the internet.

The platform is documented separately in
[workstation-architecture](https://github.com/nazmur96/workstation-architecture)
and [infrastructure](https://github.com/nazmur96/infrastructure).

---

## The one SLO that matters

> **95% of submitted media is transcribed within 30 minutes.**

It covers the whole path — the API, the queue, the worker, the model, storage.
When it breaks, you have to find out *which part* is slow. That is the skill.

---

## Status

**Planning.** Nothing runs yet.

| | |
|---|---|
| [PROJECT.md](PROJECT.md) | what it is, how it works, what it will not be |
| `BACKLOG.md` | the work as small stories *(next)* |
| `docs/adr/` | decisions, and why *(next)* |
