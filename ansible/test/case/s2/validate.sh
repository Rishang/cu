#!/bin/bash
# Validates case s2: RDS-style master role and reader/writer privilege model.
# - Checks `rds_master_user` role attributes: not superuser, but CREATEDB and
#   CREATEROLE are granted (mirrors AWS RDS master user behavior).
# - Checks that database `cloudutil_case_s2` is owned by `rds_master_user`.
# - Checks that the `pgcrypto` extension was installed in that database.
# - Checks table privileges: `cloudutil_s2_reader` has SELECT but not INSERT
#   on `public.s2_role_smoke`; `cloudutil_s2_writer` has full SELECT/INSERT/
#   UPDATE/DELETE; `cloudutil_s2_reader` also has SELECT on
#   `public.s2_default_privileges_smoke` (verifies default privileges applied
#   to objects created after the grants).
# - Checks that `cloudutil_s2_reader` can actually read seeded data created
#   by the master user.
# - Checks that `cloudutil_s2_reader` is denied when attempting an INSERT
#   (enforces the read-only grant, not just its absence in pg_catalog).
# - Checks that `cloudutil_s2_writer` can INSERT and that the change is
#   visible to the master user, confirming write privileges function.
set -euo pipefail

psql_root() {
  docker compose exec -T postgres \
    psql -U rds_master_user -d "$1" -Atqc "$2"
}

# Verify rds_master_user is not superuser, but has CREATEDB and CREATEROLE
[ "$(psql_root postgres "SELECT rolsuper || ',' || rolcreatedb || ',' || rolcreaterole FROM pg_roles WHERE rolname = 'rds_master_user'")" = "false,true,true" ]

# Verify the database is owned by rds_master_user
[ "$(psql_root postgres "SELECT pg_get_userbyid(datdba) FROM pg_database WHERE datname = 'cloudutil_case_s2'")" = "rds_master_user" ]

# Verify the pgcrypto extension was installed in the database
[ "$(psql_root cloudutil_case_s2 "SELECT extname FROM pg_extension WHERE extname = 'pgcrypto'")" = "pgcrypto" ]

# Verify table privileges: reader has SELECT only, writer has full CRUD, reader has SELECT on default-privileges table
[ "$(psql_root cloudutil_case_s2 "SELECT has_table_privilege('cloudutil_s2_reader', 'public.s2_role_smoke', 'SELECT'), has_table_privilege('cloudutil_s2_reader', 'public.s2_role_smoke', 'INSERT'), has_table_privilege('cloudutil_s2_writer', 'public.s2_role_smoke', 'SELECT,INSERT,UPDATE,DELETE'), has_table_privilege('cloudutil_s2_reader', 'public.s2_default_privileges_smoke', 'SELECT')")" = "t|f|t|t" ]

# Verify the reader can read seeded data
[ "$(docker compose exec -T -e PGPASSWORD=reader-password postgres \
  psql -h 127.0.0.1 -U cloudutil_s2_reader -d cloudutil_case_s2 -Atqc \
  "SELECT value FROM public.s2_role_smoke WHERE id = 1")" = "created-by-rds-master" ]

# Verify the reader is denied INSERT (read-only enforcement)
if docker compose exec -T -e PGPASSWORD=reader-password postgres \
  psql -v ON_ERROR_STOP=1 -h 127.0.0.1 -U cloudutil_s2_reader -d cloudutil_case_s2 \
  -c "INSERT INTO public.s2_role_smoke VALUES (2, 'forbidden')" >/dev/null 2>&1; then
  echo "reader unexpectedly received INSERT" >&2
  exit 1
fi

# Verify the writer can INSERT and the change is visible to the master user
docker compose exec -T -e PGPASSWORD=writer-password postgres \
  psql -v ON_ERROR_STOP=1 -h 127.0.0.1 -U cloudutil_s2_writer -d cloudutil_case_s2 \
  -c "INSERT INTO public.s2_role_smoke VALUES (2, 'writer-ok')" >/dev/null
[ "$(psql_root cloudutil_case_s2 "SELECT value FROM public.s2_role_smoke WHERE id = 2")" = "writer-ok" ]

echo "PASS: case s2"