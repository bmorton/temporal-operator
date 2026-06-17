#!/usr/bin/env bash
# Builds the WebAssembly resource-preview tool and stages its static assets
# under docs/static/preview. Generated files are git-ignored and rebuilt on
# every docs deploy, so the tool can never drift from the operator's code.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
out_dir="${repo_root}/docs/static/preview"
examples_dir="${out_dir}/examples"

mkdir -p "${examples_dir}"

echo "Building preview.wasm..."
GOOS=js GOARCH=wasm go build -ldflags="-s -w" \
  -o "${out_dir}/temporal-operator-preview.wasm" \
  "${repo_root}/cmd/preview-wasm"

echo "Copying wasm_exec.js..."
cp "$(go env GOROOT)/lib/wasm/wasm_exec.js" "${out_dir}/wasm_exec.js"

echo "Writing cache-busting hash..."
data_dir="${repo_root}/docs/data"
mkdir -p "${data_dir}"
wasm_hash="$(sha256sum "${out_dir}/temporal-operator-preview.wasm" | cut -c1-16)"
printf '{"wasmHash":"%s"}\n' "${wasm_hash}" > "${data_dir}/preview.json"

echo "Staging example TemporalCluster manifests..."
{
  printf '['
  first=true
  while IFS= read -r file; do
    rel="${file#"${repo_root}/examples/"}"
    name="${rel%.yaml}"
    name="${name//\//-}"
    cp "${file}" "${examples_dir}/${name}.yaml"
    if [ "${first}" = true ]; then first=false; else printf ','; fi
    printf '{"name":"%s","file":"examples/%s.yaml"}' "${name}" "${name}"
  done < <(grep -rl --include='*.yaml' '^kind: TemporalCluster' "${repo_root}/examples" | sort)
  printf ']'
} > "${examples_dir}/index.json"

echo "Preview assets written to ${out_dir}"
