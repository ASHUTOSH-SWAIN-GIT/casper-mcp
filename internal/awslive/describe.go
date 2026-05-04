package awslive

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
	"github.com/aws/aws-sdk-go-v2/service/rds"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/ASHUTOSH-SWAIN-GIT/casper-mcp/internal/graph"
)

// ErrNotSupported is returned when no describe function exists for a resource type.
var ErrNotSupported = errors.New("resource type not supported by describe_live_state")

// SupportedTypes lists all Terraform resource types that can be described.
var SupportedTypes = []string{
	"aws_db_instance",
	"aws_rds_cluster",
	"aws_db_subnet_group",
	"aws_security_group",
	"aws_subnet",
	"aws_vpc",
	"aws_instance",
	"aws_s3_bucket",
	"aws_iam_role",
	"aws_lambda_function",
	"aws_eks_cluster",
}

// describeFunc queries live AWS state for one resource and returns a flat
// key→value map of attributes normalised to Terraform naming conventions.
// The second return is a list of UnmanagedItems found alongside the resource.
type describeFunc func(ctx context.Context, awsCfg aws.Config, id string) (map[string]string, []UnmanagedItem, error)

// iamDescribeFunc is used for IAM resources, which are global and don't need
// a regional aws.Config — only any configured region is used.
var describeMap = map[string]describeFunc{
	"aws_db_instance":    describeDBInstance,
	"aws_rds_cluster":    describeRDSCluster,
	"aws_db_subnet_group": describeDBSubnetGroup,
	"aws_security_group": describeSecurityGroup,
	"aws_subnet":         describeSubnet,
	"aws_vpc":            describeVPC,
	"aws_instance":       describeEC2Instance,
	"aws_s3_bucket":      describeS3Bucket,
	"aws_iam_role":       describeIAMRole,
	"aws_lambda_function": describeLambdaFunction,
	"aws_eks_cluster":    describeEKSCluster,
}

// Describe queries live AWS state for a resource, trying each configured region
// in order until one succeeds. IAM is global — any region works.
// Returns ErrNotSupported (wrapped) for unknown types.
func Describe(ctx context.Context, client *Client, r graph.Resource) (map[string]string, []UnmanagedItem, error) {
	fn, ok := describeMap[r.Type]
	if !ok {
		return nil, nil, fmt.Errorf("%w: %s (supported: %s)",
			ErrNotSupported, r.Type, strings.Join(SupportedTypes, ", "))
	}

	resourceID := extractID(r)
	if resourceID == "" {
		return nil, nil, fmt.Errorf("cannot determine AWS resource ID for %s", r.Identifier)
	}

	var lastErr error
	for _, region := range client.Regions() {
		cfg, ok := client.ConfigForRegion(region)
		if !ok {
			continue
		}
		attrs, unmanaged, err := fn(ctx, cfg, resourceID)
		if err != nil {
			lastErr = err
			continue
		}
		return attrs, unmanaged, nil
	}
	if lastErr != nil {
		return nil, nil, lastErr
	}
	return nil, nil, fmt.Errorf("no regions configured")
}

// extractID resolves the AWS resource identifier from a graph.Resource.
// Priority: attributes["id"] → type-specific key → last component of identifier.
func extractID(r graph.Resource) string {
	if v, ok := r.Attributes["id"].(string); ok && v != "" {
		return v
	}
	typeSpecificKeys := map[string][]string{
		"aws_db_instance":     {"db_instance_identifier", "identifier"},
		"aws_rds_cluster":     {"cluster_identifier", "id"},
		"aws_db_subnet_group": {"name"},
		"aws_security_group":  {"name"},
		"aws_s3_bucket":       {"bucket", "id"},
		"aws_iam_role":        {"name"},
		"aws_lambda_function": {"function_name"},
		"aws_eks_cluster":     {"name"},
		"aws_instance":        {"id"},
		"aws_subnet":          {"id"},
		"aws_vpc":             {"id"},
	}
	for _, key := range typeSpecificKeys[r.Type] {
		if v, ok := r.Attributes[key].(string); ok && v != "" {
			return v
		}
	}
	// Fall back to the last component of "aws_db_instance.orders_main" → "orders_main"
	parts := strings.SplitN(r.Identifier, ".", 2)
	if len(parts) == 2 && parts[1] != "" {
		return parts[1]
	}
	return r.Identifier
}

// ── RDS ──────────────────────────────────────────────────────────────────────

