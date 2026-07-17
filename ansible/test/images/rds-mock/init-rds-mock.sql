-- Mock the AWS RDS master-account shape. The provisioning role is deliberately
-- not a PostgreSQL superuser, but can create databases and roles.
CREATE ROLE rds_superuser;

CREATE ROLE rds_master_user WITH
    LOGIN
    NOSUPERUSER
    INHERIT
    CREATEDB
    CREATEROLE
    REPLICATION
    ENCRYPTED PASSWORD 'my_secure_rds_password';

GRANT rds_superuser TO rds_master_user;
ALTER DATABASE postgres OWNER TO rds_master_user;
