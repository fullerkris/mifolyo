package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"regexp"
	"syscall"
	"time"

	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

var sha256Pattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

const allowInsecureDatastoresEnvironment = "ALLOW_INSECURE_DATASTORES"

type commandOptions struct {
	Publish             bool
	ExpectedGraphSHA256 string
	ConfirmTarget       string
}

type commandSummary struct {
	Status                  string  `json:"status"`
	RunID                   string  `json:"run_id,omitempty"`
	MongoDatabase           string  `json:"mongo_database"`
	GraphSHA256             string  `json:"graph_sha256"`
	AlgorithmVersion        string  `json:"algorithm_version"`
	CanonicalizationVersion int     `json:"canonicalization_version"`
	Damping                 float64 `json:"damping"`
	Tolerance               float64 `json:"tolerance"`
	MaxIterations           int     `json:"max_iterations"`
	Iterations              int     `json:"iterations"`
	Residual                float64 `json:"residual"`
	RankSum                 float64 `json:"rank_sum"`
	graphStats
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, output io.Writer) error {
	commandOptions, err := parseCommandOptions(args)
	if err != nil {
		return err
	}
	if commandOptions.Publish {
		if err := validatePublicationEnvironment(commandOptions); err != nil {
			return err
		}
	}

	client, database, err := connectMongo(ctx)
	if err != nil {
		return err
	}
	defer func() {
		disconnectContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = client.Disconnect(disconnectContext)
	}()

	var lock publicationLock
	lockHeld := false
	if commandOptions.Publish {
		lock, err = acquirePublicationLock(ctx, database)
		if err != nil {
			return err
		}
		lockHeld = true
		defer func() {
			if !lockHeld {
				return
			}
			releaseContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			_ = lock.release(releaseContext, database)
		}()
	}

	input, err := loadGraph(ctx, database)
	if err != nil {
		return err
	}
	result, err := calculatePageRank(input)
	if err != nil {
		return err
	}

	summary := commandSummary{
		Status:                  "validated",
		MongoDatabase:           database.Name(),
		GraphSHA256:             input.SHA256,
		AlgorithmVersion:        algorithmVersion,
		CanonicalizationVersion: canonicalizationVersion,
		Damping:                 pageRankDamping,
		Tolerance:               pageRankTolerance,
		MaxIterations:           pageRankMaxIterations,
		Iterations:              result.Iterations,
		Residual:                result.Residual,
		RankSum:                 result.Sum,
		graphStats:              input.Stats,
	}

	if commandOptions.Publish {
		if commandOptions.ExpectedGraphSHA256 != input.SHA256 {
			return fmt.Errorf(
				"pagerank: expected graph SHA-256 %s, loaded %s",
				commandOptions.ExpectedGraphSHA256,
				input.SHA256,
			)
		}

		reloadedInput, err := loadGraph(ctx, database)
		if err != nil {
			return fmt.Errorf("pagerank: revalidate graph before publication: %w", err)
		}
		if reloadedInput.SHA256 != input.SHA256 {
			return fmt.Errorf("pagerank: graph changed between validation and publication")
		}

		publication, err := publishPageRank(ctx, client, database, input, result)
		if err != nil {
			var unknownOutcome *activationOutcomeUnknownError
			if errors.As(err, &unknownOutcome) {
				lockHeld = false
			}
			return err
		}
		summary.Status = publication.Status
		summary.RunID = publication.RunID

		releaseContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := lock.release(releaseContext, database); err != nil {
			return err
		}
		lockHeld = false
	}

	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(summary); err != nil {
		return fmt.Errorf("pagerank: encode summary: %w", err)
	}
	return nil
}

