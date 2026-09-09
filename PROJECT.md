# scribe

> **Working name.** It ends up in every hostname, image tag, dashboard and
> alert — renaming is free today and annoying in a month. Change it now if you
> want something else.

---

## In plain English

### The problem

Video and audio are terrible formats for information you want to keep. You
cannot search them, cannot quote them, and cannot find the part where someone
explained the thing you now half-remember. So you either watch it again or you
lose it.

### What it does

You give it a link. Later, the text is in your archive — searchable, permanent,
yours.

That is the entire product. Paste a URL, walk away, come back to text.

### How it works, in ordinary words

**The front desk** takes your link, writes down that there is work to do, and
immediately says "got it". It does not wait, because the work takes twenty
minutes and you should not be staring at a spinner.

**The list** is a queue of pending jobs. Deliberately boring. If everything
crashes, the list is still there afterwards.

**The workers** take one job at a time: download the audio, run it through the
transcription model, save the text. If a worker dies halfway through, the job
returns to the list and another worker picks it up.

**The archive** splits in two: the *text* goes in a database because you want
to search it; the *audio* goes in object storage because it is hundreds of
megabytes and databases are bad at that.

Front desk, list, workers, archive. That is the whole system.

### Why this project, and not a different one

It is a vehicle for operational practice, chosen for one property:

> **The work takes longer than a request.**

That single fact is where queues, retries, idempotency, backpressure,
autoscaling, and "did it finish or did it die?" all come from — and it is
exactly what most tutorial projects, which answer in milliseconds, never teach.

The workers also idle for hours between jobs, which makes scale-to-zero
obviously correct rather than a contrived exercise.

### What it will not be

Naming these now, because scope creep is what kills projects like this.

| Not | Why |
|---|---|
| Multi-user | one person, no tenancy, no quotas — until deliberately added later |
| Real-time | it is minutes-to-hours by nature, and that is the point |
| A polished UI | JSON API first; a UI only once the pipeline is boring |
| Accurate enough to quote | ~90% is fine for search, frustrating for citation |
| Public | tailnet only, forever |

---

## Architecture

```
   you ──▶ api ──▶ postgres (jobs, transcripts, FTS)
                      │
                      │  queue = a table, not a broker
                      ▼
              worker ──▶ yt-dlp ──▶ ffmpeg ──▶ whisper
                      │
                      └──▶ rustfs (audio, artifacts)

           reaper ──▶ reclaims jobs whose lease expired
```

### Services

| Service | Shape | Responsibility |
|---|---|---|
| `api` | stateless, many replicas | accept submissions, report status, search transcripts |
| `worker` | stateful-ish, scales 0→N | lease a job, download, transcribe, store |
| `reaper` | singleton, periodic | requeue jobs whose worker died; clean up abandoned media |

**Three, not eight.** The split points are real: the api is IO-bound and needs
low latency; the worker is CPU-bound and needs minutes; the reaper must not run
twice. Anything finer would be services invented to look impressive.

### Data model (sketch)

```
jobs          id · url · state · attempts · lease_until · error
              created_at · started_at · finished_at
transcripts   job_id · text · tsv (full-text) · language · duration_s · model
media         job_id · object_key · bytes · content_type
subscriptions url · kind · poll_interval · last_polled_at      (v0.6)
```

**States:** `queued → leased → done | failed | dead`

`dead` exists on purpose. A job that has failed five times is a **poison
message**, and retrying it forever is how a queue stops making progress. It
goes to `dead` and waits for a human.

### The queue is a Postgres table

`SELECT ... FOR UPDATE SKIP LOCKED` — roughly thirty lines. No Redis, no NATS,
no RabbitMQ.

For a queue measured in dozens of jobs, a broker is a second stateful system to
run, back up and reason about, in exchange for throughput that is not needed.
It also means a job and its state change commit in **one transaction**, which
removes a whole class of "the job ran but the row says queued" bug.

