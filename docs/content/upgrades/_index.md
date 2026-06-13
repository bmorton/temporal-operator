# Upgrades

Upgrade a cluster by changing `spec.version` to an adjacent supported version
(same minor patch bump, or the next minor). The admission webhook rejects
non-adjacent jumps with `UpgradePathInvalid`.

The operator runs an ordered state machine, visible in `status.upgrade.phase`:

```text
PreflightChecks -> SchemaMigrating -> RollingFrontend -> RollingHistory ->
RollingMatching -> RollingInternalFrontend? -> RollingWorker -> PostUpgrade -> Complete
```

- Schema migrations run before any service is rolled.
- Services adopt the new image one role at a time; each phase advances only once
  the previous role is fully rolled out.
- `status.upgrade.rollbackable` is `true` until schema migration begins.

`status.version` reflects the running version and only advances to the target
once the rollout completes.
