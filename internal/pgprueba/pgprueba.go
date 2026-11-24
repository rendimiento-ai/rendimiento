// Package pgprueba gives each test a Postgres database of its own, a copy
// of one migrated once for the whole package (CREATE DATABASE … TEMPLATE).
// Only tests import it.
//
// Dropping the schema and running every migration again for each test
// made internal/store six to eight minutes long in CI (runs 1697, 1706 and
// 1737), and internal/api, with 60 tests on the database, went from 5 to
// 12 minutes in a week (run 1786). A copy of the migrated template takes
// one statement, however many migrations there are.
package pgprueba

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Migrar applies every migration to the database dsn names.
type Migrar func(ctx context.Context, dsn string) error

// Base returns the DSN of a new database for this test: a copy of
// <db>_plantilla (the name of TEST_DATABASE_URL's database with a suffix),
// which the first test of the package makes with migrar, applied twice
// since migrating must be idempotent. The copy is dropped when the test
// ends; close what connects to it in a cleanup registered after Base (they
// run last first). Without TEST_DATABASE_URL the test is skipped.
//
// Packages that use it must not run at the same time on one server: each
// makes the template again. hack/ci-test.sh runs them one after another
// (-p 1), or each with its own Postgres.
func Base(t testing.TB, migrar Migrar) string {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	plantilla.once.Do(func() {
		plantilla.admin, plantilla.base, plantilla.err = hacerPlantilla(ctx, dsn, migrar)
	})
	if plantilla.err != nil {
		t.Fatal(plantilla.err)
	}
	admin, base := plantilla.admin, plantilla.base
	name := fmt.Sprintf("%s_prueba_%d", base, copias.Add(1))
	// One copy at a time: a template cannot be copied while another
	// session uses it, and the copies never connect to it, but nothing is
	// gained by racing.
	plantilla.mu.Lock()
	defer plantilla.mu.Unlock()
	if _, err := admin.Exec(ctx, `DROP DATABASE IF EXISTS `+quoteIdent(name)+` WITH (FORCE)`); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, `CREATE DATABASE `+quoteIdent(name)+` TEMPLATE `+quoteIdent(base+"_plantilla")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec(context.Background(), `DROP DATABASE IF EXISTS `+quoteIdent(name)+` WITH (FORCE)`)
	})
	copia, err := conBase(dsn, name)
	if err != nil {
		t.Fatal(err)
	}
	return copia
}

// copias numbers the tests' databases.
var copias atomic.Int64

// plantilla is the package's migrated database, made by the first test.
var plantilla struct {
	once  sync.Once
	mu    sync.Mutex
	admin *pgxpool.Pool // on TEST_DATABASE_URL's database, to create and drop the copies
	base  string
	err   error
}

func hacerPlantilla(ctx context.Context, dsn string, migrar Migrar) (*pgxpool.Pool, string, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, "", err
	}
	base := cfg.ConnConfig.Database
	cfg.MaxConns = 2
	admin, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, "", err
	}
	fail := func(err error) (*pgxpool.Pool, string, error) {
		admin.Close()
		return nil, "", err
	}
	name := base + "_plantilla"
	if _, err := admin.Exec(ctx, `DROP DATABASE IF EXISTS `+quoteIdent(name)+` WITH (FORCE)`); err != nil {
		return fail(err)
	}
	if _, err := admin.Exec(ctx, `CREATE DATABASE `+quoteIdent(name)); err != nil {
		return fail(err)
	}
	target, err := conBase(dsn, name)
	if err != nil {
		return fail(err)
	}
	// migrar closes its connections when it returns: a template cannot be
	// copied while anyone is connected to it.
	if err := migrar(ctx, target); err != nil {
		return fail(fmt.Errorf("migrate %s: %w", name, err))
	}
	if err := migrar(ctx, target); err != nil {
		return fail(fmt.Errorf("migrate must be idempotent: %w", err))
	}
	return admin, base, nil
}

// conBase is dsn with another database of the same server.
func conBase(dsn, name string) (string, error) {
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		u, err := url.Parse(dsn)
		if err != nil {
			return "", err
		}
		u.Path, u.RawPath = "/"+name, ""
		return u.String(), nil
	}
	// Keyword/value: the last dbname wins.
	return dsn + " dbname='" + strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(name) + "'", nil
}

// quoteIdent quotes a database name for SQL.
func quoteIdent(s string) string { return `"` + strings.ReplaceAll(s, `"`, `""`) + `"` }
