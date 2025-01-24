package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/go-co-op/gocron"
	"github.com/mholt/archives"
	log "github.com/sirupsen/logrus"
)

const (
	backupFileFormat      = "backup-%s.tar.gz"
	defaultBackupInterval = 5 // minutes
)

func main() {
	// Get environment variables
	awsAccessKeyID := os.Getenv("AWS_ACCESS_KEY_ID")
	awsSecretAccessKey := os.Getenv("AWS_SECRET_ACCESS_KEY")
	awsRegion := os.Getenv("AWS_REGION")
	bucketName := os.Getenv("S3_BUCKET_NAME")
	datadir := os.Getenv("TARGET_DATADIR")
	backupIntervalStr := os.Getenv("BACKUP_INTERVAL_IN_MINUTES")

	if awsAccessKeyID == "" || awsSecretAccessKey == "" || awsRegion == "" || bucketName == "" || datadir == "" {
		log.Fatal("Missing required environment variables")
	}

	log.SetLevel(log.DebugLevel)

	backupInterval := defaultBackupInterval
	if backupIntervalStr != "" {
		i, err := strconv.Atoi(backupIntervalStr)
		if err != nil {
			log.Fatalf("invalid backup interval, must be a number represening minutes: %s", err)
		}
		backupInterval = i
	}

	datadir = cleanAndExpandPath(datadir)

	log.Debug("config:")
	log.Debugf("AWS_ACCESS_KEY_ID: %s", awsAccessKeyID)
	log.Debugf("AWS_REGION: %s", awsRegion)
	log.Debugf("S3_BUCKET_NAME: %s", bucketName)
	log.Debugf("BACKUP_INTERVAL: %.dm", backupInterval)

	scheduler := gocron.NewScheduler(time.UTC)
	scheduler.Every(backupInterval).Minutes().Do(func() {
		if err := backup(awsRegion, bucketName, datadir); err != nil {
			log.WithError(err).Warnf("failed to backup datadir to %s in region %s", bucketName, awsRegion)
		}
		log.Infof("next backup scheduled at %s", time.Now().Add(time.Duration(backupInterval)*time.Minute).Format(time.RFC3339))
	})
	scheduler.StartAsync()
	log.Info("started backup service")

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGTERM, syscall.SIGINT, syscall.SIGQUIT, os.Interrupt)
	<-sigChan

	log.Info("stop service")
	scheduler.Stop()
	log.Exit(0)
}

func backup(awsRegion, bucketName, datadir string) error {
	ctx := context.Background()

	// Load AWS configuration, creds are sourced from env vars.
	cfg, err := config.LoadDefaultConfig(ctx,
		config.WithRegion(awsRegion),
	)
	if err != nil {
		return fmt.Errorf("failed to load AWS SDK config: %s", err)
	}

	// Create S3 bucket if it doesn't exist. This assumes the AWS user to have full access to S3.
	s3Client := s3.NewFromConfig(cfg)
	if err := createBucketIfNotExists(ctx, s3Client, bucketName, awsRegion); err != nil {
		return err
	}

	log.Debug("compressing target datadir into tar.gz archive...")
	// Compress with gZIP the target datadir into a tar archive.
	tarFileName := fmt.Sprintf(backupFileFormat, time.Now().Format(time.RFC3339))
	if err := createTarGz(ctx, datadir, tarFileName); err != nil {
		return err
	}

	log.Debug("uploading backup...")
	// Upload the archive to the target S3 bucket
	tarFile, err := os.Open(tarFileName)
	if err != nil {
		return fmt.Errorf("failed to open tar file for upload: %v", err)
	}
	defer tarFile.Close()
	defer os.Remove(tarFileName)

	uploader := manager.NewUploader(s3Client)
	if _, err := uploader.Upload(ctx, &s3.PutObjectInput{
		Bucket: aws.String(bucketName),
		Key:    aws.String(tarFileName),
		Body:   tarFile,
	}); err != nil {
		return fmt.Errorf("failed to upload backup: %v", err)
	}

	log.Infof("successfully uploaded backup to s3://%s/%s", bucketName, tarFileName)

	return nil
}

func createTarGz(ctx context.Context, datadir, tarFileName string) (err error) {
	// Create the archive file
	out, err := os.Create(tarFileName)
	if err != nil {
		return fmt.Errorf("failed to create archive file: %s", err)
	}
	defer out.Close()
	defer func() {
		if err != nil {
			os.Remove(tarFileName)
		}
	}()

	// Map the data directory to be archived
	files, err := archives.FilesFromDisk(ctx, nil, map[string]string{
		datadir: "", // Put contents at root of archive
	})
	if err != nil {
		return fmt.Errorf("failed to prepare files for archiving: %s", err)
	}

	// Create a compressed tar archive
	format := archives.CompressedArchive{
		Compression: archives.Gz{},
		Archival:    archives.Tar{},
	}

	// Create the archive
	if err := format.Archive(ctx, out, files); err != nil {
		return fmt.Errorf("failed to create archive: %v", err)
	}

	return nil
}

func createBucketIfNotExists(ctx context.Context, s3Client *s3.Client, bucketName, awsRegion string) error {
	// Check if bucket exists
	if _, err := s3Client.HeadBucket(ctx, &s3.HeadBucketInput{
		Bucket: aws.String(bucketName),
	}); err == nil {
		log.Debugf("bucket %s already exists in region %s, skip", bucketName, awsRegion)
		return nil
	}
	// Create bucket if it doesn't exist
	log.Debugf("creating bucket %s...", bucketName)
	if _, err := s3Client.CreateBucket(ctx, &s3.CreateBucketInput{
		Bucket: aws.String(bucketName),
		CreateBucketConfiguration: &types.CreateBucketConfiguration{
			LocationConstraint: types.BucketLocationConstraint(awsRegion),
		},
	}); err != nil {
		return fmt.Errorf("failed to create bucket %s in region %s: %s", bucketName, awsRegion, err)
	}
	log.Debug("done")

	// Enable versioning on the bucket
	log.Debug("enabling versioning...")
	if _, err := s3Client.PutBucketVersioning(ctx, &s3.PutBucketVersioningInput{
		Bucket: aws.String(bucketName),
		VersioningConfiguration: &types.VersioningConfiguration{
			Status: types.BucketVersioningStatusEnabled,
		},
	}); err != nil {
		return fmt.Errorf("failed to enable versioning on bucket %s in region %s: %s", bucketName, awsRegion, err)
	}
	log.Debug("done")

	return nil
}

func cleanAndExpandPath(path string) string {
	// Expand initial ~ to OS specific home directory.
	if strings.HasPrefix(path, "~") {
		var homeDir string
		u, err := user.Current()
		if err == nil {
			homeDir = u.HomeDir
		} else {
			homeDir = os.Getenv("HOME")
		}

		path = strings.Replace(path, "~", homeDir, 1)
	}

	// NOTE: The os.ExpandEnv doesn't work with Windows-style %VARIABLE%,
	// but the variables can still be expanded via POSIX-style $VARIABLE.
	return filepath.Clean(os.ExpandEnv(path))
}
