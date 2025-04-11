BEGIN;

create schema if not exists blockchain;

create table  blockchain.trusted_mc_block
(
    id         integer  primary key,
    seqno      integer  not null,
    root_hash  bytea    not null,
    file_hash  bytea    not null
);

create table if not exists blockchain.accounts
(
    last_processed_lt   bigint       not null,            -- last transaction processed by indexer (indexer is separated process from loading tx to db)
    last_tx_lt          bigint       not null,
    start_tx_lt         bigint       not null,            -- start indexing from the last transaction at the time of adding the account
    last_checked_block  bigint       not null default 0,  -- masterchain block number for which the account state was requested
    indexer_timestamp   timestamptz  not null,
    last_tx_hash        bytea        not null,
    address             text         primary key
);

create table if not exists blockchain.transactions (
     lt                bigint  not null,
     prev_tx_lt        bigint  not null,
     utime             bigint  not null,
     success           boolean not null,
     account_id        text    not null,
     prev_tx_hash      bytea   not null,
     hash              bytea   primary key,
     processing_error  text,
     in_message        jsonb,
     out_messages      jsonb[]
);
create index if not exists transactions_prev_lt_account_idx on blockchain.transactions (prev_tx_lt, account_id);
create index if not exists transactions_lt_account_idx on blockchain.transactions (lt, account_id);
create index if not exists transactions_prev_tx_hash_idx on blockchain.transactions (prev_tx_hash);

create schema if not exists payments;

create type   currency_type as enum ('ton', 'jetton', 'extra');
create table  payments.currencies
(
    id      uuid default gen_random_uuid() primary key,
    type    currency_type not null,
    info    text not null,
    unique  (type, info)
);

create table if not exists payments.jetton_wallets
(
    currency  uuid not null references payments.currencies(id),
    address   text not null references blockchain.accounts(address),
    owner     text not null
);
create index if not exists jetton_wallets_address_currency_idx on payments.jetton_wallets (address, currency);

create type   invoice_status_type as enum ('waiting', 'paid', 'cancelled', 'expired');
create table  payments.invoices
(
    id            uuid primary key,
    currency      uuid not null references payments.currencies (id),
    created_at    timestamptz not null,
    expire_at     timestamptz not null,
    updated_at    timestamptz not null,
    paid_at       timestamptz,
    amount        numeric not null,
    overpayment   numeric not null,
    status        invoice_status_type not null,
    recipient     text not null,
    paid_by       text,            -- payer who changed the invoice status to "paid"
    tx_hash       bytea,           -- transaction that changed the invoice status to "paid"
    private_info  jsonb not null,
    metadata      jsonb not null
);
create index if not exists invoices_status_expire_at_idx on payments.invoices (status, expire_at);
create index if not exists invoices_currency_recipient_idx on payments.invoices (currency, recipient);

create table  payments.invoice_notifications -- sync with payments.invoices
(
    id            uuid, -- not primary key
    currency      uuid not null references payments.currencies (id),
    created_at    timestamptz not null,
    expire_at     timestamptz not null,
    updated_at    timestamptz not null,
    paid_at       timestamptz,
    amount        numeric not null,
    overpayment   numeric not null,
    status        invoice_status_type not null,
    recipient     text not null,
    paid_by       text,
    tx_hash       bytea,
    private_info  jsonb not null,
    metadata      jsonb not null
);
create index if not exists invoice_notifications_updated_at_idx on payments.invoice_notifications (updated_at);

create table if not exists payments.keys
(
    created_at      timestamptz not null,
    accepted        boolean     not null default false,
    address         text        primary key,
    encryption_key  bytea       not null
);
create index if not exists keys_created_at_accepted_idx on payments.keys (created_at, accepted);

COMMIT;