func parseCommandOptions(args []string) (commandOptions, error) {
	flags := flag.NewFlagSet("page-rank", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	publish := flags.Bool("publish", false, "publish the validated PageRank generation")
	expectedGraphSHA256 := flags.String("expected-graph-sha256", "", "required validated graph hash for publication")
	confirmTarget := flags.String("confirm-target", "", "required host:port/database/pagerank publication target")
	if err := flags.Parse(args); err != nil {
		return commandOptions{}, fmt.Errorf("pagerank: parse arguments: %w", err)
	}
	if flags.NArg() != 0 {
		return commandOptions{}, fmt.Errorf("pagerank: unexpected positional arguments")
	}
	if *publish && !sha256Pattern.MatchString(*expectedGraphSHA256) {
		return commandOptions{}, fmt.Errorf("pagerank: --publish requires a lowercase 64-character --expected-graph-sha256")
	}
	if *publish && *confirmTarget == "" {
		return commandOptions{}, fmt.Errorf("pagerank: --publish requires --confirm-target")
	}
	if !*publish && *expectedGraphSHA256 != "" {
		return commandOptions{}, fmt.Errorf("pagerank: --expected-graph-sha256 requires --publish")
	}
	if !*publish && *confirmTarget != "" {
		return commandOptions{}, fmt.Errorf("pagerank: --confirm-target requires --publish")
	}
	return commandOptions{
		Publish:             *publish,
		ExpectedGraphSHA256: *expectedGraphSHA256,
		ConfirmTarget:       *confirmTarget,
	}, nil
}

func connectMongo(ctx context.Context) (*mongo.Client, *mongo.Database, error) {
	host := getEnv("MONGO_HOST", "localhost")
	port := getEnv("MONGO_PORT", "27017")
	databaseName := getEnv("MONGO_DB", "test")
	username := getEnv("MONGO_USERNAME", "")
	password := getEnv("MONGO_PASSWORD", "")
	if host == "" || port == "" || databaseName == "" {
		return nil, nil, errors.New("pagerank: MongoDB host, port, and database must be non-empty")
	}
	if err := validateMongoAuthentication(
		username,
		password,
		os.Getenv(allowInsecureDatastoresEnvironment) == "true",
	); err != nil {
		return nil, nil, err
	}

	clientOptions := options.Client().
		ApplyURI("mongodb://" + net.JoinHostPort(host, port)).
		SetConnectTimeout(10 * time.Second).
		SetServerSelectionTimeout(10 * time.Second)
	if username != "" {
		clientOptions.SetAuth(options.Credential{
			AuthSource: "admin",
			Username:   username,
			Password:   password,
		})
	}

	client, err := mongo.Connect(clientOptions)
	if err != nil {
		return nil, nil, fmt.Errorf("pagerank: connect to MongoDB: %w", err)
	}
	if err := client.Ping(ctx, nil); err != nil {
		_ = client.Disconnect(context.Background())
		return nil, nil, fmt.Errorf("pagerank: ping MongoDB: %w", err)
	}
	return client, client.Database(databaseName), nil
}

func validateMongoAuthentication(username, password string, allowInsecure bool) error {
	if (username == "") != (password == "") {
		return errors.New("pagerank: MONGO_USERNAME and MONGO_PASSWORD must be set together")
	}
	if username == "" && !allowInsecure {
		return errors.New("pagerank: MongoDB authentication is required unless ALLOW_INSECURE_DATASTORES=true is explicitly set for local testing")
	}
	return nil
}

func validatePublicationEnvironment(commandOptions commandOptions) error {
	host, hostExists := os.LookupEnv("MONGO_HOST")
	port, portExists := os.LookupEnv("MONGO_PORT")
	databaseName, exists := os.LookupEnv("MONGO_DB")
	if !hostExists || host == "" || !portExists || port == "" || !exists || databaseName == "" {
		return errors.New("pagerank: publication requires explicit MONGO_HOST, MONGO_PORT, and MONGO_DB")
	}
	switch databaseName {
	case "admin", "config", "local":
		return fmt.Errorf("pagerank: publication to MongoDB system database %q is forbidden", databaseName)
	default:
		target := net.JoinHostPort(host, port) + "/" + databaseName + "/" + pageRankCollection
		if commandOptions.ConfirmTarget != target {
			return fmt.Errorf("pagerank: --confirm-target must exactly equal %q", target)
		}
		return nil
	}
}

func getEnv(key, fallback string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return fallback
}