func describeDBInstance(ctx context.Context, cfg aws.Config, id string) (map[string]string, []UnmanagedItem, error) {
	svc := rds.NewFromConfig(cfg)
	out, err := svc.DescribeDBInstances(ctx, &rds.DescribeDBInstancesInput{
		DBInstanceIdentifier: aws.String(id),
	})
	if err != nil {
		return nil, nil, fmt.Errorf("DescribeDBInstances %q: %w", id, err)
	}
	if len(out.DBInstances) == 0 {
		return nil, nil, fmt.Errorf("DB instance %q not found in AWS", id)
	}
	db := out.DBInstances[0]
	m := map[string]string{
		"instance_class":        aws.ToString(db.DBInstanceClass),
		"engine":                aws.ToString(db.Engine),
		"engine_version":        aws.ToString(db.EngineVersion),
		"storage_type":          aws.ToString(db.StorageType),
		"allocated_storage":     fmt.Sprintf("%d", db.AllocatedStorage),
		"multi_az":              fmt.Sprintf("%v", db.MultiAZ),
		"publicly_accessible":   fmt.Sprintf("%v", db.PubliclyAccessible),
		"storage_encrypted":     fmt.Sprintf("%v", db.StorageEncrypted),
		"deletion_protection":   fmt.Sprintf("%v", db.DeletionProtection),
		"backup_retention_period": fmt.Sprintf("%d", db.BackupRetentionPeriod),
		"db_instance_identifier": aws.ToString(db.DBInstanceIdentifier),
	}
	if db.DBInstanceStatus != nil {
		m["status"] = *db.DBInstanceStatus
	}
	if db.DBName != nil {
		m["db_name"] = *db.DBName
	}
	return m, nil, nil
}

func describeRDSCluster(ctx context.Context, cfg aws.Config, id string) (map[string]string, []UnmanagedItem, error) {
	svc := rds.NewFromConfig(cfg)
	out, err := svc.DescribeDBClusters(ctx, &rds.DescribeDBClustersInput{
		DBClusterIdentifier: aws.String(id),
	})
	if err != nil {
		return nil, nil, fmt.Errorf("DescribeDBClusters %q: %w", id, err)
	}
	if len(out.DBClusters) == 0 {
		return nil, nil, fmt.Errorf("RDS cluster %q not found in AWS", id)
	}
	c := out.DBClusters[0]
	m := map[string]string{
		"engine":                  aws.ToString(c.Engine),
		"engine_version":          aws.ToString(c.EngineVersion),
		"cluster_identifier":      aws.ToString(c.DBClusterIdentifier),
		"deletion_protection":     fmt.Sprintf("%v", c.DeletionProtection),
		"backup_retention_period": fmt.Sprintf("%d", c.BackupRetentionPeriod),
		"storage_encrypted":       fmt.Sprintf("%v", c.StorageEncrypted),
		"multi_az":                fmt.Sprintf("%v", c.MultiAZ),
	}
	if c.Status != nil {
		m["status"] = *c.Status
	}
	return m, nil, nil
}

func describeDBSubnetGroup(ctx context.Context, cfg aws.Config, id string) (map[string]string, []UnmanagedItem, error) {
	svc := rds.NewFromConfig(cfg)
	out, err := svc.DescribeDBSubnetGroups(ctx, &rds.DescribeDBSubnetGroupsInput{
		DBSubnetGroupName: aws.String(id),
	})
	if err != nil {
		return nil, nil, fmt.Errorf("DescribeDBSubnetGroups %q: %w", id, err)
	}
	if len(out.DBSubnetGroups) == 0 {
		return nil, nil, fmt.Errorf("DB subnet group %q not found in AWS", id)
	}
	sg := out.DBSubnetGroups[0]
	m := map[string]string{
		"name":        aws.ToString(sg.DBSubnetGroupName),
		"description": aws.ToString(sg.DBSubnetGroupDescription),
		"vpc_id":      aws.ToString(sg.VpcId),
		"status":      aws.ToString(sg.SubnetGroupStatus),
	}
	return m, nil, nil
}

// ── EC2 ──────────────────────────────────────────────────────────────────────

