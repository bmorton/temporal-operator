#!/usr/bin/env bash
# Generates Hugo content pages for each example under examples/ into
# docs/content/examples/ (git-ignored). Rebuilt on every docs deploy so the
# published examples can never drift from the examples/ directory.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
examples_root="${repo_root}/examples"
out_dir="${repo_root}/docs/content/docs/examples"

# Start clean so output is deterministic and deleted examples disappear.
rm -rf "${out_dir}"
mkdir -p "${out_dir}"

index="${out_dir}/_index.md"
{
  printf '+++\n'
  printf 'title = "Examples"\n'
  printf 'weight = 75\n'
  printf '+++\n\n'
  printf 'Curated `TemporalCluster` (and related) custom resources for common\n'
  printf 'scenarios. Each page renders the example README and its manifests.\n\n'
  printf 'These pages are generated from the\n'
  printf '[`examples/`](https://github.com/bmorton/temporal-operator/tree/main/examples)\n'
  printf 'directory; edit the examples there, not these pages.\n\n'
  printf '| Example | Manifests |\n'
  printf '| --- | --- |\n'
} > "${index}"

weight=10
while IFS= read -r dir; do
  name="$(basename "${dir}")"

  mapfile -t yaml_files < <(find "${dir}" -maxdepth 1 -name '*.yaml' -printf '%f\n' | sort)
  if [ "${#yaml_files[@]}" -eq 0 ]; then
    continue
  fi

  readme="${dir}/README.md"
  title="${name}"
  if [ -f "${readme}" ]; then
    h1="$(grep -m1 '^# ' "${readme}" || true)"
    if [ -n "${h1}" ]; then
      title="${h1#\# }"
    fi
  fi

  page="${out_dir}/${name}.md"
  {
    printf '+++\n'
    printf 'title = "%s"\n' "${title}"
    printf 'weight = %d\n' "${weight}"
    printf '+++\n\n'

    if [ -f "${readme}" ]; then
      # Drop the first level-1 heading; the title comes from front matter.
      awk 'skipped!=1 && /^# /{skipped=1; next} {print}' "${readme}"
      printf '\n'
    fi

    printf '## Manifests\n\n'
    for yf in "${yaml_files[@]}"; do
      printf '### %s\n\n' "${yf}"
      printf '```yaml\n'
      # Normalize the trailing newline so the closing fence is always on its
      # own line, even if the manifest does not end in a newline.
      printf '%s\n' "$(cat "${dir}/${yf}")"
      printf '```\n\n'
    done
  } > "${page}"

  manifest_list="$(printf '`%s` ' "${yaml_files[@]}")"
  printf '| [%s](%s) | %s |\n' "${title}" "${name}" "${manifest_list% }" >> "${index}"

  weight=$((weight + 10))
done < <(find "${examples_root}" -mindepth 1 -maxdepth 1 -type d | sort)

echo "Generated examples docs in ${out_dir}"
