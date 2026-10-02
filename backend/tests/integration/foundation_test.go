package integration_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/nats-io/nats.go/jetstream"

	"secureai/backend/internal/broker"
	"secureai/backend/internal/platform/config"
	"secureai/backend/internal/platform/dependencies"
	"secureai/backend/migrations"
)

// Uses a dedicated Compose development database. All row-level test data is rolled back.
func TestFoundation(t *testing.T) {
	if os.Getenv("INTEGRATION_TEST") != "1" {
		t.Skip("set INTEGRATION_TEST=1 with the development services running")
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	d, err := dependencies.Open(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	t.Run("concurrent migration rerun", func(t *testing.T) {
		var wg sync.WaitGroup
		errs := make(chan error, 2)
		for range 2 {
			wg.Add(1)
			go func() { defer wg.Done(); errs <- migrations.Up(ctx, d.Postgres) }()
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			if err != nil {
				t.Fatal(err)
			}
		}
	})
	t.Run("stream provisioning is idempotent", func(t *testing.T) {
		if err := broker.Provision(ctx, d.JetStream); err != nil {
			t.Fatal(err)
		}
		if err := broker.Provision(ctx, d.JetStream); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("readiness dependencies", func(t *testing.T) {
		for name, check := range d.Checks() {
			if err := check(ctx); err != nil {
				t.Errorf("%s: %v", name, err)
			}
		}
	})
	t.Run("schema rejects duplicate message and inconsistent epoch", func(t *testing.T) {
		tx, err := d.Postgres.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback(context.Background()) }()
		const user = "00000000-0000-4000-8000-000000000001"
		const device = "00000000-0000-4000-8000-000000000002"
		const message = "00000000-0000-4000-8000-000000000003"
		const clientID = "00000000-0000-4000-8000-000000000004"
		if _, err := tx.Exec(ctx, `INSERT INTO users(id,username,password_hash) VALUES ($1,'foundation_test','test-only')`, user); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO devices(id,user_id,registration_id,identity_key) VALUES ($1,$2,1,$3)`, device, user, []byte{1}); err != nil {
			t.Fatal(err)
		}
		insert := `INSERT INTO messages(id,sender_device_id,client_message_id,request_hash,expires_at) VALUES ($1,$2,$3,$4,now()+interval '1 day')`
		if _, err := tx.Exec(ctx, insert, message, device, clientID, make([]byte, 32)); err != nil {
			t.Fatal(err)
		}
		expectConstraint(t, ctx, tx, func(nested pgx.Tx) error {
			_, err := nested.Exec(ctx, insert, "00000000-0000-4000-8000-000000000005", device, clientID, make([]byte, 32))
			return err
		})
		expectConstraint(t, ctx, tx, func(nested pgx.Tx) error {
			_, err := nested.Exec(ctx, `UPDATE messages SET group_epoch=1 WHERE id=$1`, message)
			return err
		})
		expectConstraint(t, ctx, tx, func(nested pgx.Tx) error {
			_, err := nested.Exec(ctx, `INSERT INTO message_envelopes(message_id,recipient_device_id,envelope_type,ciphertext) VALUES ($1,$2,'signal',$3)`, message, device, []byte{})
			return err
		})
	})
	t.Run("Redis round trip", func(t *testing.T) {
		key := fmt.Sprintf("secureai:test:%d", time.Now().UnixNano())
		if err := d.Redis.Set(ctx, key, "ok", time.Minute).Err(); err != nil {
			t.Fatal(err)
		}
		defer d.Redis.Del(ctx, key)
		if value, err := d.Redis.Get(ctx, key).Result(); err != nil || value != "ok" {
			t.Fatalf("value=%q err=%v", value, err)
		}
	})
	t.Run("JetStream publish consume and deduplicate", func(t *testing.T) {
		// Isolated temporary subject/stream; never pollute the application event stream.
		name := fmt.Sprintf("FOUNDATION_TEST_%d", time.Now().UnixNano())
		subject := "secureai.test." + name
		stream, err := d.JetStream.CreateStream(ctx, jetstream.StreamConfig{Name: name, Subjects: []string{subject}, Storage: jetstream.MemoryStorage, MaxAge: time.Minute})
		if err != nil {
			t.Fatal(err)
		}
		defer d.JetStream.DeleteStream(context.Background(), name)
		for i := range 2 {
			ack, err := d.JetStream.Publish(ctx, subject, []byte(`{"event_id":"test"}`), jetstream.WithMsgID("test"))
			if err != nil {
				t.Fatal(err)
			}
			if ack.Duplicate != (i == 1) {
				t.Fatal("unexpected deduplication result")
			}
		}
		consumer, err := stream.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{Durable: "test", AckPolicy: jetstream.AckExplicitPolicy})
		if err != nil {
			t.Fatal(err)
		}
		msg, err := consumer.Next(jetstream.FetchMaxWait(5 * time.Second))
		if err != nil {
			t.Fatal(err)
		}
		if string(msg.Data()) != `{"event_id":"test"}` {
			t.Fatal("event payload changed")
		}
		if err := msg.DoubleAck(ctx); err != nil {
			t.Fatal(err)
		}
	})
}

func expectConstraint(t *testing.T, ctx context.Context, tx pgx.Tx, action func(pgx.Tx) error) {
	t.Helper()
	nested, err := tx.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer nested.Rollback(ctx)
	err = action(nested)
	var constraint *pgconn.PgError
	if !errors.As(err, &constraint) || (constraint.Code != "23505" && constraint.Code != "23514") {
		t.Fatalf("expected unique/check constraint violation, got %v", err)
	}
}
