#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."
export GOWORK=off
fuzztime=${FUZZTIME:-10s}
parallel=${FUZZPARALLEL:-2}
count=0

# Discover native targets just as ntfuzz does; do not maintain a second list.
# Keep the examples module included when targets are added there in the future.
for module in . examples; do
  packages=$(go -C "$module" list ./...)
  for package in $packages; do
    listing=$(go -C "$module" test "$package" -list '^Fuzz' -run '^$')
    for target in $listing; do
      if [[ "$target" =~ ^Fuzz[A-Z0-9_][A-Za-z0-9_]*$ ]]; then
        echo "Fuzzing $package/$target ($fuzztime, $parallel workers)"
        go -C "$module" test "$package" -run '^$' -fuzz "^${target}$" \
          -fuzztime "$fuzztime" -parallel "$parallel"
        count=$((count + 1))
      fi
    done
  done
done

if [[ "$count" -eq 0 ]]; then
  echo 'No fuzz targets discovered' >&2
  exit 1
fi
echo "Fuzzed $count targets"
