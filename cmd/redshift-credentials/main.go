package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/Songmu/prompter"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/fatih/color"
	"github.com/fujiwara/logutils"
	redshiftcredentials "github.com/mashiike/redshift-credentials"
	"github.com/pkg/errors"
	"gopkg.in/yaml.v3"
)

var (
	Version = "current"
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
	flag.CommandLine.Usage = func() {
		fmt.Fprintln(flag.CommandLine.Output(), "redshift-credentials is a command-like tool for Amazon Redshift temporary authorization")
		fmt.Fprintln(flag.CommandLine.Output(), "version:", Version)
		fmt.Fprintln(flag.CommandLine.Output(), "")
		fmt.Fprintln(flag.CommandLine.Output(), "usage: redshift-credentials [options] [-- user command]")
		fmt.Fprintln(flag.CommandLine.Output(), "")
		flag.CommandLine.PrintDefaults()
	}
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
		o.Filter = runFilter
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
			if output.ClusterIdentifier != nil {
				fmt.Printf("export %sPROVISIONED_CLUSTER=%s\n", prefix, *output.ClusterIdentifier)
			}
			if output.WorkgroupName != nil {
				fmt.Printf("export %sSERVERLESS_WORKGROUP=%s\n", prefix, *output.WorkgroupName)
			}
			if output.Endpoint != nil {
				fmt.Printf("export %sHOST=%s\n", prefix, *output.Endpoint)
			}
			if output.Port != nil {
				fmt.Printf("export %sPORT=%s\n", prefix, *output.Port)
			}
			fmt.Printf("export %sPASSWORD=%s\n", prefix, *output.DbPassword)
			fmt.Printf("export %sUSER=%s\n", prefix, *output.DbUser)
			fmt.Printf("export %sEXPIRATION=%s\n", prefix, output.Expiration.Format(time.RFC3339Nano))
			if output.NextRefreshTime != nil {
				fmt.Printf("export %sNEXT_REFRESH_TIME=%s", prefix, output.NextRefreshTime.Format(time.RFC3339Nano))
			}
		}
		return
	}
	env := os.Environ()
	if output.ClusterIdentifier != nil {
		env = append(env, fmt.Sprintf("%sPROVISIONED_CLUSTER=%s", prefix, *output.ClusterIdentifier))
	}
	if output.WorkgroupName != nil {
		env = append(env, fmt.Sprintf("%sSERVERLESS_WORKGROUP=%s", prefix, *output.WorkgroupName))
	}
	if output.Endpoint != nil {
		env = append(env, fmt.Sprintf("%sHOST=%s", prefix, *output.Endpoint))
	}
	if output.Port != nil {
		env = append(env, fmt.Sprintf("%sPORT=%s", prefix, *output.Port))
	}
	env = append(env, fmt.Sprintf("%sPASSWORD=%s", prefix, *output.DbPassword))
	env = append(env, fmt.Sprintf("%sUSER=%s", prefix, *output.DbUser))
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

// **********************************************************************
// The code in the following section is implemented with reference to github.com/kayac/ecspresso.
// code link: https://github.com/kayac/ecspresso/blob/86b25405f942a4ac30772c8c75c087f5bd5017f5/exec.go#L126
// Please refer to https://github.com/kayac/ecspresso/blob/v1/LICENSE for the license of the original code.
func runFilter(s []string) (string, error) {
	filterCommand := os.Getenv("FILTER")
	if filterCommand == "" {
		return runInternalFilter(s)
	}
	var f *exec.Cmd
	if strings.Contains(filterCommand, " ") {
		f = exec.Command("sh", "-c", filterCommand)
	} else {
		f = exec.Command(filterCommand)
	}
	f.Stderr = os.Stderr
	p, _ := f.StdinPipe()
	go func() {
		io.Copy(p, strings.NewReader(strings.Join(s, "\n")))
		p.Close()
	}()
	b, err := f.Output()
	if err != nil {
		return "", errors.Wrap(err, "failed to execute filter command")
	}
	return strings.TrimRight(string(b), "\n"), nil
}

// code link: https://github.com/kayac/ecspresso/blob/86b25405f942a4ac30772c8c75c087f5bd5017f5/exec.go#L126
func runInternalFilter(items []string) (string, error) {
	var input string
	title := "number"
	for _, item := range items {
		fmt.Fprintln(os.Stderr, item)
	}

	for {
		input = prompter.Prompt("Enter "+title, "")
		if input == "" {
			continue
		}
		var found []string
		for _, item := range items {
			item := item
			if item == input {
				found = []string{item}
				break
			} else if strings.HasPrefix(item, input) {
				found = append(found, item)
			} else if strings.HasPrefix(item, "["+input+"]") {
				found = append(found, item)
			}
		}

		switch len(found) {
		case 0:
			fmt.Fprintf(os.Stderr, "no such item %s\n", input)
		case 1:
			fmt.Fprintf(os.Stderr, "%s=%s\n", title, found[0])
			return found[0], nil
		default:
			fmt.Fprintf(os.Stderr, "%s is ambiguous\n", input)
		}
	}
}

// ***********************************************************************
