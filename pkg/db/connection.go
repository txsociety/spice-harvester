package db

import (
	"cmp"
	"context"
	"embed"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tonkeeper/tongo/ton"
	"regexp"
	"sort"
	"strconv"
)

type Connection struct {
	postgres *pgxpool.Pool

	recipient ton.AccountID
}

func New(ctx context.Context, postgresURI string, recipient ton.AccountID) (*Connection, error) {
	pool, err := pgxpool.New(ctx, postgresURI)
	if err != nil {
		return nil, err
	}
	err = migrate(ctx, pool)
	if err != nil {
		return nil, err
	}
	return &Connection{
		postgres:  pool,
		recipient: recipient,
	}, nil
}

//go:embed migrations/*.sql
var fs embed.FS

func migrate(ctx context.Context, postgres *pgxpool.Pool) error {
	dir, err := fs.ReadDir("migrations")
	if err != nil {
		return err
	}
	_, err = postgres.Exec(ctx, `create table if not exists schema_migrations (version bigint, dirty boolean)`)
	if err != nil {
		return err
	}
	var version int
	var dirty bool
	err = postgres.QueryRow(ctx, `select version, dirty from schema_migrations limit 1`).Scan(&version, &dirty)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if dirty {
		return fmt.Errorf("database migration is dirty")
	}
	sort.Slice(dir, func(i, j int) bool {
		return cmp.Less(dir[i].Name(), dir[j].Name())
	})
	for _, f := range dir {
		if !f.Type().IsRegular() {
			continue
		}
		matches := regexp.MustCompile(`^(\d+)_(\w+)\.(.+)\.sql$`).FindStringSubmatch(f.Name())
		if len(matches) != 4 {
			return fmt.Errorf("invalid filename %s", f.Name())
		}
		fVersion, _ := strconv.Atoi(matches[1])
		op := matches[3]
		if fVersion <= version {
			continue
		}
		if op != "up" {
			continue
		}
		if fVersion != version+1 {
			return fmt.Errorf("invalid version %d, expected %d", fVersion, version+1)
		}
		if version == 0 {
			_, err = postgres.Exec(ctx, `insert into schema_migrations (version, dirty) values ($1, $2)`, fVersion, true)
		} else {
			_, err = postgres.Exec(ctx, `update schema_migrations set dirty = $1,  version = $2`, true, fVersion)
		}
		if err != nil {
			return err
		}
		data, err := fs.ReadFile("migrations/" + f.Name())
		if err != nil {
			return err
		}
		_, err = postgres.Exec(ctx, string(data))
		if err != nil {
			return err

		}
		_, err = postgres.Exec(ctx, `update schema_migrations set dirty = $1,  version = $2`, false, fVersion)
		if err != nil {
			return err

		}
		version = fVersion
	}
	return nil
}
