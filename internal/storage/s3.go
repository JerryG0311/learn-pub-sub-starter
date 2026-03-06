package storage

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

func UploadToS3(filename string, file io.Reader) (string, error) {
	bucket := os.Getenv("S3_BUCKET_NAME")
	region := os.Getenv("AWS_REGION")

	// 1. Load AWS Config from Environment Variables
	cfg, err := config.LoadDefaultConfig(context.TODO(), config.WithRegion(region))
	if err != nil {
		return "", err
	}

	client := s3.NewFromConfig(cfg)

	// 2. Upload the file
	_, err = client.PutObject(context.TODO(), &s3.PutObjectInput{
		Bucket:             aws.String(bucket),
		Key:                aws.String(filename),
		Body:               file,
		ACL:                types.ObjectCannedACLPublicRead, // Make it viewable in gallery
		ContentDisposition: aws.String("attachment; filename=\"" + filename + "\""),
	})
	if err != nil {
		return "", err
	}

	// 3. Return the publick URL
	return fmt.Sprintf("https://%s.s3.%s.amazonaws.com/%s", bucket, region, filename), nil
}

func UploadFileToS3(filename string, localPath string) (string, error) {
	file, err := os.Open(localPath)
	if err != nil {
		return "", err
	}
	defer file.Close()
	return UploadToS3(filename, file)
}

func DownloadFromS3(url string, localPath string) error {
	// 1. Simple helper to download a public S3 file to a local path
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("bad status: %s", resp.Status)
	}

	out, err := os.Create(localPath)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	return err
}

func DeleteFromS3(filename string) error {
	bucket := os.Getenv("S3_BUCKET_NAME")
	region := os.Getenv("AWS_REGION")

	cfg, err := config.LoadDefaultConfig(context.TODO(), config.WithRegion(region))
	if err != nil {
		return err
	}

	client := s3.NewFromConfig(cfg)

	_, err = client.DeleteObject(context.TODO(), &s3.DeleteObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(filename),
	})

	return err
}
