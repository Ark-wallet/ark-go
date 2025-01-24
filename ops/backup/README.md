# S3 Backup service

## Description

This service periodically compresses with gZIP a target directory into a tar archive and pushes it to the specified S3 bucket on AWS.

## Usage

### Configuration

The service can be configured using environment variables:
- `AWS_ACCESS_KEY_ID`: The account id of the AWS user
- `AWS_SECRET_ACCESS_KEY`: The secret access key of the AWS user
- `AWS_REGION`: The region where the bucket is located
- `S3_BUCKET_NAME`: The name of the bucket where to push the tar archive
- `TARGET_DATADIR`: The path to the target datadir to backup
- `BACKUP_INTERVAL_IN_MINUTES`: The interval for executing the backup expressed in minutes (defaults to 5min)

NOTE:  
If the bucket doesn't exist, the service attempts to create it, therefore the AWS user is required to have full access rights to S3.

### Local Development

Build and run the service:

```bash
make run AWS_ACCESS_KEY_ID=key AWS_SECRET_ACCESS_KEY=secret S3_BUCKET_NAME=myapp-backup-dev LOCAL_PATH=path/to/datadir
```

... or run it with Docker:

```bash
make docker-run AWS_ACCESS_KEY_ID=key AWS_SECRET_ACCESS_KEY=secret S3_BUCKET_NAME=myapp-backup-dev LOCAL_PATH=path/to/datadir
```

You can ether specify a volume instead of a local folder:

```bash
make docker-run AWS_ACCESS_KEY_ID=key AWS_SECRET_ACCESS_KEY=secret S3_BUCKET_NAME=myapp-backup-dev DATA_VOLUME=myvolume
```
