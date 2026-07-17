#!/bin/bash
# Validates case s1: basic single-database, single-app-user provisioning.
# - Checks that database `cloudutil_case_s1` was created.
# - Checks that the `pgcrypto` extension was installed in that database.
# - Checks that app user `cloudutil_s1_app` can authenticate and read seeded
#   smoke-test data from `public.s1_smoke`, confirming role creation and
#   grants took effect end-to-end.
set -euo pipefail

psql_root() {
  docker compose exec -T postgres \
    psql -U aws_internal_superuser -d "$1" -Atqc "$2"
}

# Verify the database was created
[ "$(psql_root postgres "SELECT 1 FROM pg_database WHERE datname = 'cloudutil_case_s1'")" = "1" ]

# Verify the pgcrypto extension was installed in that database
[ "$(psql_root cloudutil_case_s1 "SELECT extname FROM pg_extension WHERE extname = 'pgcrypto'")" = "pgcrypto" ]

# Verify the app user can authenticate and read seeded smoke-test data
[ "$(docker compose exec -T -e PGPASSWORD=app-password postgres \
  psql -h 127.0.0.1 -U cloudutil_s1_app -d cloudutil_case_s1 -Atqc \
  "SELECT value FROM public.s1_smoke WHERE id = 1")" = "hello-from-s1" ]

echo "PASS: case s1"
