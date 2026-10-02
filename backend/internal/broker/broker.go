// Package broker owns JetStream infrastructure and event contracts (developer 2).
package broker

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/nats-io/nats.go/jetstream"
)

const StreamName = "SECUREAI_EVENTS"
const MessageAccepted = "secureai.events.message.accepted.v1"
const GroupChanged = "secureai.events.group.changed.v1"
const DeviceRevoked = "secureai.events.device.revoked.v1"

// Provision is an explicit local provisioning step, not a mutation on API startup.
// One replica is for the single-node development Compose stack only.
func Provision(ctx context.Context, js jetstream.JetStream) error {
	existing, err := js.Stream(ctx, StreamName)
	if err == nil {
		info, err := existing.Info(ctx)
		if err != nil {
			return err
		}
		c := info.Config
		if c.Storage != jetstream.FileStorage || c.Retention != jetstream.LimitsPolicy || len(c.Subjects) != 1 || c.Subjects[0] != "secureai.events.>" {
			return fmt.Errorf("existing stream has incompatible configuration")
		}
		return nil
	}
	if !errors.Is(err, jetstream.ErrStreamNotFound) {
		return err
	}
	_, err = js.CreateStream(ctx, jetstream.StreamConfig{
		Name: StreamName, Subjects: []string{"secureai.events.>"},
		Storage: jetstream.FileStorage, Retention: jetstream.LimitsPolicy,
		Replicas: 1, MaxAge: 7 * 24 * time.Hour, MaxBytes: 256 * 1024 * 1024,
		MaxMsgSize: 16 * 1024, Discard: jetstream.DiscardNew, Duplicates: 2 * time.Minute,
	})
	return err
}
