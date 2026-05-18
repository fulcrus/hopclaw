---
name: aws
description: Manage AWS resources including S3, EC2, Lambda, and CloudWatch via the AWS CLI
homepage: https://aws.amazon.com/cli/
user-invocable: true
command-dispatch: tool
command-tool: aws.run
command-arg-mode: raw
metadata:
  openclaw:
    skillKey: infra.aws
    emoji: "\u2601\uFE0F"
    primaryEnv: AWS_ACCESS_KEY_ID
    requires:
      bins:
        - aws
      env:
        - AWS_ACCESS_KEY_ID
        - AWS_SECRET_ACCESS_KEY
    always: false
---
# AWS

Manage Amazon Web Services resources using the AWS CLI.

## Capabilities

- S3: list, upload, download, sync buckets and objects
- EC2: list, describe, start, stop instances
- Lambda: list, invoke, view logs for functions
- CloudWatch: query logs, get metrics, describe alarms
- IAM: list users, roles, policies (read-only recommended)
- ECS/EKS: list clusters, services, tasks
- RDS: list and describe database instances
- STS: verify identity and assume roles

## Authentication

Requires AWS credentials:

- `AWS_ACCESS_KEY_ID`: Access key ID
- `AWS_SECRET_ACCESS_KEY`: Secret access key
- `AWS_DEFAULT_REGION`: Default region (e.g., us-east-1)

Optional:
- `AWS_SESSION_TOKEN`: For temporary credentials (STS)
- `AWS_PROFILE`: Named profile from ~/.aws/credentials

### Verify Identity

```bash
# Check who you are authenticated as
aws sts get-caller-identity
```

## Usage

### S3 Operations

```bash
# List buckets
aws s3 ls

# List objects in a bucket
aws s3 ls s3://my-bucket/prefix/

# Upload a file
aws s3 cp ./local-file.txt s3://my-bucket/path/file.txt

# Download a file
aws s3 cp s3://my-bucket/path/file.txt ./local-file.txt

# Sync a directory
aws s3 sync ./local-dir s3://my-bucket/prefix/ --exclude "*.tmp"

# Generate a presigned URL (1 hour expiry)
aws s3 presign s3://my-bucket/path/file.txt --expires-in 3600

# Bucket size
aws s3 ls s3://my-bucket --recursive --summarize | tail -2

# Remove objects
aws s3 rm s3://my-bucket/path/file.txt
```

### EC2 Operations

```bash
# List running instances
aws ec2 describe-instances \
  --filters "Name=instance-state-name,Values=running" \
  --query "Reservations[].Instances[].{ID:InstanceId,Type:InstanceType,IP:PublicIpAddress,Name:Tags[?Key=='Name']|[0].Value}" \
  --output table

# Start an instance
aws ec2 start-instances --instance-ids i-1234567890abcdef0

# Stop an instance
aws ec2 stop-instances --instance-ids i-1234567890abcdef0

# Describe instance details
aws ec2 describe-instances --instance-ids i-1234567890abcdef0

# List security groups
aws ec2 describe-security-groups \
  --query "SecurityGroups[].{ID:GroupId,Name:GroupName,VPC:VpcId}" \
  --output table
```

### Lambda Operations

```bash
# List functions
aws lambda list-functions \
  --query "Functions[].{Name:FunctionName,Runtime:Runtime,Memory:MemorySize}" \
  --output table

# Invoke a function
aws lambda invoke \
  --function-name my-function \
  --payload '{"key": "value"}' \
  --cli-binary-format raw-in-base64-out \
  /tmp/lambda-response.json && cat /tmp/lambda-response.json

# View function configuration
aws lambda get-function-configuration --function-name my-function

# View recent invocations via CloudWatch
aws logs filter-log-events \
  --log-group-name "/aws/lambda/my-function" \
  --start-time $(date -d '-1 hour' +%s000 2>/dev/null || date -v-1H +%s000) \
  --limit 20
```

### CloudWatch Operations

```bash
# List log groups
aws logs describe-log-groups \
  --query "logGroups[].{Name:logGroupName,Size:storedBytes}" \
  --output table

# Query recent logs
aws logs filter-log-events \
  --log-group-name "/aws/lambda/my-function" \
  --filter-pattern "ERROR" \
  --start-time $(date -d '-24 hours' +%s000 2>/dev/null || date -v-24H +%s000) \
  --limit 50

# Describe alarms
aws cloudwatch describe-alarms --state-value ALARM \
  --query "MetricAlarms[].{Name:AlarmName,State:StateValue,Metric:MetricName}" \
  --output table

# Get CPU metric for an EC2 instance
aws cloudwatch get-metric-statistics \
  --namespace AWS/EC2 \
  --metric-name CPUUtilization \
  --dimensions Name=InstanceId,Value=i-1234567890abcdef0 \
  --start-time $(date -u -d '-1 hour' +%Y-%m-%dT%H:%M:%S 2>/dev/null || date -u -v-1H +%Y-%m-%dT%H:%M:%S) \
  --end-time $(date -u +%Y-%m-%dT%H:%M:%S) \
  --period 300 \
  --statistics Average
```

### IAM Operations (Read-Only)

```bash
# List users
aws iam list-users --query "Users[].{Name:UserName,Created:CreateDate}" --output table

# List roles
aws iam list-roles --query "Roles[].{Name:RoleName,Arn:Arn}" --output table --max-items 20

# Get current account's access summary
aws iam get-account-summary
```

## Error Handling

- If "Unable to locate credentials", verify AWS_ACCESS_KEY_ID and AWS_SECRET_ACCESS_KEY are set.
- If "AccessDenied", the IAM user/role lacks the required permissions. Show the failed action for debugging.
- If "ExpiredToken", the session token has expired. Refresh credentials or re-assume the role.
- If region-specific errors occur, verify AWS_DEFAULT_REGION is set correctly.

## Security

- NEVER expose AWS_SECRET_ACCESS_KEY or AWS_SESSION_TOKEN in output.
- Prefer read-only operations unless the user explicitly requests write operations.
- Confirm before any destructive actions (terminate instances, delete S3 objects, delete functions).
- Use `--dry-run` flag where supported (EC2) before executing changes.
- Do not list or display IAM credentials, access keys, or secret values.
- Warn before operations that could incur significant costs (launching large instances, data transfer).
