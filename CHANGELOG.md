# Changelog

## 1.0.0 (2026-06-14)


### Features

* **api:** define full v1alpha1 CRD schema and satellite CRDs ([23af968](https://github.com/bmorton/temporal-operator/commit/23af9686e520b52c4b77a1960b2827a5d7a539b6))
* **controller:** implement no-op TemporalCluster reconciler ([8ec5ac0](https://github.com/bmorton/temporal-operator/commit/8ec5ac0a588c702e855dd7b331ada40dce3a927e))
* **dist:** add Helm chart, OLM bundle, and kustomize installer ([2018072](https://github.com/bmorton/temporal-operator/commit/201807230cc2862d418ce4c87c9dc884f9403194))
* **mtls:** cert-manager mTLS and TemporalClusterClient credentials ([b716f69](https://github.com/bmorton/temporal-operator/commit/b716f69904f78725a6ddd32e846e62c608f565e9))
* **namespace:** reconcile TemporalNamespace against the cluster ([beac37f](https://github.com/bmorton/temporal-operator/commit/beac37fd3aaca8ee072518e5020258f8e0a1d61f))
* **persistence:** add Cassandra and Elasticsearch backends ([faf3a5f](https://github.com/bmorton/temporal-operator/commit/faf3a5ff8dedd5218b2c6dd088acc1a154a9b64c))
* **persistence:** reconcile Postgres reachability and schema ([85869ef](https://github.com/bmorton/temporal-operator/commit/85869effb937c01831c066078d01166c9aa9d3de))
* scaffold operator project with kubebuilder ([42c4c0e](https://github.com/bmorton/temporal-operator/commit/42c4c0e622f81bc5565bec92695512c3b861114e))
* **searchattribute:** reconcile TemporalSearchAttribute registration ([915e9f3](https://github.com/bmorton/temporal-operator/commit/915e9f3e81189f8b7c1d938fe8e811095b4be80c))
* **services:** deploy Temporal services with rollout and status rollup ([922a21b](https://github.com/bmorton/temporal-operator/commit/922a21b73d05cf33612b88ef0aef016ad22641cd))
* **temporal:** add version matrix generator and config-template engine ([6cb462e](https://github.com/bmorton/temporal-operator/commit/6cb462ee7b352d1f8081238740769772be4b2cf4))
* **ui:** add temporal-ui, ServiceMonitor monitoring, and Grafana dashboard ([267a316](https://github.com/bmorton/temporal-operator/commit/267a31656e18d02ddec7ba76b01959bb5888c8f8))
* **upgrade:** orchestrate ordered version upgrades ([f81e7b1](https://github.com/bmorton/temporal-operator/commit/f81e7b14e214ac763b3a7076b41a1e0ca045a69b))
* **webhook:** add admission webhooks and version matrix ([6207411](https://github.com/bmorton/temporal-operator/commit/62074110713c60f5804a3c8b23b2c74155b4e0af))


### Bug Fixes

* inject POD_IP broadcastAddress for Temporal 1.31+ membership ([bd684f5](https://github.com/bmorton/temporal-operator/commit/bd684f5922719a15517ae658676dd1f462b16eda))