func describeSecurityGroup(ctx context.Context, cfg aws.Config, id string) (map[string]string, []UnmanagedItem, error) {
	svc := ec2.NewFromConfig(cfg)

	// id may be a sg-xxx ID or a name — try ID filter first, fall back to name.
	var filterName, filterValue string
	if strings.HasPrefix(id, "sg-") {
		filterName, filterValue = "group-id", id
	} else {
		filterName, filterValue = "group-name", id
	}

	out, err := svc.DescribeSecurityGroups(ctx, &ec2.DescribeSecurityGroupsInput{
		Filters: []ec2types.Filter{{Name: aws.String(filterName), Values: []string{filterValue}}},
	})
	if err != nil {
		return nil, nil, fmt.Errorf("DescribeSecurityGroups %q: %w", id, err)
	}
	if len(out.SecurityGroups) == 0 {
		return nil, nil, fmt.Errorf("security group %q not found in AWS", id)
	}
	g := out.SecurityGroups[0]
	m := map[string]string{
		"id":          aws.ToString(g.GroupId),
		"name":        aws.ToString(g.GroupName),
		"description": aws.ToString(g.Description),
		"vpc_id":      aws.ToString(g.VpcId),
		"ingress_rule_count": fmt.Sprintf("%d", len(g.IpPermissions)),
		"egress_rule_count":  fmt.Sprintf("%d", len(g.IpPermissionsEgress)),
	}

	// Not-in-Terraform: inbound/outbound rules present in AWS (v0.1: report count,
	// full rule diff is a future enhancement).
	var unmanaged []UnmanagedItem
	for _, perm := range g.IpPermissions {
		for _, r := range perm.IpRanges {
			unmanaged = append(unmanaged, UnmanagedItem{
				Type:   "aws_security_group_rule",
				ID:     fmt.Sprintf("%s-ingress-%d-%s", aws.ToString(g.GroupId), aws.ToInt32(perm.FromPort), aws.ToString(r.CidrIp)),
				Reason: "inbound rule attached to managed SG — not tracked in Terraform state",
			})
		}
	}

	return m, unmanaged, nil
}

func describeSubnet(ctx context.Context, cfg aws.Config, id string) (map[string]string, []UnmanagedItem, error) {
	svc := ec2.NewFromConfig(cfg)
	out, err := svc.DescribeSubnets(ctx, &ec2.DescribeSubnetsInput{
		SubnetIds: []string{id},
	})
	if err != nil {
		return nil, nil, fmt.Errorf("DescribeSubnets %q: %w", id, err)
	}
	if len(out.Subnets) == 0 {
		return nil, nil, fmt.Errorf("subnet %q not found in AWS", id)
	}
	s := out.Subnets[0]
	m := map[string]string{
		"id":                           aws.ToString(s.SubnetId),
		"vpc_id":                       aws.ToString(s.VpcId),
		"cidr_block":                   aws.ToString(s.CidrBlock),
		"availability_zone":            aws.ToString(s.AvailabilityZone),
		"map_public_ip_on_launch":      fmt.Sprintf("%v", s.MapPublicIpOnLaunch),
		"available_ip_address_count":   fmt.Sprintf("%d", aws.ToInt32(s.AvailableIpAddressCount)),
	}
	return m, nil, nil
}

func describeVPC(ctx context.Context, cfg aws.Config, id string) (map[string]string, []UnmanagedItem, error) {
	svc := ec2.NewFromConfig(cfg)
	out, err := svc.DescribeVpcs(ctx, &ec2.DescribeVpcsInput{
		VpcIds: []string{id},
	})
	if err != nil {
		return nil, nil, fmt.Errorf("DescribeVpcs %q: %w", id, err)
	}
	if len(out.Vpcs) == 0 {
		return nil, nil, fmt.Errorf("VPC %q not found in AWS", id)
	}
	v := out.Vpcs[0]
	m := map[string]string{
		"id":         aws.ToString(v.VpcId),
		"cidr_block": aws.ToString(v.CidrBlock),
		"state":      string(v.State),
		"is_default": fmt.Sprintf("%v", aws.ToBool(v.IsDefault)),
	}
	return m, nil, nil
}

func describeEC2Instance(ctx context.Context, cfg aws.Config, id string) (map[string]string, []UnmanagedItem, error) {
	svc := ec2.NewFromConfig(cfg)
	out, err := svc.DescribeInstances(ctx, &ec2.DescribeInstancesInput{
		InstanceIds: []string{id},
	})
	if err != nil {
		return nil, nil, fmt.Errorf("DescribeInstances %q: %w", id, err)
	}
	for _, r := range out.Reservations {
		for _, inst := range r.Instances {
			m := map[string]string{
				"id":                    aws.ToString(inst.InstanceId),
				"instance_type":         string(inst.InstanceType),
				"availability_zone":     aws.ToString(inst.Placement.AvailabilityZone),
				"public_ip":             aws.ToString(inst.PublicIpAddress),
				"private_ip":            aws.ToString(inst.PrivateIpAddress),
				"state":                 string(inst.State.Name),
				"vpc_id":                aws.ToString(inst.VpcId),
				"subnet_id":             aws.ToString(inst.SubnetId),
				"ami":                   aws.ToString(inst.ImageId),
				"ebs_optimized":         fmt.Sprintf("%v", aws.ToBool(inst.EbsOptimized)),
			}
			return m, nil, nil
		}
	}
	return nil, nil, fmt.Errorf("EC2 instance %q not found in AWS", id)
}

