BEGIN;

drop schema if exists payments cascade;
drop schema if exists blockchain cascade;

drop type if exists currency_type;
drop type if exists invoice_status_type;

COMMIT ;