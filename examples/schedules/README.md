# Declarative Temporal schedules

Four `TemporalSchedule` resources that exercise the different trigger styles and
options the CRD supports. Each schedule is its own file; they share a cluster and
namespace defined in [`00-shared.yaml`](./00-shared.yaml).

| File | Trigger | Highlights |
| --- | --- | --- |
| [`00-shared.yaml`](./00-shared.yaml) | — | Shared `TemporalCluster` (`app`) + `TemporalNamespace` (`scheduling`). Apply first. |
| [`nightly-report.yaml`](./nightly-report.yaml) | Cron (`0 2 * * *`, NY time) | `args`, `retryPolicy`, `memo`, `overlapPolicy: Skip`, `pauseOnFailure` |
| [`healthcheck-every-15m.yaml`](./healthcheck-every-15m.yaml) | Interval (`every: 15m`, `offset: 2m`) | `jitter` |
| [`business-hours-sync.yaml`](./business-hours-sync.yaml) | Structured calendar | weekdays 09:00–17:00 hourly, no cron syntax |
| [`maintenance-window.yaml`](./maintenance-window.yaml) | Cron (Sundays 03:00) | starts `paused: true` |

All four schedules set `allowDeletion: true`, so deleting the CR also deletes the
schedule in Temporal. Set it to `false` (the default) to leave the schedule in
place when the CR is removed.

## Apply

Apply the shared cluster + namespace first, then any subset of schedules:

```sh
kubectl apply -f 00-shared.yaml
kubectl apply -f nightly-report.yaml

# ...or apply everything in the directory at once (00-shared sorts first):
kubectl apply -f .

kubectl get temporalschedules        # short name: tsch
kubectl describe tsch nightly-report
```

The printed `READY` column reflects the `Ready` condition; `PAUSED` reflects the
live schedule state.

## Prerequisites for workflows to actually run

Creating a `TemporalSchedule` registers the schedule with Temporal, but a fired
action only makes progress when:

1. The referenced `TemporalCluster` is `Ready`.
2. The target Temporal namespace exists. The operator does **not** auto-create
   namespaces, so `00-shared.yaml` registers one (`scheduling`) via a
   `TemporalNamespace`.
3. A worker is polling the schedule's `taskQueue` for the named `workflowType`.
   Without a worker the schedules still appear in the Temporal UI and
   `temporal schedule list`; they just won't execute.

To reuse an existing cluster/namespace, skip `00-shared.yaml` and update
`clusterRef.name` / `namespace` in each schedule file to match your environment.

## Try it

```sh
# Pause / resume in place by editing state.paused, then re-apply:
kubectl patch tsch healthcheck-every-15m --type merge \
  -p '{"spec":{"state":{"paused":true}}}'

# Inspect from inside the cluster with the Temporal CLI:
temporal schedule list  --namespace scheduling --address <frontend>:7233
temporal schedule describe --schedule-id nightly-report --namespace scheduling
```
