#!/usr/bin/env bash
set -euo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
cd "$root"

plan_flags=()
if [[ "${1:-}" == "--check" ]]; then
  plan_flags=(--check --diff)
fi

export COMPOSE_FILE=docker-compose.yml
export COMPOSE_PROJECT_NAME=cloudutil-ansible-test

cleanup() {
  docker compose down --volumes --remove-orphans >/dev/null 2>&1 || true
}
trap cleanup EXIT

docker compose build
docker compose up -d --wait postgres

for case_dir in case/*/; do
  case_name=$(basename "$case_dir")
  echo "== Running case: $case_name =="
  docker compose run --rm \
    -v "$root/$case_dir:/case:ro" \
    ansible -i localhost, /case/playbook.yml -vv "${plan_flags[@]}"
  if [[ ${#plan_flags[@]} -eq 0 ]]; then
    bash "$case_dir/validate.sh"
  fi
done

echo "All end-user test cases passed."
