package redshiftcredentials

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/url"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/redshift"
	"github.com/aws/aws-sdk-go-v2/service/redshiftserverless"
	"github.com/aws/smithy-go"
)

type Client struct {
	logger      *log.Logger
	filter      func([]string) (string, error)
	provisioned *redshift.Client
	serverless  *redshiftserverless.Client
}

type Options struct {
	Filter             func([]string) (string, error)
	Logger             *log.Logger
	ProvisionedOptions *redshift.Options
	ServerlessOptions  *redshiftserverless.Options
}

func NewFromConfig(cfg aws.Config, optFns ...func(*Options)) *Client {
	provisionedOptFns := make([]func(*redshift.Options), 0, len(optFns))
	serverlessOptFns := make([]func(*redshiftserverless.Options), 0, len(optFns))
	opts := &Options{
		Logger: log.New(io.Discard, log.Default().Prefix(), log.Default().Flags()),
	}
	for _, optFn := range optFns {
		_optFn := optFn
		_optFn(opts)
		provisionedOptFns = append(provisionedOptFns, func(o *redshift.Options) {
			opts := &Options{
				ProvisionedOptions: o,
				ServerlessOptions:  &redshiftserverless.Options{},
			}
			_optFn(opts)
		})
		serverlessOptFns = append(serverlessOptFns, func(o *redshiftserverless.Options) {
			opts := &Options{
				ProvisionedOptions: &redshift.Options{},
				ServerlessOptions:  o,
			}
			_optFn(opts)
		})
	}

	return &Client{
		logger:      opts.Logger,
		filter:      opts.Filter,
		provisioned: redshift.NewFromConfig(cfg, provisionedOptFns...),
		serverless:  redshiftserverless.NewFromConfig(cfg, serverlessOptFns...),
	}
}

type GetCredentialsInput struct {
	Endpoint          *string
	WorkgroupName     *string
	ClusterIdentifier *string
	DbUser            *string
	DbName            *string
	DurationSeconds   *int32
	address           *string
	port              *string
}

type GetCredentialsOutput struct {
	WorkgroupName     *string    `json:",omitempty" yaml:"workgroup_name,omitempty"`
	ClusterIdentifier *string    `json:",omitempty" yaml:"cluster_identifier,omitempty"`
	Endpoint          *string    `json:",omitempty" yaml:"endpoint,omitempty"`
	Port              *string    `json:",omitempty" yaml:"port,omitempty"`
	DbPassword        *string    `json:",omitempty" yaml:"db_password,omitempty"`
	DbUser            *string    `json:",omitempty" yaml:"db_user,omitempty"`
	Expiration        *time.Time `json:",omitempty" yaml:"expiration,omitempty"`
	NextRefreshTime   *time.Time `json:",omitempty" yaml:"next_refresh_time,omitempty"`
}

type redshiftListItem struct {
	DBMasterUser  *string
	InitialDBName *string
	Type          string
	Identifier    string
	Address       string
	Port          string
}

func (item redshiftListItem) String() string {
	return fmt.Sprintf("%s\t%s", item.Identifier, item.Type)
}

const (
	redshiftListItemProvisioned = "provisioned cluster"
	redshiftListItemServerless  = "serverless workgroup"
)

