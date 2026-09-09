# scribe

Give it a link to a video or podcast. Get back searchable text.

Built to learn how to run software properly — not because transcription is new.
The interesting part is everything around it: queues, retries, monitoring,
backups, and recovering when it breaks.

**Status:** planning. Nothing runs yet.

## Documents

| | |
|---|---|
| [PROJECT.md](PROJECT.md) | what it is, how it works, what it will not be |
| `BACKLOG.md` | the work, broken into stories *(next)* |
| `docs/adr/` | decisions and why *(next)* |

## Where it runs

A three-node Talos Kubernetes cluster on one Hetzner machine. Deployed by
Argo CD from Git. Reachable only over Tailscale — nothing is public.
