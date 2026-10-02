-- Metadata for a logical send. Sender identity comes from verified auth, never the body.
CREATE TABLE messages (
    id UUID PRIMARY KEY,
    sender_device_id UUID NOT NULL REFERENCES devices(id),
    client_message_id UUID NOT NULL,
    request_hash BYTEA NOT NULL CHECK (octet_length(request_hash) = 32),
    group_id UUID REFERENCES groups(id),
    group_epoch BIGINT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ NOT NULL,
    UNIQUE (sender_device_id, client_message_id),
    CHECK ((group_id IS NULL AND group_epoch IS NULL) OR (group_id IS NOT NULL AND group_epoch IS NOT NULL AND group_epoch > 0)),
    CHECK (expires_at > created_at)
);
CREATE INDEX messages_expiry_idx ON messages(expires_at);

-- A cursor identifies an envelope, not a plaintext message or a global delivery order.
CREATE TABLE message_envelopes (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    message_id UUID NOT NULL REFERENCES messages(id),
    recipient_device_id UUID NOT NULL REFERENCES devices(id),
    envelope_type TEXT NOT NULL CHECK (envelope_type IN ('prekey', 'signal', 'sender_key', 'sender_key_distribution')),
    ciphertext BYTEA NOT NULL CHECK (octet_length(ciphertext) BETWEEN 1 AND 262144),
    delivered_at TIMESTAMPTZ,
    read_at TIMESTAMPTZ,
    UNIQUE (message_id, recipient_device_id),
    CHECK (read_at IS NULL OR delivered_at IS NOT NULL)
);
CREATE INDEX message_envelopes_pending_idx ON message_envelopes(recipient_device_id, id) WHERE delivered_at IS NULL;

-- Inserted in the SAME transaction as messages/envelopes; publish happens after commit.
-- payload contains event IDs, message IDs and device routing IDs only.
CREATE TABLE outbox_events (
    id UUID PRIMARY KEY,
    subject TEXT NOT NULL,
    payload JSONB NOT NULL CHECK (jsonb_typeof(payload) = 'object'),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    published_at TIMESTAMPTZ,
    attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX outbox_pending_idx ON outbox_events(next_attempt_at, created_at) WHERE published_at IS NULL;

CREATE TABLE audit_events (
    id UUID PRIMARY KEY,
    actor_user_id UUID REFERENCES users(id),
    actor_device_id UUID REFERENCES devices(id),
    action TEXT NOT NULL,
    outcome TEXT NOT NULL CHECK (outcome IN ('success', 'denied', 'failure')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX audit_events_created_idx ON audit_events(created_at);