Revisit if throughput ever justifies it. It will not.

### Leases, not locks

A worker claims a job by setting `lease_until = now() + 30m` and heartbeats
while working. The reaper requeues anything whose lease has expired.

This is the answer to *"the worker was killed mid-job"* — which will happen
constantly, because that is what a rolling deploy is.

---

## SLOs

Only one of these is interesting.

| SLI | Target | Why |
|---|---|---|
| API availability (non-5xx) | 99.5% | table stakes |
| API latency p99 | < 200 ms | it only writes a row |
| **Time-to-transcript p95** | **< 30 min for media under 1 h** | ← **the real SLO** |
| Job success rate | > 95% | excluding `dead` |

**Time-to-transcript spans everything** — the api, the queue, the worker, the
model, storage. Burn it and you must work out *which stage* is slow. That
investigation is the skill; the other three rows are decoration.

---

## Failure modes expected, and wanted

These are not risks to avoid. They are the curriculum, and each becomes a chaos
experiment later.

| Failure | What it teaches |
|---|---|
| Worker killed mid-job | leases, idempotency, resumption |
| Three-hour video OOMs the worker | resource limits, pre-flight duration checks |
| Liveness probe kills a slow-but-healthy worker | probes vs long-running work — the classic mistake |
| Ten submissions at once | queue depth, backpressure, autoscaling |
| Object storage fills | capacity alerting, retention |
| Postgres fails over mid-write | CNPG failover, transaction semantics |
| `yt-dlp` breaks on a site change | external dependency drift, and why version pinning cuts both ways |
| Same URL submitted twice | idempotency keys |
| A cold start pulls a 2 GB image | image size, node warmth, scale-to-zero's real cost |

---

## Roadmap

Each step adds one thing, and each tool arrives because the previous step
created a need for it.

| | What | What it forces |
|---|---|---|
| **v0.1** | api + postgres. Accepts a URL, stores a row, returns it. **No transcription.** | the delivery loop: commit → build → Argo → running |
| **v0.2** | worker + queue + leases. Downloads audio only. | a StorageClass, because something must persist |
| **v0.3** | actual transcription | long jobs, probe tuning, OOM, resource limits |
| **v0.4** | RustFS for media | object storage, because media has nowhere else to live |
| **v0.5** | Prometheus, Grafana, the first SLO | measurement before optimisation |
| **v0.6** | KEDA scale-to-zero + subscriptions (auto-submit from a channel) | event-driven scaling; the reaper gains a second job |
| **v0.7** | chaos: kill a worker mid-job · CNPG PITR · Velero restore | recovery as a tested property, not a hope |
| **v0.8** | a UI, if the pipeline is boring by then | — |

**v0.1 is an afternoon.** One binary, one Dockerfile, three manifests.

---

## Constraints, decided up front

| | |
|---|---|
| **Language** | Go — one static binary, `FROM scratch` possible, good Prometheus client |
| **Transcription** | runs locally on CPU. Slow, free, private. |
| **Worker CPU** | capped, with a low `PriorityClass`. It shares six cores with the whole cluster and must not starve the control plane. |
| **Worker replicas** | 1 until v0.6. Concurrency is a later problem. |
| **Image budget** | api < 30 MB · worker < 2 GB (model weights dominate) |
| **Exposure** | Tailscale operator only. No domain, no ingress, no public anything. |

---

## Decisions to record as ADRs

| ADR | Question |
|---|---|
| 0001 | Three services, not eight |
| 0002 | Postgres-backed queue, not Redis or NATS |
| 0003 | Run the model locally, or call a hosted API |
| 0004 | Leases with a reaper, rather than a broker's ack semantics |
| 0005 | Go, not Python — despite the ML ecosystem being Python |

ADR-0003 is the one worth arguing with yourself about: a hosted API makes the
worker IO-bound and adds a real per-job cost, which teaches quotas and budget
alerts instead of OOM and CPU pressure. Different lessons, both legitimate.
