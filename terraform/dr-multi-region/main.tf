# CloudAI Fusion — Terraform Multi-Cluster DR Module
# Configures cross-region disaster recovery infrastructure

terraform {
  required_version = ">= 1.5"
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
  }
}

# ============================================================================
# Variables
# ============================================================================

variable "primary_region" {
  description = "Primary AWS region"
  type        = string
  default     = "us-east-1"
}

variable "standby_region" {
  description = "Standby AWS region for DR"
  type        = string
  default     = "us-west-2"
}

variable "project_name" {
  description = "Project name prefix"
  type        = string
  default     = "cloudai-fusion"
}

variable "dr_strategy" {
  description = "DR strategy: multi-active, hot-standby, warm-standby"
  type        = string
  default     = "hot-standby"
}

variable "rds_instance_class" {
  description = "RDS instance class"
  type        = string
  default     = "db.r6g.xlarge"
}

variable "enable_global_accelerator" {
  description = "Enable AWS Global Accelerator for cross-region traffic"
  type        = bool
  default     = true
}

variable "tags" {
  description = "Common tags"
  type        = map(string)
  default = {
    Project   = "cloudai-fusion"
    ManagedBy = "terraform"
    Component = "disaster-recovery"
  }
}

# ============================================================================
# Provider Aliases
# ============================================================================

provider "aws" {
  alias  = "primary"
  region = var.primary_region
}

provider "aws" {
  alias  = "standby"
  region = var.standby_region
}

# ============================================================================
# Cross-Region Database Replication (RDS Global Database)
# ============================================================================

resource "aws_rds_global_cluster" "main" {
  provider = aws.primary

  global_cluster_identifier = "${var.project_name}-global-db"
  engine                    = "aurora-postgresql"
  engine_version            = "15.4"
  database_name             = "cloudai"
  storage_encrypted         = true

  # Force destroy for non-production; remove for production
  force_destroy = false
}

resource "aws_rds_cluster" "primary" {
  provider = aws.primary

  cluster_identifier        = "${var.project_name}-primary"
  engine                    = "aurora-postgresql"
  engine_version            = "15.4"
  global_cluster_identifier = aws_rds_global_cluster.main.id
  master_username           = "cloudai"
  master_password           = "CHANGE_ME_IN_PRODUCTION"
  database_name             = "cloudai"
  storage_encrypted         = true
  skip_final_snapshot       = true

  tags = merge(var.tags, {
    Role   = "primary"
    Region = var.primary_region
  })
}

resource "aws_rds_cluster_instance" "primary" {
  provider = aws.primary

  count              = 2
  identifier         = "${var.project_name}-primary-${count.index}"
  cluster_identifier = aws_rds_cluster.primary.id
  instance_class     = var.rds_instance_class
  engine             = "aurora-postgresql"

  tags = var.tags
}

resource "aws_rds_cluster" "standby" {
  provider = aws.standby

  cluster_identifier        = "${var.project_name}-standby"
  engine                    = "aurora-postgresql"
  engine_version            = "15.4"
  global_cluster_identifier = aws_rds_global_cluster.main.id
  storage_encrypted         = true
  skip_final_snapshot       = true

  # Standby cluster depends on primary
  depends_on = [aws_rds_cluster.primary]

  tags = merge(var.tags, {
    Role   = "standby"
    Region = var.standby_region
  })
}

resource "aws_rds_cluster_instance" "standby" {
  provider = aws.standby

  count              = 1
  identifier         = "${var.project_name}-standby-${count.index}"
  cluster_identifier = aws_rds_cluster.standby.id
  instance_class     = var.rds_instance_class
  engine             = "aurora-postgresql"

  tags = var.tags
}

# ============================================================================
# Route53 Health Checks & DNS Failover
# ============================================================================

resource "aws_route53_health_check" "primary" {
  provider = aws.primary

  fqdn              = "${var.project_name}-primary.internal"
  port               = 443
  type               = "HTTPS"
  resource_path      = "/healthz"
  failure_threshold  = 3
  request_interval   = 10

  tags = merge(var.tags, { Name = "${var.project_name}-primary-healthcheck" })
}

resource "aws_route53_health_check" "standby" {
  provider = aws.primary

  fqdn              = "${var.project_name}-standby.internal"
  port               = 443
  type               = "HTTPS"
  resource_path      = "/healthz"
  failure_threshold  = 3
  request_interval   = 10

  tags = merge(var.tags, { Name = "${var.project_name}-standby-healthcheck" })
}

# ============================================================================
# S3 Cross-Region Replication (for backups and state)
# ============================================================================

resource "aws_s3_bucket" "primary_backup" {
  provider = aws.primary
  bucket   = "${var.project_name}-backup-${var.primary_region}"

  tags = merge(var.tags, { Role = "primary-backup" })
}

resource "aws_s3_bucket_versioning" "primary_backup" {
  provider = aws.primary
  bucket   = aws_s3_bucket.primary_backup.id

  versioning_configuration {
    status = "Enabled"
  }
}

resource "aws_s3_bucket" "standby_backup" {
  provider = aws.standby
  bucket   = "${var.project_name}-backup-${var.standby_region}"

  tags = merge(var.tags, { Role = "standby-backup" })
}

resource "aws_s3_bucket_versioning" "standby_backup" {
  provider = aws.standby
  bucket   = aws_s3_bucket.standby_backup.id

  versioning_configuration {
    status = "Enabled"
  }
}

# ============================================================================
# Outputs
# ============================================================================

output "global_database_id" {
  description = "RDS Global Database identifier"
  value       = aws_rds_global_cluster.main.id
}

output "primary_cluster_endpoint" {
  description = "Primary RDS cluster endpoint"
  value       = aws_rds_cluster.primary.endpoint
}

output "standby_cluster_endpoint" {
  description = "Standby RDS cluster endpoint"
  value       = aws_rds_cluster.standby.endpoint
}

output "primary_backup_bucket" {
  description = "Primary backup S3 bucket"
  value       = aws_s3_bucket.primary_backup.id
}

output "standby_backup_bucket" {
  description = "Standby backup S3 bucket"
  value       = aws_s3_bucket.standby_backup.id
}