func (client *Client) GetCredentials(ctx context.Context, input *GetCredentialsInput) (*GetCredentialsOutput, error) {
	if input.Endpoint != nil {
		u, err := url.Parse(*input.Endpoint)
		if err != nil {
			return nil, fmt.Errorf("endpoint can not parse as URL, %w", err)
		}
		parts := strings.Split(u.Host, ".")
		if strings.HasSuffix(u.Host, "redshift.amazonaws.com") {
			input.ClusterIdentifier = aws.String(parts[0])
		}
		if strings.HasSuffix(u.Host, "redshift-serverless.amazonaws.com") {
			input.WorkgroupName = aws.String(parts[0])
		}
		if input.DbName == nil {
			input.DbName = aws.String(strings.TrimLeft(u.Path, "/"))
		}
		input.address = aws.String(u.Host)
		input.port = aws.String(u.Port())
	}
	if input.WorkgroupName == nil && input.ClusterIdentifier == nil {
		redshiftList := make([]redshiftListItem, 0)
		pp := redshift.NewDescribeClustersPaginator(client.provisioned, &redshift.DescribeClustersInput{})
		for pp.HasMorePages() {
			output, err := pp.NextPage(ctx)
			if err != nil {
				var ae smithy.APIError
				if !errors.As(err, &ae) {
					return nil, err
				}
				if !strings.HasPrefix(ae.ErrorCode(), "AccessDenied") {
					return nil, ae
				}
				client.logger.Println("[warn] Assume that the Redshift provisioned cluster does not exist because redshift:DescribeClusters is AccessDenied")
				break
			}
			for _, cluster := range output.Clusters {
				item := redshiftListItem{
					Type:          redshiftListItemProvisioned,
					Identifier:    *cluster.ClusterIdentifier,
					DBMasterUser:  cluster.MasterUsername,
					InitialDBName: cluster.DBName,
					Address:       *cluster.Endpoint.Address,
					Port:          fmt.Sprintf("%d", cluster.Endpoint.Port),
				}
				client.logger.Printf("[debug] %s is found", item)
				redshiftList = append(redshiftList, item)
			}
		}
		if input.DbUser == nil {
			sp := redshiftserverless.NewListWorkgroupsPaginator(client.serverless, &redshiftserverless.ListWorkgroupsInput{})
			for sp.HasMorePages() {
				output, err := sp.NextPage(ctx)
				if err != nil {
					var ae smithy.APIError
					if !errors.As(err, &ae) {
						return nil, err
					}
					if !strings.HasPrefix(ae.ErrorCode(), "AccessDenied") {
						return nil, ae
					}
					client.logger.Println("[warn] Assume that the Redshift serverless workgroup does not exist because redshift-serverless:ListWorkgroups is AccessDenied")
					break
				}
				for _, workgroup := range output.Workgroups {
					item := redshiftListItem{
						Type:       redshiftListItemServerless,
						Identifier: *workgroup.WorkgroupName,
						Address:    *workgroup.Endpoint.Address,
						Port:       fmt.Sprintf("%d", *workgroup.Endpoint.Port),
					}
					client.logger.Printf("[debug] %s is found", item)
					redshiftList = append(redshiftList, item)
				}
			}
		}
		client.logger.Printf("[debug] redshift %d found", len(redshiftList))
		if len(redshiftList) == 0 {
			return nil, fmt.Errorf("input parameters Endpoint, WorkgroupName and ClusterIdentifier were not given, and Redshift search and could not find them")
		}
		selected := redshiftList[0]
		if len(redshiftList) > 1 {
			if client.filter == nil {
				return nil, fmt.Errorf("automatic selection was not possible because %d redshifts were found", len(redshiftList))
			}
			items := make(map[string]redshiftListItem, len(redshiftList))
			lines := make([]string, 0, len(redshiftList))
			for i, item := range redshiftList {
				line := fmt.Sprintf("[%d] %s\t%s", i+1, item.String(), item.Address)
				items[line] = item
				lines = append(lines, line)
			}
			selectedLine, err := client.filter(lines)
			if err != nil {
				return nil, fmt.Errorf("manual selection was failed, %v", err)
			}
			var ok bool
			selected, ok = items[selectedLine]
			if !ok {
				return nil, fmt.Errorf("manual selection was failed, filter return invalid line")
			}
			client.logger.Printf("[debug] redshift %s selected", selected)
		}
		switch selected.Type {
		case redshiftListItemProvisioned:
			input.ClusterIdentifier = &selected.Identifier
			if input.DbUser == nil {
				input.DbUser = selected.DBMasterUser
			}
			if input.DbName == nil {
				input.DbName = selected.InitialDBName
			}
		case redshiftListItemServerless:
			input.WorkgroupName = &selected.Identifier
		}
		input.address = &selected.Address
		input.port = &selected.Port
	}
	if input.ClusterIdentifier != nil {
		return client.getCredentialsForProvisioned(ctx, input)
	}
	if input.WorkgroupName != nil {
		return client.getCredentialsForServerless(ctx, input)
	}

	return nil, errors.New("not implemented yet")
}

