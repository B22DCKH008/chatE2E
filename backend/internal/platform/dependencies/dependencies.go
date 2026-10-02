package dependencies

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/redis/go-redis/v9"

	"secureai/backend/internal/broker"
	"secureai/backend/internal/platform/config"
	"secureai/backend/internal/platform/httpapi"
	"secureai/backend/migrations"
)

type Dependencies struct {
	Postgres  *pgxpool.Pool
	Redis     *redis.Client
	NATS      *nats.Conn
	JetStream jetstream.JetStream
}

func Open(ctx context.Context, cfg config.Config) (*Dependencies, error) {
	d := &Dependencies{}
	ready := false
	defer func() {
		if !ready {
			d.Close()
		}
	}()
	poolConfig, err := pgxpool.ParseConfig(cfg.DatabaseURL)
	if err != nil {
		return nil, errors.New("invalid PostgreSQL configuration")
	}
	poolConfig.MaxConns = 10
	poolConfig.ConnConfig.ConnectTimeout = cfg.DependencyTimeout
	d.Postgres, err = pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, errors.New("PostgreSQL initialization failed")
	}
	if err := d.Postgres.Ping(ctx); err != nil {
		return nil, errors.New("PostgreSQL connection failed")
	}
	redisConfig, err := redis.ParseURL(cfg.RedisURL)
	if err != nil {
		return nil, errors.New("invalid Redis configuration")
	}
	redisConfig.DialTimeout = cfg.DependencyTimeout
	redisConfig.ReadTimeout = cfg.DependencyTimeout
	redisConfig.WriteTimeout = cfg.DependencyTimeout
	redisConfig.ContextTimeoutEnabled = true
	d.Redis = redis.NewClient(redisConfig)
	if err := d.Redis.Ping(ctx).Err(); err != nil {
		return nil, errors.New("Redis connection failed")
	}
	d.NATS, err = nats.Connect(cfg.NATSURL, nats.Name("secureai-api"), nats.Timeout(cfg.DependencyTimeout), nats.MaxReconnects(-1), nats.ReconnectWait(time.Second))
	if err != nil {
		return nil, errors.New("NATS connection failed")
	}
	d.JetStream, err = jetstream.New(d.NATS)
	if err != nil {
		return nil, errors.New("JetStream initialization failed")
	}
	if _, err := d.JetStream.AccountInfo(ctx); err != nil {
		return nil, errors.New("JetStream is unavailable")
	}
	ready = true
	return d, nil
}

func (d *Dependencies) Checks() map[string]httpapi.Check {
	return map[string]httpapi.Check{
		"postgres": d.Postgres.Ping,
		"schema":   func(ctx context.Context) error { return migrations.Check(ctx, d.Postgres) },
		"redis":    func(ctx context.Context) error { return d.Redis.Ping(ctx).Err() },
		"jetstream": func(ctx context.Context) error {
			if !d.NATS.IsConnected() {
				return errors.New("NATS disconnected")
			}
			_, err := d.JetStream.Stream(ctx, broker.StreamName)
			return err
		},
	}
}

func (d *Dependencies) Close() {
	if d.NATS != nil {
		d.NATS.Close()
	}
	if d.Redis != nil {
		_ = d.Redis.Close()
	}
	if d.Postgres != nil {
		d.Postgres.Close()
	}
}
