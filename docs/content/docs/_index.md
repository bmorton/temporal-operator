---
title: Documentation
weight: 1
---

A Kubernetes operator that manages the full lifecycle of [Temporal](https://temporal.io)
clusters — persistence and schema management, mTLS, the UI, monitoring,
controlled version upgrades, and declarative namespaces, search attributes,
schedules, and client credentials — plus disposable dev servers for local
development and CI.

{{< cards >}}
  {{< card link="getting-started" title="Getting Started" subtitle="Zero to a running cluster." >}}
  {{< card link="installation" title="Installation" subtitle="Helm, OLM, or kustomize." >}}
  {{< card link="architecture" title="Architecture" subtitle="How the operator and CRDs fit together." >}}
  {{< card link="operations" title="Operations" subtitle="Status, conditions, day-2." >}}
  {{< card link="upgrades" title="Upgrades" subtitle="Moving between Temporal versions." >}}
  {{< card link="troubleshooting" title="Troubleshooting" subtitle="Common failures." >}}
  {{< card link="reference" title="CRD Reference" subtitle="The full API." >}}
{{< /cards >}}
