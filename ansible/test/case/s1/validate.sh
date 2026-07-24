#!/bin/bash
# Validates case s1: basic single-database provisioning with readwrite and
# readonly app users.
# - Checks that database `cloudutil_case_s1` was created.
# - Checks that the `pgcrypto` extension was installed in that database.
# - Checks that app user `cloudutil_s1_app` can authenticate and read seeded
#   smoke-test data from `public.s1_smoke`, confirming role creation and
#   grants took effect end-to-end.
# - Checks that readonly user `cloudutil_s1_reader` can read the same data
#   but is denied when attempting an INSERT (read-only enforcement).
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

# Verify the reader can read seeded data
[ "$(docker compose exec -T -e PGPASSWORD=reader-password postgres \
  psql -h 127.0.0.1 -U cloudutil_s1_reader -d cloudutil_case_s1 -Atqc \
  "SELECT value FROM public.s1_smoke WHERE id = 1")" = "hello-from-s1" ]

# Verify the reader is denied INSERT (read-only enforcement)
if docker compose exec -T -e PGPASSWORD=reader-password postgres \
  psql -v ON_ERROR_STOP=1 -h 127.0.0.1 -U cloudutil_s1_reader -d cloudutil_case_s1 \
  -c "INSERT INTO public.s1_smoke VALUES (2, 'forbidden')" >/dev/null 2>&1; then
  echo "reader unexpectedly received INSERT" >&2
  exit 1
fi

echo "PASS: case s1"