func (client *Client) getCredentialsForProvisioned(ctx context.Context, input *GetCredentialsInput) (*GetCredentialsOutput, error) {
	if input.DbUser == nil {
		clusters, err := client.provisioned.DescribeClusters(ctx, &redshift.DescribeClustersInput{
			ClusterIdentifier: input.ClusterIdentifier,
		})
		if err != nil {
			return nil, err
		}
		if len(clusters.Clusters) == 0 {
			return nil, fmt.Errorf("cluster `%s` is not found", *input.ClusterIdentifier)
		}
		input.DbUser = clusters.Clusters[0].MasterUsername
		input.address = clusters.Clusters[0].Endpoint.Address
		input.port = aws.String(fmt.Sprintf("%d", clusters.Clusters[0].Endpoint.Port))
	}
	output, err := client.provisioned.GetClusterCredentials(ctx, &redshift.GetClusterCredentialsInput{
		ClusterIdentifier: input.ClusterIdentifier,
		DbUser:            input.DbUser,
		DbName:            input.DbName,
		DurationSeconds:   input.DurationSeconds,
	})
	if err != nil {
		return nil, err
	}
	if input.address == nil {
		clusters, err := client.provisioned.DescribeClusters(ctx, &redshift.DescribeClustersInput{
			ClusterIdentifier: input.ClusterIdentifier,
		})
		if err != nil {
			var ae smithy.APIError
			if errors.As(err, &ae) && strings.HasPrefix(ae.ErrorCode(), "AccessDenied") {
				client.logger.Println("[debug] failed to endpoint info  because redshift:DescribeClusters is AccessDenied")
			} else {
				client.logger.Printf("[debug] failed to redshift:DescribeClusters, %v", err)
			}
		} else {
			if len(clusters.Clusters) != 0 {
				input.address = clusters.Clusters[0].Endpoint.Address
				input.port = aws.String(fmt.Sprintf("%d", clusters.Clusters[0].Endpoint.Port))
			}
		}

	}
	return &GetCredentialsOutput{
		ClusterIdentifier: input.ClusterIdentifier,
		Endpoint:          input.address,
		Port:              input.port,
		DbPassword:        output.DbPassword,
		DbUser:            output.DbUser,
		Expiration:        output.Expiration,
		NextRefreshTime:   nil,
	}, nil
}

func (client *Client) getCredentialsForServerless(ctx context.Context, input *GetCredentialsInput) (*GetCredentialsOutput, error) {
	output, err := client.serverless.GetCredentials(ctx, &redshiftserverless.GetCredentialsInput{
		WorkgroupName:   input.WorkgroupName,
		DbName:          input.DbName,
		DurationSeconds: input.DurationSeconds,
	})
	if err != nil {
		return nil, err
	}
	if input.address == nil {
		workgroup, err := client.serverless.GetWorkgroup(ctx, &redshiftserverless.GetWorkgroupInput{
			WorkgroupName: input.WorkgroupName,
		})
		if err != nil {
			var ae smithy.APIError
			if errors.As(err, &ae) && strings.HasPrefix(ae.ErrorCode(), "AccessDenied") {
				client.logger.Println("[debug] failed to endpoint info  because redshift-serverless:GetWorkgroup is AccessDenied")
			} else {
				client.logger.Printf("[debug] failed to redshift-serverless:GetWorkgroup, %v", err)
			}
		} else {
			input.address = workgroup.Workgroup.Endpoint.Address
			input.port = aws.String(fmt.Sprintf("%d", *workgroup.Workgroup.Endpoint.Port))
		}
	}
	return &GetCredentialsOutput{
		WorkgroupName:   input.WorkgroupName,
		Endpoint:        input.address,
		Port:            input.port,
		DbPassword:      output.DbPassword,
		DbUser:          output.DbUser,
		Expiration:      output.Expiration,
		NextRefreshTime: output.NextRefreshTime,
	}, nil
}
