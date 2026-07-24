+++
title = "Resource Preview"
weight = 10
+++

The Resource Preview tool runs the operator's own object planner — compiled to
WebAssembly — entirely in your browser. Paste a `TemporalCluster` custom
resource and it shows every Kubernetes object the operator would create, grouped
into collapsible cards by kind, after applying the same defaulting and validation
the operator's admission webhooks perform.

Because the tool is built from the operator's source on every docs deploy, the
preview stays in lockstep with the operator and never drifts.

{{< button href="/preview/" >}}Open Resource Preview{{< /button >}}

> **Alpha.** The tool currently supports `TemporalCluster`. It uses placeholder
> credentials when rendering configuration, so secret values shown are not real.
