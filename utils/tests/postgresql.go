// pmm-agent
// Copyright (C) 2018 Percona LLC
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program. If not, see <https://www.gnu.org/licenses/>.

package tests

import (
	"database/sql"
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"

	_ "github.com/lib/pq" // register SQL driver
	"github.com/stretchr/testify/require"
)

// regexps to extract version numbers from the `SELECT version()` output
var (
	postgresDBRegexp = regexp.MustCompile(`PostgreSQL ([\d\.]+)`)
)

// GetTestPostgreSQLDSN returns DNS for PostgreSQL test database.
func GetTestPostgreSQLDSN(tb testing.TB) string {
	tb.Helper()

	if testing.Short() {
		tb.Skip("-short flag is passed, skipping test with real database.")
	}
	q := make(url.Values)
	q.Set("sslmode", "disable") // TODO: make it configurable

	u := &url.URL{
		Scheme:   "postgres",
		Host:     net.JoinHostPort("localhost", strconv.Itoa(int(15432))),
		Path:     "pmm-agent",
		User:     url.UserPassword("pmm-agent", "pmm-agent-password"),
		RawQuery: q.Encode(),
	}

	return u.String()
}

// OpenTestPostgreSQL opens connection to PostgreSQL test database.
func OpenTestPostgreSQL(tb testing.TB) *sql.DB {
	tb.Helper()

	db, err := sql.Open("postgres", GetTestPostgreSQLDSN(tb))
	require.NoError(tb, err)

	db.SetMaxIdleConns(10)
	db.SetMaxOpenConns(10)
	db.SetConnMaxLifetime(0)

	waitForFixtures(tb, db)

	return db
}

// PostgreSQLVersion returns major PostgreSQL version (e.g. "9.6", "10", etc.).
func PostgreSQLVersion(tb testing.TB, db *sql.DB) string {
	tb.Helper()

	var version string
	err := db.QueryRow("SELECT /* pmm-agent-tests:PostgreSQLVersion */ version()").Scan(&version)
	require.NoError(tb, err)

	m := parsePostgreSQLVersion(version)
	require.NotEmpty(tb, m, "Failed to parse PostgreSQL version from %q.", version)
	tb.Logf("version = %q (m = %q)", version, m)
	return m
}

func parsePostgreSQLVersion(v string) string {
	m := postgresDBRegexp.FindStringSubmatch(v)
	if len(m) != 2 {
		return ""
	}

	parts := strings.Split(m[1], ".")
	switch len(parts) {
	case 1: // major only
		return parts[0]
	case 2: // major and patch
		return parts[0]
	case 3: // major, minor, and patch
		return parts[0] + "." + parts[1]
	default:
		return ""
	}
}
