package awslive

import (
	"context"
	"fmt"
	"io"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials/stscreds"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/sts"
)

// FetchS3State downloads a Terraform state object from S3 using the same
// credential chain as NewClient — assume cfg.RoleARN if set, otherwise
// ambient credentials (env, AWS_PROFILE, SSO, IAM role, etc).
//
// region overrides cfg.Regions for this single call so backends declared in
// regions that aren't part of the configured describe regions still work.
// If region is empty, falls back to cfg.Regions[0] or "us-east-1".
func FetchS3State(ctx context.Context, cfg AWSConfig, bucket, key, region string) ([]byte, error) {
	if bucket == "" || key == "" {
		return nil, fmt.Errorf("fetch s3 state: bucket and key required")
	}
	if region == "" {
		if len(cfg.Regions) > 0 {
			region = cfg.Regions[0]
		} else {
			region = "us-east-1"
		}
	}

	awsCfg, err := buildAWSConfig(ctx, cfg, region)
	if err != nil {
		return nil, fmt.Errorf("build aws config for s3://%s/%s: %w", bucket, key, err)
	}

	client := s3.NewFromConfig(awsCfg)
	out, err := client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, fmt.Errorf("s3 get s3://%s/%s: %w", bucket, key, err)
	}
	defer out.Body.Close()

	data, err := io.ReadAll(out.Body)
	if err != nil {
		return nil, fmt.Errorf("read s3://%s/%s body: %w", bucket, key, err)
	}
	return data, nil
}

// buildAWSConfig produces an aws.Config for the given region, applying the
// same assume-role chain NewClient uses. Standalone so we can target a
// region that wasn't pre-configured in cfg.Regions.
func buildAWSConfig(ctx context.Context, cfg AWSConfig, region string) (aws.Config, error) {
	baseCfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region))
	if err != nil {
		return aws.Config{}, fmt.Errorf("load default aws config: %w", err)
	}

	if cfg.RoleARN == "" {
		return baseCfg, nil
	}

	stsClient := sts.NewFromConfig(baseCfg)
	provider := stscreds.NewAssumeRoleProvider(stsClient, cfg.RoleARN)
	assumed, err := config.LoadDefaultConfig(ctx,
		config.WithRegion(region),
		config.WithCredentialsProvider(provider),
	)
	if err != nil {
		return aws.Config{}, fmt.Errorf("load assume-role aws config: %w", err)
	}
	return assumed, nil
}
