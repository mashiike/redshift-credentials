package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"

	"encoding/json"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/fatih/color"
	"github.com/fujiwara/logutils"
	redshiftcredentials "github.com/mashiike/redshift-credentials"
	"gopkg.in/yaml.v3"
)

func main() {
	filter := &logutils.LevelFilter{
		Levels: []logutils.LogLevel{"debug", "info", "notice", "warn", "error"},
		ModifierFuncs: []logutils.ModifierFunc{
			logutils.Color(color.FgHiBlack),
			nil,
			logutils.Color(color.FgHiBlue),
			logutils.Color(color.FgYellow),
			logutils.Color(color.FgRed, color.BgBlack),
		},
		MinLevel: "info",
		Writer:   os.Stderr,
	}
	log.SetOutput(filter)
	var (
		minLevel          string
		endpoint          string
		workgroupName     string
		clusterIdentifier string
		dbUser            string
		dbName            string
		prefix            string
		format            string
		durationSeconds   int
	)
	flag.StringVar(&minLevel, "log-level", "info", "redshift-credentials log level")
	flag.StringVar(&endpoint, "endpoint", "", "redshift endpoint url")
	flag.StringVar(&workgroupName, "workgroup", "", "redshift serverless workgroup name")
	flag.StringVar(&clusterIdentifier, "cluster", "", "redshift provisioned cluster identifier")
	flag.StringVar(&dbUser, "db-user", "", "redshift database user name (provisioned only)")
	flag.StringVar(&dbName, "db-name", "", "redshift database name")
	flag.IntVar(&durationSeconds, "duration-seconds", 0, "number of seconds until the returned temporary password expires (900 ~ 3600)")
	flag.StringVar(&prefix, "prefix", "REDSHIFT_", "Prefixes environment variable names when writing to environment variables (e.g., `REDSHIFT_`)")
	flag.StringVar(&format, "output", "env", "Specifies the output format of the credential when not driven in wrapper mode. If not specified, the output is formatted for environment variable settings. [env|json|yaml]")
	flag.Parse()
	filter.SetMinLevel(logutils.LogLevel(strings.ToLower(minLevel)))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	awsCfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		log.Fatalf("[error] failed to load default aws config, %v", err)
	}

	client := redshiftcredentials.NewFromConfig(awsCfg, func(o *redshiftcredentials.Options) {
		o.Logger = log.Default()
	})
	output, err := client.GetCredentials(ctx, &redshiftcredentials.GetCredentialsInput{
		Endpoint:          nilIfEmpty(endpoint),
		WorkgroupName:     nilIfEmpty(workgroupName),
		ClusterIdentifier: nilIfEmpty(clusterIdentifier),
		DbName:            nilIfEmpty(dbName),
		DbUser:            nilIfEmpty(dbUser),
		DurationSeconds:   nilIfEmpty((int32)(durationSeconds)),
	})
	if err != nil {
		log.Fatalf("[error] failed to get redshift credentials, %v", err)
	}

	if flag.NArg() == 0 {
		switch format {
		case "json":
			encoder := json.NewEncoder(os.Stdout)
			encoder.SetIndent("", "  ")
			if err := encoder.Encode(output); err != nil {
				log.Fatalf("[error] failed to encode output, %v", err)
			}
		case "yaml", "yml":
			encoder := yaml.NewEncoder(os.Stdout)
			if err := encoder.Encode(output); err != nil {
				log.Fatalf("[error] failed to encode output, %v", err)
			}
		default:
			if output.Endpoint != nil {
				fmt.Printf("export %sENDPOINT=%s\n", prefix, *output.Endpoint)
			}
			fmt.Printf("export %sDB_PASSWORD=%s\n", prefix, *output.DbPassword)
			fmt.Printf("export %sDB_USER=%s\n", prefix, *output.DbUser)
			fmt.Printf("export %sEXPIRATION=%s\n", prefix, output.Expiration.Format(time.RFC3339Nano))
			if output.NextRefreshTime != nil {
				fmt.Printf("export %sNEXT_REFRESH_TIME=%s", prefix, output.NextRefreshTime.Format(time.RFC3339Nano))
			}
		}
		return
	}
	env := os.Environ()
	if output.Endpoint != nil {
		env = append(env, fmt.Sprintf("%sENDPOINT=%s", prefix, *output.Endpoint))
	}
	env = append(env, fmt.Sprintf("%sDB_PASSWORD=%s", prefix, *output.DbPassword))
	env = append(env, fmt.Sprintf("%sDB_USER=%s", prefix, *output.DbUser))
	env = append(env, fmt.Sprintf("%sEXPIRATION=%s", prefix, output.Expiration.Format(time.RFC3339Nano)))
	if output.NextRefreshTime != nil {
		env = append(env, fmt.Sprintf("%sNEXT_REFRESH_TIME=%s", prefix, output.NextRefreshTime.Format(time.RFC3339Nano)))
	}
	args := flag.Args()
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = env
	if err := cmd.Run(); err != nil {
		log.Fatalf("[error] command runtime error, %v", err)
	}
}

func nilIfEmpty[T comparable](t T) *T {
	var empty T
	if t == empty {
		return nil
	}
	return &t
}
