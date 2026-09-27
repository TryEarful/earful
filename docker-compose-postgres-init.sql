-- Runs once on first container init alongside the dev DB, so make test /
-- CI can use a separate database on the same Postgres instance.
CREATE DATABASE earful_test;
