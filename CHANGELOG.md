# Changelog

## [0.8.0](https://github.com/bmorton/temporal-operator/compare/v0.7.0...v0.8.0) (2026-06-19)


### ⚠ BREAKING CHANGES

* **devserver:** TemporalDevServer.spec.version is now a Temporal server version (e.g. 1.31.1) mapped to the CLI image, not a raw temporalio/temporal tag. Use spec.image to pin a CLI image directly.

### Features

* **devserver:** accept a Temporal server version for TemporalDevServer ([#79](https://github.com/bmorton/temporal-operator/issues/79)) ([80d32b9](https://github.com/bmorton/temporal-operator/commit/80d32b960c46e5ce92bd364be3a924575e000774))
* **ui:** live-refresh pause/resume controls + layout polish ([#78](https://github.com/bmorton/temporal-operator/issues/78)) ([19f9a4f](https://github.com/bmorton/temporal-operator/commit/19f9a4f37c4a36f14097b341545d6be98a10cc72))

## [0.7.0](https://github.com/bmorton/temporal-operator/compare/v0.6.0...v0.7.0) (2026-06-19)


### Features

* **ui:** read-only operator UI (topology & state overview) ([#63](https://github.com/bmorton/temporal-operator/issues/63)) ([de0f61d](https://github.com/bmorton/temporal-operator/commit/de0f61dd997bcce25c7302a468f0dacadef61b51))

## [0.6.0](https://github.com/bmorton/temporal-operator/compare/v0.5.1...v0.6.0) (2026-06-18)


### Features

* add TemporalDevServer CRD for disposable dev servers ([#68](https://github.com/bmorton/temporal-operator/issues/68)) ([2d896a8](https://github.com/bmorton/temporal-operator/commit/2d896a85647de3e45072305f5afc95a0e98e9167))

## [0.5.1](https://github.com/bmorton/temporal-operator/compare/v0.5.0...v0.5.1) (2026-06-18)


### Bug Fixes

* **controller:** remove stranded finalizer when cluster deleted first ([#62](https://github.com/bmorton/temporal-operator/issues/62)) ([28f444c](https://github.com/bmorton/temporal-operator/commit/28f444c21db11a8be874a922750bfe4569d91d96))
* **temporal:** set RequestId on schedule create/update requests ([#66](https://github.com/bmorton/temporal-operator/issues/66)) ([7550264](https://github.com/bmorton/temporal-operator/commit/7550264b2ad4af37bf9f24def66e68eee31ace4d))

## [0.5.0](https://github.com/bmorton/temporal-operator/compare/v0.4.0...v0.5.0) (2026-06-18)


### Features

* add TemporalSchedule CRD ([#57](https://github.com/bmorton/temporal-operator/issues/57)) ([1ea4778](https://github.com/bmorton/temporal-operator/commit/1ea47784319c6161cae4fa1de31ef72da5b623bb))

## [0.4.0](https://github.com/bmorton/temporal-operator/compare/v0.3.0...v0.4.0) (2026-06-17)


### Features

* Azure-friendly operator (phase 1) — podTemplate overrides, examples, docs ([#48](https://github.com/bmorton/temporal-operator/issues/48)) ([b5b16e4](https://github.com/bmorton/temporal-operator/commit/b5b16e4dfc0de233c975db53c39d29282b354377))
* in-browser resource preview tool (WebAssembly) ([#50](https://github.com/bmorton/temporal-operator/issues/50)) ([3944eb8](https://github.com/bmorton/temporal-operator/commit/3944eb83ab7bd25c58df28a107947e5ae43ce053))
* **preview:** redesign the resource preview page ([#52](https://github.com/bmorton/temporal-operator/issues/52)) ([dc5f4c5](https://github.com/bmorton/temporal-operator/commit/dc5f4c5c38d87e8fd0bc39b93562a4dc8bb2ba69))


### Bug Fixes

* **preview:** re-highlight YAML on subsequent renders ([#55](https://github.com/bmorton/temporal-operator/issues/55)) ([c18460c](https://github.com/bmorton/temporal-operator/commit/c18460caa1898231990a33cbc8ca6dbb6da3a383))

## [0.3.0](https://github.com/bmorton/temporal-operator/compare/v0.2.1...v0.3.0) (2026-06-16)


### Features

* **e2e:** add nsc-based Chainsaw e2e runner ([#46](https://github.com/bmorton/temporal-operator/issues/46)) ([45c57c7](https://github.com/bmorton/temporal-operator/commit/45c57c7a07362423c51093a0e529d608826b7e21))


### Bug Fixes

* **e2e:** wait for create-visibility-db Job before deploying cluster ([#35](https://github.com/bmorton/temporal-operator/issues/35)) ([4937e7e](https://github.com/bmorton/temporal-operator/commit/4937e7ef77d62d9edab82c43b2eff078548e053e))
* **mtls:** make mTLS clusters healthy and operator controllers mTLS-aware ([#45](https://github.com/bmorton/temporal-operator/issues/45)) ([c7121c2](https://github.com/bmorton/temporal-operator/commit/c7121c2afe69c106ae62a115c95c8e9d72c6a4a6))
* pin Temporal versions to published server/admin-tools/ui images ([e8b0207](https://github.com/bmorton/temporal-operator/commit/e8b0207233549250fc000fed92f133d9953b4972))
* worker never becomes Ready (gRPC probe on non-serving worker) + upgrade diagnostics ([#37](https://github.com/bmorton/temporal-operator/issues/37)) ([2e5110a](https://github.com/bmorton/temporal-operator/commit/2e5110ab57db9e91fefd8a48e37061b881a16640))

## [0.2.1](https://github.com/bmorton/temporal-operator/compare/v0.2.0...v0.2.1) (2026-06-15)


### Bug Fixes

* **e2e:** qualify Postgres host across suites and harden nightly upgrade jobs ([cb98a31](https://github.com/bmorton/temporal-operator/commit/cb98a311fd5df8bbc44907ce07ed723070037615))
* **e2e:** qualify Postgres host with test namespace across remaining suites ([53c78bc](https://github.com/bmorton/temporal-operator/commit/53c78bc9523c545ecadb6585ef3ab07ea66d8d3e))

## [0.2.0](https://github.com/bmorton/temporal-operator/compare/v0.1.0...v0.2.0) (2026-06-15)


### Features

* **chart:** default operator image to the published GHCR image ([9c64499](https://github.com/bmorton/temporal-operator/commit/9c64499982caacbb01fb64cbc8d501379a32076c))
* **chart:** default operator image to the published GHCR image ([932ecf9](https://github.com/bmorton/temporal-operator/commit/932ecf93c22e59fb52da2b86c9e1b6ef2f8faf04))
* **docs:** publish documentation to GitHub Pages with Hugo ([1cf16a1](https://github.com/bmorton/temporal-operator/commit/1cf16a15165de98d72dc29ebfa08f0a3c085b8fd))

## 0.1.0 (2026-06-14)


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


### Miscellaneous Chores

* release 0.1.0 ([1ad3491](https://github.com/bmorton/temporal-operator/commit/1ad3491633058bb835cc45afcfffd4ac4ec86997))
