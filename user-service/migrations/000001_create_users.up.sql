-- AI Assistance Disclosure:
-- Tool: Codex (GPT-5), date: 2026-09-18
-- Scope: Added the recorded users table and account enum migration.
-- Author review: COMPLETED BY ZI YANG

CREATE TYPE account_role AS ENUM ('STUDENT', 'ADMIN');
CREATE TYPE account_status AS ENUM ('ACTIVE', 'SUSPENDED');

CREATE TABLE users (
    uid UUID PRIMARY KEY,
    username VARCHAR(128) NOT NULL UNIQUE,
    email VARCHAR(255) NOT NULL UNIQUE,
    password VARCHAR(255) NOT NULL,
    phone_num VARCHAR(20) NOT NULL,
    date_created TIMESTAMPTZ NOT NULL,
    last_login_date TIMESTAMPTZ,
    account_role account_role NOT NULL DEFAULT 'STUDENT',
    account_status account_status NOT NULL DEFAULT 'ACTIVE'
);
