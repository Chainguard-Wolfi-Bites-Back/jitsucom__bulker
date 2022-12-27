package clickhouse_noshards

import (
	"context"
	"database/sql"
	"fmt"
	"github.com/jitsucom/bulker/base/logging"
	"github.com/testcontainers/testcontainers-go"
	tcWait "github.com/testcontainers/testcontainers-go/wait"
)

const (
	chClusterDatabase               = "default"
	chClusterCluster                = "company_cluster"
	chClusterDatasourceTemplateHttp = "http://default:@localhost:8133,localhost:8134/default?read_timeout=5m&mutations_sync=2&connection_open_strategy=round_robin"
	chClusterDatasourceTemplate     = "clickhouse://default:@localhost:9010,localhost:9011/default?read_timeout=5m&mutations_sync=2&connection_open_strategy=round_robin"
)

// ClickHouseClusterContainerNoShards is a ClickHouse testcontainer
type ClickHouseClusterContainerNoShards struct {
	datasource *sql.DB
	Container  testcontainers.Container
	Compose    *testcontainers.LocalDockerCompose
	Context    context.Context

	Cluster  string
	Dsns     []string
	DsnsHTTP []string
	Database string
}

// ClickHouseClusterContainerNoShards creates new Clickhouse test container if CH_TEST_PORT is not defined. Otherwise uses db at defined port.
// This logic is required for running test at CI environment
func NewClickHouseClusterContainerNoShards(ctx context.Context) (*ClickHouseClusterContainerNoShards, error) {
	composeFilePaths := []string{"testcontainers/clickhouse_noshards/docker-compose.yml"}
	identifier := "bulker_clickhouse_cluster_noshards_compose"

	compose := testcontainers.NewLocalDockerCompose(composeFilePaths, identifier)
	execError := compose.Down()
	err := execError.Error
	if err != nil {
		logging.Errorf("couldnt down docker compose: %s : %v", compose.Identifier, err)
	}

	compose = testcontainers.NewLocalDockerCompose(composeFilePaths, identifier)
	execError = compose.
		WithCommand([]string{"up", "-d"}).
		WaitForService("clickhouse2_01", tcWait.ForListeningPort("9000/tcp")).
		WaitForService("clickhouse2_02", tcWait.ForListeningPort("9000/tcp")).
		Invoke()
	err = execError.Error
	if err != nil {
		return nil, fmt.Errorf("could not run compose file: %v - %v", composeFilePaths, err)
	}

	datasource, err := sql.Open("clickhouse", chClusterDatasourceTemplate)
	if err != nil {
		execError := compose.Down()
		err := execError.Error
		if err != nil {
			return nil, fmt.Errorf("could not run compose file: %v - %v", composeFilePaths, err)
		}
		return nil, err
	}

	return &ClickHouseClusterContainerNoShards{
		datasource: datasource,
		Compose:    compose,
		Context:    ctx,
		Dsns:       []string{chClusterDatasourceTemplate},
		DsnsHTTP:   []string{chClusterDatasourceTemplateHttp},
		Database:   chClusterDatabase,
		Cluster:    chClusterCluster,
	}, nil
}

// Close terminates underlying docker container
func (ch *ClickHouseClusterContainerNoShards) Close() error {
	if ch.Compose != nil {
		execError := ch.Compose.Down()
		err := execError.Error
		if err != nil {
			return fmt.Errorf("could down docker compose: %s", ch.Compose.Identifier)
		}
	}

	if ch.datasource != nil {
		if err := ch.datasource.Close(); err != nil {
			logging.Errorf("failed to close datasource in clickhouse container: %v", err)
		}
	}

	return nil
}