// ── S3 ───────────────────────────────────────────────────────────────────────
// Coverage: existence, versioning, tags. ACL and lifecycle require separate calls;
// not included in v0.1.

func describeS3Bucket(ctx context.Context, cfg aws.Config, id string) (map[string]string, []UnmanagedItem, error) {
	svc := s3.NewFromConfig(cfg)

	if _, err := svc.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(id)}); err != nil {
		return nil, nil, fmt.Errorf("HeadBucket %q: %w", id, err)
	}

	m := map[string]string{"bucket": id, "exists": "true"}

	if vOut, err := svc.GetBucketVersioning(ctx, &s3.GetBucketVersioningInput{Bucket: aws.String(id)}); err == nil {
		m["versioning_status"] = string(vOut.Status)
	}

	if tOut, err := svc.GetBucketTagging(ctx, &s3.GetBucketTaggingInput{Bucket: aws.String(id)}); err == nil {
		for _, tag := range tOut.TagSet {
			m["tag_"+aws.ToString(tag.Key)] = aws.ToString(tag.Value)
		}
	}

	return m, nil, nil
}

// ── IAM ──────────────────────────────────────────────────────────────────────
// IAM is global; any configured region's credentials are used.

func describeIAMRole(ctx context.Context, cfg aws.Config, id string) (map[string]string, []UnmanagedItem, error) {
	svc := iam.NewFromConfig(cfg)
	out, err := svc.GetRole(ctx, &iam.GetRoleInput{RoleName: aws.String(id)})
	if err != nil {
		return nil, nil, fmt.Errorf("GetRole %q: %w", id, err)
	}
	r := out.Role
	m := map[string]string{
		"name":        aws.ToString(r.RoleName),
		"arn":         aws.ToString(r.Arn),
		"path":        aws.ToString(r.Path),
		"description": aws.ToString(r.Description),
	}
	if r.MaxSessionDuration != nil {
		m["max_session_duration"] = fmt.Sprintf("%d", *r.MaxSessionDuration)
	}
	return m, nil, nil
}

// ── Lambda ───────────────────────────────────────────────────────────────────

func describeLambdaFunction(ctx context.Context, cfg aws.Config, id string) (map[string]string, []UnmanagedItem, error) {
	svc := lambda.NewFromConfig(cfg)
	out, err := svc.GetFunction(ctx, &lambda.GetFunctionInput{FunctionName: aws.String(id)})
	if err != nil {
		return nil, nil, fmt.Errorf("GetFunction %q: %w", id, err)
	}
	fn := out.Configuration
	m := map[string]string{
		"function_name": aws.ToString(fn.FunctionName),
		"runtime":       string(fn.Runtime),
		"handler":       aws.ToString(fn.Handler),
		"memory_size":   fmt.Sprintf("%d", aws.ToInt32(fn.MemorySize)),
		"timeout":       fmt.Sprintf("%d", aws.ToInt32(fn.Timeout)),
		"role":          aws.ToString(fn.Role),
		"arn":           aws.ToString(fn.FunctionArn),
	}
	if fn.Description != nil {
		m["description"] = *fn.Description
	}
	return m, nil, nil
}

// ── EKS ──────────────────────────────────────────────────────────────────────

func describeEKSCluster(ctx context.Context, cfg aws.Config, id string) (map[string]string, []UnmanagedItem, error) {
	svc := eks.NewFromConfig(cfg)
	out, err := svc.DescribeCluster(ctx, &eks.DescribeClusterInput{Name: aws.String(id)})
	if err != nil {
		return nil, nil, fmt.Errorf("DescribeCluster %q: %w", id, err)
	}
	c := out.Cluster
	m := map[string]string{
		"name":     aws.ToString(c.Name),
		"arn":      aws.ToString(c.Arn),
		"version":  aws.ToString(c.Version),
		"status":   string(c.Status),
		"role_arn": aws.ToString(c.RoleArn),
	}
	if c.ResourcesVpcConfig != nil {
		m["endpoint_public_access"]  = fmt.Sprintf("%v", c.ResourcesVpcConfig.EndpointPublicAccess)
		m["endpoint_private_access"] = fmt.Sprintf("%v", c.ResourcesVpcConfig.EndpointPrivateAccess)
	}
	return m, nil, nil
}
