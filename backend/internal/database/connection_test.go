package database_test

import (
	"strings"
	"testing"
	"time"

	"ottodot-trial-booking/backend/internal/database"
)

const (
	primaryAddress = "postgres://ottodot:pw@127.0.0.1:5432/ottodot?sslmode=disable"
	replicaAddress = "postgres://ottodot:pw@127.0.0.1:5433/ottodot?sslmode=disable"
)

func usableSettings() database.Settings {
	return database.Settings{
		PrimaryURL:     primaryAddress,
		ReplicaURL:     replicaAddress,
		MaxConnections: 10,
		ConnectTimeout: 5 * time.Second,
	}
}

func TestBuildPoolConfigAppliesTheSettings(t *testing.T) {
	settings := usableSettings()

	poolConfig, err := database.BuildPoolConfig(settings.PrimaryURL, settings)
	if err != nil {
		t.Fatalf("expected the address to parse, got: %v", err)
	}

	if poolConfig.MaxConns != settings.MaxConnections {
		t.Fatalf("expected %d connections, got %d", settings.MaxConnections, poolConfig.MaxConns)
	}

	if poolConfig.ConnConfig.ConnectTimeout != settings.ConnectTimeout {
		t.Fatalf("expected a %s timeout, got %s",
			settings.ConnectTimeout, poolConfig.ConnConfig.ConnectTimeout)
	}

	if poolConfig.ConnConfig.Port != 5432 {
		t.Fatalf("expected port 5432 from the address, got %d", poolConfig.ConnConfig.Port)
	}
}

func TestPrimaryAndReplicaStayDistinct(t *testing.T) {
	// The one mistake this package exists to prevent: a deciding read reaching
	// the replica because both pools were built from the same address.
	settings := usableSettings()

	primary, err := database.BuildPoolConfig(settings.PrimaryURL, settings)
	if err != nil {
		t.Fatalf("primary address did not parse: %v", err)
	}

	replica, err := database.BuildPoolConfig(settings.ReplicaURL, settings)
	if err != nil {
		t.Fatalf("replica address did not parse: %v", err)
	}

	if primary.ConnConfig.Port == replica.ConnConfig.Port {
		t.Fatal("both pools resolved to the same port, they must be separate servers")
	}
}

func TestBuildPoolConfigEdgeCases(t *testing.T) {
	cases := []struct {
		name     string
		address  string
		mutate   func(settings *database.Settings)
		fragment string
	}{
		{
			name:     "edge: an empty address",
			address:  "",
			fragment: "the connection url is empty",
		},
		{
			name:     "edge: an address that cannot be parsed",
			address:  "postgres://ottodot:pw@127.0.0.1:not-a-port/ottodot",
			fragment: "cannot be parsed",
		},
		{
			name:    "edge: a pool with no connections",
			address: primaryAddress,
			mutate: func(settings *database.Settings) {
				settings.MaxConnections = 0
			},
			fragment: "it must be at least 1",
		},
		{
			name:    "edge: a connect timeout of zero",
			address: primaryAddress,
			mutate: func(settings *database.Settings) {
				settings.ConnectTimeout = 0
			},
			fragment: "connect timeout must be greater than zero",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			settings := usableSettings()

			if testCase.mutate != nil {
				testCase.mutate(&settings)
			}

			_, err := database.BuildPoolConfig(testCase.address, settings)
			if err == nil {
				t.Fatal("expected the settings to be refused, they were accepted")
			}

			if !strings.Contains(err.Error(), testCase.fragment) {
				t.Fatalf("expected %q in the error, got: %v", testCase.fragment, err)
			}
		})
	}
}

func TestAnUnparseableAddressNeverEchoesItsPassword(t *testing.T) {
	const password = "correct-horse-battery-staple"

	settings := usableSettings()
	broken := "postgres://ottodot:" + password + "@127.0.0.1:not-a-port/ottodot"

	_, err := database.BuildPoolConfig(broken, settings)
	if err == nil {
		t.Fatal("expected the address to be refused")
	}

	if strings.Contains(err.Error(), password) {
		t.Fatalf("the password reached the error text: %v", err)
	}
}

func TestCloseIsSafeOnAnEmptyPools(t *testing.T) {
	// Open closes what it built when the second pool fails, so Close has to
	// tolerate a half built value and a nil receiver.
	var pools *database.Pools

	pools.Close()
}
