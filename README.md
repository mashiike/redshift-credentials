# redshift-credentials

[![Documentation](https://godoc.org/github.com/mashiike/awstee?status.svg)](https://godoc.org/github.com/mashiike/awstee)
![Latest GitHub release](https://img.shields.io/github/release/mashiike/awstee.svg)
![Github Actions test](https://github.com/mashiike/awstee/workflows/Test/badge.svg?branch=main)
[![License](https://img.shields.io/badge/license-MIT-blue.svg)](https://github.com/mashiike/awstee/blob/master/LICENSE)

a command line tool for Amazon Redshift temporary authorization with AWS IAM 

### Usage 

#### as Basically

```shell 
$ redshift-credentials --endpoint default.012345678910.ap-northeast-1.redshift-serverless.amazonaws.com --output json    
{
  "WorkgroupName": "default",
  "Endpoint": "default.012345678910.ap-northeast-1.redshift-serverless.amazonaws.com",
  "Port": "5439",
  "DbPassword": "<temporary password>",
  "DbUser": "<temporary user>",
  "Expiration": "2022-07-16T12:59:09.059Z",
  "NextRefreshTime": "2022-07-16T14:44:09.059Z"
}
```
The endpoints can be from a provisioned cluster.

If you do not specify anything, you will be asked to which Redshift you want to temporarily authenticate
```shell
$ redshift-credentials
$ go run main.go                                                                                               
[1] default     provisioned cluster     default.xxxxxxxxxxxx.ap-northeast-1.redshift.amazonaws.com
[2] default     serverless workgroup    default.012345678910.ap-northeast-1.redshift-serverless.amazonaws.com
Enter number: 
```

For more convenient use
```shell
$ eval `redshift-credentials --endpoint default.012345678910.ap-northeast-1.redshift-serverless.amazonaws.com`
$ export PGPASSWORD=$REDSHOFT_DB_PASSWORD
$ psql -U $REDSHIFT_DB_USER -h $REDSHIFT_ENDPOINT -p $REDSHIFT_PORT -W dev
```

#### as Wrapper


```shell 
$ redshift-credentials --endpoint default.012345678910.ap-northeast-1.redshift-serverless.amazonaws.com -- your_application_command    
```

It also works as a wrapper command.

In this case, the retrieved temporary authentication information is passed as the following environment variable

- $REDSHOFT_DB_PASSWORD
- $REDSHOFT_DB_USER 
- $REDSHOFT_ENDPOINT 
- $REDSHOFT_PORT


### Install 
#### Homebrew (macOS and Linux)

```console
$ brew install mashiike/tap/redshift-credentials
```
#### Binary packages

[Releases](https://github.com/mashiike/redshift-credentials/releases)

### Options 

```
redshift-credentials is a command-like tool for Amazon Redshift temporary authorization
version: current

usage: redshift-credentials [options] [-- user command]

  -cluster string
        redshift provisioned cluster identifier
  -db-name string
        redshift database name
  -db-user string
        redshift database user name (provisioned only)
  -duration-seconds int
        number of seconds until the returned temporary password expires (900 ~ 3600)
  -endpoint string
        redshift endpoint url
  -log-level string
        redshift-credentials log level (default "info")
  -output string
        Specifies the output format of the credential when not driven in wrapper mode. If not specified, the output is formatted for environment variable settings. [env|json|yaml] (default "env")
  -prefix REDSHIFT_
        Prefixes environment variable names when writing to environment variables (e.g., REDSHIFT_) (default "REDSHIFT_")
  -workgroup string
        redshift serverless workgroup name
```

## LICENSE

MIT License

Copyright (c) 2022 IKEDA Masashi
