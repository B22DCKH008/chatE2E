CREATE TABLE users (
    id UUID PRIMARY KEY,
    username TEXT NOT NULL UNIQUE CHECK (username ~ '^[a-z0-9_]{3,32}$'),
    password_hash TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE devices (
    id UUID PRIMARY KEY,
    user_id UUID NOT NULL REFERENCES users(id),
    registration_id INTEGER NOT NULL CHECK (registration_id > 0),
    identity_key BYTEA NOT NULL CHECK (octet_length(identity_key) BETWEEN 1 AND 4096),
    identity_version INTEGER NOT NULL DEFAULT 1 CHECK (identity_version > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    revoked_at TIMESTAMPTZ,
    UNIQUE (id, user_id)
);
CREATE INDEX devices_active_user_idx ON devices(user_id) WHERE revoked_at IS NULL;

CREATE TABLE auth_sessions (
    id UUID PRIMARY KEY,
    device_id UUID NOT NULL REFERENCES devices(id),
    refresh_token_hash BYTEA NOT NULL UNIQUE CHECK (octet_length(refresh_token_hash) = 32),
    family_id UUID NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ NOT NULL,
    revoked_at TIMESTAMPTZ,
    CHECK (expires_at > created_at)
);
CREATE INDEX auth_sessions_device_idx ON auth_sessions(device_id);
CREATE INDEX auth_sessions_family_idx ON auth_sessions(family_id);

CREATE TABLE signed_prekeys (
    device_id UUID NOT NULL REFERENCES devices(id),
    key_id BIGINT NOT NULL CHECK (key_id BETWEEN 0 AND 4294967295),
    public_key BYTEA NOT NULL CHECK (octet_length(public_key) BETWEEN 1 AND 4096),
    signature BYTEA NOT NULL CHECK (octet_length(signature) BETWEEN 1 AND 4096),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    retired_at TIMESTAMPTZ,
    PRIMARY KEY (device_id, key_id)
);
CREATE UNIQUE INDEX signed_prekeys_current_idx ON signed_prekeys(device_id) WHERE retired_at IS NULL;

CREATE TABLE one_time_prekeys (
    device_id UUID NOT NULL REFERENCES devices(id),
    key_id BIGINT NOT NULL CHECK (key_id BETWEEN 0 AND 4294967295),
    public_key BYTEA NOT NULL CHECK (octet_length(public_key) BETWEEN 1 AND 4096),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    claimed_at TIMESTAMPTZ,
    PRIMARY KEY (device_id, key_id)
);
CREATE INDEX one_time_prekeys_available_idx ON one_time_prekeys(device_id, key_id) WHERE claimed_at IS NULL;

CREATE TABLE groups (
    id UUID PRIMARY KEY,
    owner_user_id UUID NOT NULL REFERENCES users(id),
    epoch BIGINT NOT NULL DEFAULT 1 CHECK (epoch > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE group_members (
    group_id UUID NOT NULL REFERENCES groups(id),
    user_id UUID NOT NULL REFERENCES users(id),
    role TEXT NOT NULL CHECK (role IN ('owner', 'admin', 'member')),
    joined_epoch BIGINT NOT NULL CHECK (joined_epoch > 0),
    left_epoch BIGINT CHECK (left_epoch > joined_epoch),
    PRIMARY KEY (group_id, user_id)
);
CREATE UNIQUE INDEX group_single_owner_idx ON group_members(group_id) WHERE role = 'owner' AND left_epoch IS NULL;
CREATE INDEX group_members_active_user_idx ON group_members(user_id) WHERE left_epoch IS NULL;

