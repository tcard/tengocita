
CREATE TABLE "businesses" (
    "id" text NOT NULL PRIMARY KEY,
    "email" text NULL UNIQUE,
    "phone" text NULL UNIQUE,
    "password" text NOT NULL,
    "name" text NULL,
    "address" text NULL,
    "photo" bytea NULL,
    "created_at" timestamptz NOT NULL DEFAULT now(),
    "last_login" timestamptz NULL,
    "signup_promo_code" text NULL,
    "can_send_promo_emails" boolean NOT NULL DEFAULT true,
    "promo_emails_unsubscribe_link" text NOT NULL UNIQUE DEFAULT base64_web_encode(random_bytea(12)),
    CHECK (NOT (("email" IS NULL) AND ("phone" IS NULL)))
) WITH (oids = false);

CREATE TABLE "business_sessions" (
    "business_id" text NOT NULL REFERENCES "businesses" ("id") ON DELETE CASCADE ON UPDATE CASCADE,
    "id" TEXT NOT NULL,
    "created_at" timestamptz NOT NULL DEFAULT now(),
    "last_used" timestamptz DEFAULT now(),
    PRIMARY KEY ("business_id", "id")
) WITH (oids = false);

CREATE OR REPLACE FUNCTION random_bytea(bytea_length integer)
RETURNS bytea AS $body$
    SELECT decode(string_agg(lpad(to_hex(width_bucket(random(), 0, 1, 256)-1),2,'0') ,''), 'hex')
    FROM generate_series(1, $1);
$body$
LANGUAGE 'sql'
VOLATILE    
SET search_path = 'pg_catalog';


CREATE OR REPLACE FUNCTION base64_web_encode(b bytea)
RETURNS text AS $body$
    SELECT replace(replace(encode(b, 'base64'), '+', '-'), '/', '_');
$body$
LANGUAGE 'sql'
VOLATILE
SET search_path = 'pg_catalog';

CREATE OR REPLACE FUNCTION timestamptz_to_date(timestamptz) 
  RETURNS date AS
$func$
SELECT ($1 AT TIME ZONE 'UTC') :: date
$func$ LANGUAGE sql IMMUTABLE;

CREATE TABLE "appointments" (
    "business_id" text NOT NULL REFERENCES "businesses" ("id") ON DELETE CASCADE ON UPDATE CASCADE,
    "id" text NOT NULL,
    "customer_link" text NOT NULL UNIQUE DEFAULT base64_web_encode(random_bytea(9)),
    "customer_code" int NOT NULL DEFAULT 0 +
        width_bucket(random(), 0, 1, 9) * 1 +
        width_bucket(random(), 0, 1, 9) * 10 +
        width_bucket(random(), 0, 1, 9) * 100 +
        width_bucket(random(), 0, 1, 9) * 1000,
    "number" int NOT NULL,
    "start" timestamptz NOT NULL,
    "end" timestamptz NOT NULL,
    "phone" text,
    "email" text,
    "name" text,
    "comments" text,
    "started_at" timestamptz,
    "finished_at" timestamptz,
    "canceled_at" timestamptz,
    "cancel_reason" text,
    "created_at" timestamptz NOT NULL DEFAULT now(),
    "push_subscription" json,
    PRIMARY KEY ("business_id", "id"),
    CHECK (NOT (("email" IS NULL) AND ("phone" IS NULL))),
    CHECK (NOT (("finished_at" IS NOT NULL) AND ("canceled_at" IS NOT NULL))),
    CHECK (NOT (("finished_at" IS NOT NULL) AND ("started_at" IS NULL))),
    CHECK (NOT (("cancel_reason" IS NOT NULL) AND ("canceled_at" IS NULL)))
) WITH (oids = false);

CREATE INDEX ON appointments ("business_id", "end", "start");
CREATE UNIQUE INDEX ON appointments ("business_id", (("start" AT TIME ZONE 'UTC') :: date), "customer_code");

CREATE TABLE "last_appointment_number_for_day" (
    "business_id" text NOT NULL REFERENCES "businesses" ("id") ON DELETE CASCADE ON UPDATE CASCADE,
    "day" date NOT NULL,
    "number" int NOT NULL DEFAULT 1,
    PRIMARY KEY ("business_id", "day")
)  WITH (oids = false);

CREATE TABLE "delay_alerts" (
    "business_id" text NOT NULL REFERENCES "businesses" ("id") ON DELETE CASCADE ON UPDATE CASCADE,
    "checking_started" timestamptz,
    "last_delay" interval,
    "last_start_cutoff" timestamptz,
    PRIMARY KEY ("business_id")
) WITH (oids = false);
