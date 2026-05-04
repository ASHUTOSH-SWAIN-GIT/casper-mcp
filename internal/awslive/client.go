package awslive

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials/stscreds"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"gopkg.in/yaml.v3"
)

// AWSConfig holds the cloud.aws block from .casper/config.yaml.
type AWSConfig struct {
	RoleARN string   `yaml:"role_arn"`
	Regions []string `yaml:"regions"`
}

// rawCasperConfig is a minimal struct used to extract just the cloud section
// without triggering the full config.Validate() requirements (database.url, states).
type rawCasperConfig struct {
	Cloud struct {
		AWS AWSConfig `yaml:"aws"`
	} `yaml:"cloud"`
}

// LoadConfig reads the cloud.aws section from .casper/config.yaml in dir.
// Returns ok=false (no error) when the file exists but has no cloud.aws section.
// Returns an error only on parse failure.
func LoadConfig(dir string) (AWSConfig, bool, error) {
	path := filepath.Join(dir, ".casper", "config.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return AWSConfig{}, false, nil
		}
		return AWSConfig{}, false, fmt.Errorf("read %s: %w", path, err)
	}

	var raw rawCasperConfig
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return AWSConfig{}, false, fmt.Errorf("parse %s: %w", path, err)
	}

	cfg := raw.Cloud.AWS
	if cfg.RoleARN == "" && len(cfg.Regions) == 0 {
		return AWSConfig{}, false, nil
	}
	if len(cfg.Regions) == 0 {
		cfg.Regions = []string{"us-east-1"}
	}
	return cfg, true, nil
}

// Client holds one aws.Config per region, all using the assumed role.
type Client struct {
	configs map[string]aws.Config
	regions []string
}

// NewClient assumes the role in cfg.RoleARN (empty = ambient credentials) and
// builds a regional aws.Config for each region in cfg.Regions.
func NewClient(ctx context.Context, cfg AWSConfig) (*Client, error) {
	baseCfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("load AWS config: %w", err)
	}

	stsClient := sts.NewFromConfig(baseCfg)

	configs := make(map[string]aws.Config, len(cfg.Regions))
	for _, region := range cfg.Regions {
		var regionalCfg aws.Config
		if cfg.RoleARN != "" {
			provider := stscreds.NewAssumeRoleProvider(stsClient, cfg.RoleARN)
			regionalCfg, err = config.LoadDefaultConfig(ctx,
				config.WithRegion(region),
				config.WithCredentialsProvider(provider),
			)
		} else {
			regionalCfg, err = config.LoadDefaultConfig(ctx,
				config.WithRegion(region),
			)
		}
		if err != nil {
			return nil, fmt.Errorf("build config for region %s: %w", region, err)
		}
		configs[region] = regionalCfg
	}

	return &Client{configs: configs, regions: cfg.Regions}, nil
}

// ConfigForRegion returns the aws.Config for the given region, or the first
// available config if the region isn't explicitly configured.
func (c *Client) ConfigForRegion(region string) (aws.Config, bool) {
	cfg, ok := c.configs[region]
	if ok {
		return cfg, true
	}
	if len(c.regions) > 0 {
		return c.configs[c.regions[0]], true
	}
	return aws.Config{}, false
}

// Regions returns the ordered list of configured regions.
func (c *Client) Regions() []string {
	return c.regions
}
