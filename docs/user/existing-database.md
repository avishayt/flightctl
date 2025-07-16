# Using an Existing PostgreSQL Database with Flight Control

This guide explains how to configure Flight Control to use an existing PostgreSQL database instead of deploying a new one. This is useful for organizations that have existing database infrastructure or want to integrate Flight Control with their existing PostgreSQL clusters.

## Prerequisites

Before configuring Flight Control to use an existing database, ensure your PostgreSQL installation meets the following requirements:

### PostgreSQL Version Requirements
- **Minimum Version**: PostgreSQL 12+
- **Recommended Version**: PostgreSQL 16+ (current Flight Control target)
- **Required Extensions**: `pg_trgm` (trigram matching for text search)

### Database Administrator Access
You'll need access to a PostgreSQL user with sufficient privileges to:
- Create databases and users
- Install extensions
- Grant privileges to users
- Create functions and event triggers

### Database Features
Your PostgreSQL installation must support:
- **JSONB data type** - for flexible metadata storage
- **GIN indexes** - for efficient JSONB querying
- **Event triggers** - for automatic permission management
- **PL/pgSQL functions** - for custom permission logic

## Database Setup Process

### Step 1: Create the Flight Control Database

Connect to your PostgreSQL instance as a superuser and create the Flight Control database:

```sql
-- Connect as superuser (e.g., postgres)
CREATE DATABASE flightctl;

-- Enable required extensions
\c flightctl
CREATE EXTENSION IF NOT EXISTS pg_trgm;
```

### Step 2: Create Administrative User

Create a dedicated administrative user for Flight Control setup:

```sql
-- Create admin user for Flight Control
CREATE USER flightctl_admin WITH PASSWORD 'your_secure_admin_password';

-- Grant necessary privileges
ALTER USER flightctl_admin WITH SUPERUSER;
GRANT ALL PRIVILEGES ON DATABASE flightctl TO flightctl_admin;
```

### Step 3: Run Database User Setup

Flight Control requires a three-user security model. Use the provided setup script to create these users:

```bash
# Set environment variables for your database
export DB_HOST=your-postgres-host
export DB_PORT=5432
export DB_NAME=flightctl
export DB_ADMIN_USER=flightctl_admin
export DB_ADMIN_PASSWORD=your_secure_admin_password
export DB_MIGRATION_USER=flightctl_migrator
export DB_MIGRATION_PASSWORD=your_secure_migration_password
export DB_APP_USER=flightctl_app
export DB_APP_PASSWORD=your_secure_app_password

# Run the setup script
./deploy/scripts/setup_database_users.sh
```

Alternatively, you can manually execute the SQL script:

```bash
# Download the setup script
curl -O https://raw.githubusercontent.com/flightctl/flightctl/main/deploy/scripts/setup_database_users.sql

# Substitute environment variables and execute
envsubst < setup_database_users.sql | psql -h $DB_HOST -U $DB_ADMIN_USER -d $DB_NAME
```

## Helm Deployment Configuration

### Step 1: Configure External Database

The Helm charts now support external databases through the `db.enabled` and `db.external` configuration options. Create a custom values file:

```yaml
# values-external-db.yaml
global:
  internalNamespace: flightctl-internal
  
# Database configuration
db:
  # Disable the built-in PostgreSQL deployment
  enabled: false
  
  # External database configuration
  external:
    hostname: your-postgres-host
    port: 5432
    name: flightctl
    sslMode: require  # Options: disable, require, verify-ca, verify-full
    
    # Database users (these should already exist - see database setup section)
    masterUser: flightctl_admin
    user: flightctl_app
    migrationUser: flightctl_migrator
    
    # Database passwords - provide these for external database
    masterPassword: "your-admin-password"
    userPassword: "your-app-password"
    migrationPassword: "your-migration-password"
    
    # Optional: Use existing secret instead of creating new one
    secretName: ""  # Leave empty to use default flightctl-db-secret
    
    # Optional: SSL certificate configuration
    sslCert: ""     # Path to SSL certificate file
    sslKey: ""      # Path to SSL key file
    sslRootCert: "" # Path to SSL root certificate file
```

### Step 2: Deploy with External Database

Deploy Flight Control using the external database configuration:

```bash
# Deploy Flight Control with external database
helm install flightctl deploy/helm/flightctl \
  --namespace flightctl \
  --create-namespace \
  --values values-external-db.yaml
```

**Note**: The Helm chart will automatically create the necessary database secrets based on your `values-external-db.yaml` configuration. If you prefer to manage secrets separately, you can set `db.external.secretName` to point to an existing secret.

### Step 3: Verify Database Migration

The Helm chart will automatically handle database migration and configuration. Verify that the migration completed successfully:

```bash
# Check that the database migration job completed
kubectl get jobs -n flightctl-internal

# Check migration job logs
kubectl logs -n flightctl-internal job/flightctl-db-migration-<revision>

# Verify that services are connecting to the external database
kubectl logs -n flightctl deployment/flightctl-api | grep -i "database\|connection"
```

### Benefits of Using External Database with Helm

1. **Native Support**: Built-in support for external databases in Helm charts
2. **Simplified Configuration**: Single values file configuration
3. **Automatic Secret Management**: Handles database credentials automatically
4. **SSL Support**: Built-in SSL/TLS configuration options
5. **No Custom Templates**: Works with standard Helm deployment
6. **Conditional Deployment**: Automatically disables built-in database when external is configured

## Quadlet Deployment Configuration

### Step 1: Disable Built-in Database

Modify the quadlet configuration to skip the built-in database:

```bash
# Edit the service configuration
sudo mkdir -p /etc/flightctl
sudo tee /etc/flightctl/external-db.env << EOF
# External database configuration
DB_HOST=your-postgres-host
DB_PORT=5432
DB_NAME=flightctl
DB_ADMIN_USER=flightctl_admin
DB_ADMIN_PASSWORD=your_secure_admin_password
DB_MIGRATION_USER=flightctl_migrator
DB_MIGRATION_PASSWORD=your_secure_migration_password
DB_APP_USER=flightctl_app
DB_APP_PASSWORD=your_secure_app_password
EOF
```

### Step 2: Update Service Configuration

Create a custom service configuration file:

```yaml
# /etc/flightctl/service-config.yaml
database:
  hostname: your-postgres-host
  type: pgsql
  port: 5432
  name: flightctl
  user: flightctl_app
  password: your_secure_app_password
  migrationUser: flightctl_migrator
  migrationPassword: your_secure_migration_password

service: {}
kv:
  hostname: flightctl-kv
  port: 6379
  password: your_kv_password
```

### Step 3: Modify Deployment Script

Create a custom deployment script that skips database deployment:

```bash
#!/usr/bin/env bash
# custom-deploy.sh

set -eo pipefail

# Source the external database configuration
source /etc/flightctl/external-db.env

echo "Using external database at $DB_HOST:$DB_PORT"

# Run database setup (skip if already done)
if ! psql -h "$DB_HOST" -U "$DB_ADMIN_USER" -d "$DB_NAME" -c "SELECT 1 FROM pg_roles WHERE rolname = '$DB_APP_USER'" | grep -q 1; then
    echo "Setting up database users..."
    ./deploy/scripts/setup_database_users.sh
fi

# Run database migrations
echo "Running database migrations..."
export DB_USER=$DB_MIGRATION_USER
export DB_PASSWORD=$DB_MIGRATION_PASSWORD

# Use flightctl-db-migrate command
./bin/flightctl-db-migrate

# Start services (excluding database)
echo "Starting Flight Control services..."
sudo systemctl start flightctl-kv
sudo systemctl start flightctl-api
sudo systemctl start flightctl-worker
sudo systemctl start flightctl-periodic
sudo systemctl start flightctl-alert-exporter

echo "Flight Control deployment completed with external database"
```

### Step 4: Execute Custom Deployment

Run your custom deployment script:

```bash
# Make script executable
chmod +x custom-deploy.sh

# Run deployment
./custom-deploy.sh
```

## Configuration Verification

### Verify Database Setup

Check that all required users and permissions are in place:

```sql
-- Connect to your Flight Control database
\c flightctl

-- Check users exist
SELECT rolname, rolsuper, rolcreaterole, rolcreatedb, rolcanlogin
FROM pg_roles
WHERE rolname IN ('flightctl_admin', 'flightctl_migrator', 'flightctl_app')
ORDER BY rolname;

-- Check application user permissions
SELECT schemaname, tablename,
       array_to_string(array_agg(privilege_type), ', ') as privileges
FROM information_schema.table_privileges
WHERE grantee = 'flightctl_app' AND schemaname = 'public'
GROUP BY schemaname, tablename;

-- Check that extensions are installed
SELECT extname FROM pg_extension WHERE extname = 'pg_trgm';
```

### Verify Service Connectivity

Test that Flight Control services can connect to your database:

```bash
# For Helm deployments
kubectl logs -n flightctl deployment/flightctl-api | grep -i database

# For Quadlet deployments
sudo journalctl -u flightctl-api | grep -i database
```

## Troubleshooting

### Common Issues

#### 1. Connection Refused
**Symptom**: Services cannot connect to the database
**Solutions**:
- Verify database host and port are correct
- Check firewall rules allow connections from Flight Control services
- Ensure PostgreSQL is configured to accept connections from Flight Control hosts

#### 2. Authentication Failed
**Symptom**: Authentication errors in service logs
**Solutions**:
- Verify database credentials are correct
- Check that users were created successfully
- Ensure password complexity meets PostgreSQL requirements

#### 3. Permission Denied
**Symptom**: Services fail with permission errors
**Solutions**:
- Verify the setup script ran successfully
- Check that all required privileges were granted
- Ensure migration user has necessary DDL permissions

#### 4. Missing Extensions
**Symptom**: Errors about missing `pg_trgm` extension
**Solutions**:
- Install the extension: `CREATE EXTENSION IF NOT EXISTS pg_trgm;`
- Verify the extension is available on your PostgreSQL installation
- Check that the database user has permission to use the extension

#### 5. Migration Failures
**Symptom**: Migration job fails during deployment
**Solutions**:
- Check migration logs for specific errors
- Verify migration user has CREATE privileges
- Ensure database schema is clean (no conflicting objects)

### Debugging Commands

#### Check Database Status
```bash
# Test database connectivity
psql -h your-postgres-host -U flightctl_app -d flightctl -c "SELECT version();"

# Check table creation
psql -h your-postgres-host -U flightctl_app -d flightctl -c "\dt"
```

#### Check Service Logs
```bash
# Helm deployment
kubectl logs -n flightctl deployment/flightctl-api --tail=100
kubectl logs -n flightctl-internal job/flightctl-db-migration-<revision>

# Quadlet deployment
sudo journalctl -u flightctl-api --no-pager -n 100
```

## Security Considerations

### Database Security
- Use strong passwords for all database users
- Restrict database access to Flight Control services only
- Regularly rotate database credentials
- Monitor database access logs

### Network Security
- Configure PostgreSQL to accept connections only from Flight Control hosts
- Use TLS/SSL for database connections when possible
- Implement network segmentation between Flight Control and database

### Credential Management
- Store database credentials securely (Kubernetes secrets, vault, etc.)
- Use least-privilege access for each database user
- Regularly audit database permissions

## Backup and Recovery

### Database Backups
Since Flight Control uses your existing database, follow your organization's backup procedures:

```bash
# Example backup command
pg_dump -h your-postgres-host -U flightctl_admin -d flightctl > flightctl_backup.sql

# Example restore command
psql -h your-postgres-host -U flightctl_admin -d flightctl < flightctl_backup.sql
```

### Migration Recovery
If migrations fail, you can clean up and retry:

```bash
# Clean up failed migration (use with caution)
psql -h your-postgres-host -U flightctl_admin -d flightctl -c "DROP SCHEMA IF EXISTS public CASCADE; CREATE SCHEMA public;"

# Re-run setup
./deploy/scripts/setup_database_users.sh
./bin/flightctl-db-migrate
```

## Performance Considerations

### Database Tuning
Consider these PostgreSQL settings for Flight Control:

```sql
-- Recommended settings for Flight Control
ALTER SYSTEM SET max_connections = 200;
ALTER SYSTEM SET shared_buffers = '256MB';
ALTER SYSTEM SET effective_cache_size = '1GB';
ALTER SYSTEM SET work_mem = '4MB';
ALTER SYSTEM SET maintenance_work_mem = '64MB';

-- Reload configuration
SELECT pg_reload_conf();
```

### Connection Pooling
For high-load environments, consider using connection pooling:
- PgBouncer
- PostgreSQL built-in connection pooling
- Application-level connection pooling

## Monitoring

### Database Monitoring
Monitor these key metrics:
- Connection count
- Query performance
- Lock contention
- JSONB query efficiency

### Flight Control Metrics
Flight Control exposes database metrics on port 15691:
- Database connection status
- Query execution time
- Transaction rates

## Complete Example Scripts

For your convenience, here are complete example scripts that you can customize for your environment:

### External Database Setup Script

```bash
#!/bin/bash
# setup-external-db.sh

set -eo pipefail

# Configuration
DB_HOST="your-postgres-host"
DB_PORT=5432
DB_NAME="flightctl"
DB_ADMIN_USER="flightctl_admin"
DB_ADMIN_PASSWORD="your_secure_admin_password"
DB_MIGRATION_USER="flightctl_migrator"
DB_MIGRATION_PASSWORD="your_secure_migration_password"
DB_APP_USER="flightctl_app"
DB_APP_PASSWORD="your_secure_app_password"

echo "Setting up Flight Control database on external PostgreSQL..."

# Create database and enable extensions
psql -h "$DB_HOST" -U postgres -c "CREATE DATABASE $DB_NAME;"
psql -h "$DB_HOST" -U postgres -d "$DB_NAME" -c "CREATE EXTENSION IF NOT EXISTS pg_trgm;"

# Create admin user
psql -h "$DB_HOST" -U postgres -c "CREATE USER $DB_ADMIN_USER WITH PASSWORD '$DB_ADMIN_PASSWORD';"
psql -h "$DB_HOST" -U postgres -c "ALTER USER $DB_ADMIN_USER WITH SUPERUSER;"
psql -h "$DB_HOST" -U postgres -c "GRANT ALL PRIVILEGES ON DATABASE $DB_NAME TO $DB_ADMIN_USER;"

# Run database user setup
export DB_HOST DB_PORT DB_NAME DB_ADMIN_USER DB_ADMIN_PASSWORD
export DB_MIGRATION_USER DB_MIGRATION_PASSWORD DB_APP_USER DB_APP_PASSWORD

./deploy/scripts/setup_database_users.sh

echo "External database setup completed successfully!"
```

### Helm Deployment Script

```bash
#!/bin/bash
# deploy-helm-external-db.sh

set -eo pipefail

NAMESPACE="flightctl"
INTERNAL_NAMESPACE="flightctl-internal"
DB_HOST="your-postgres-host"

# Create custom ConfigMaps
cat > external-db-configs.yaml << EOF
apiVersion: v1
kind: ConfigMap
metadata:
  name: flightctl-api-config
  namespace: $NAMESPACE
data:
  config.yaml: |-
    database:
        hostname: $DB_HOST
        type: pgsql
        port: 5432
        name: flightctl
    service: {}
    kv:
        hostname: flightctl-kv.$INTERNAL_NAMESPACE.svc.cluster.local
        port: 6379
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: flightctl-worker-config
  namespace: $INTERNAL_NAMESPACE
data:
  config.yaml: |-
    database:
        hostname: $DB_HOST
        type: pgsql
        port: 5432
        name: flightctl
    service: {}
    kv:
        hostname: flightctl-kv.$INTERNAL_NAMESPACE.svc.cluster.local
        port: 6379
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: flightctl-periodic-config
  namespace: $INTERNAL_NAMESPACE
data:
  config.yaml: |-
    database:
        hostname: $DB_HOST
        type: pgsql
        port: 5432
        name: flightctl
    service: {}
    kv:
        hostname: flightctl-kv.$INTERNAL_NAMESPACE.svc.cluster.local
        port: 6379
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: flightctl-db-migration-config
  namespace: $INTERNAL_NAMESPACE
data:
  config.yaml: |-
    database:
        hostname: $DB_HOST
        type: pgsql
        port: 5432
        name: flightctl
        user: flightctl_migrator
        migrationUser: flightctl_migrator
    service: {}
EOF

# Apply configurations
kubectl apply -f external-db-configs.yaml

# Create migration job
cat > external-db-migration-job.yaml << EOF
apiVersion: batch/v1
kind: Job
metadata:
  name: flightctl-external-db-migration
  namespace: $INTERNAL_NAMESPACE
spec:
  template:
    spec:
      restartPolicy: OnFailure
      initContainers:
      - name: setup-database-users
        image: quay.io/flightctl/flightctl-db-setup:latest
        env:
        - name: DB_HOST
          value: "$DB_HOST"
        - name: DB_PORT
          value: "5432"
        - name: DB_NAME
          value: "flightctl"
        - name: DB_ADMIN_USER
          valueFrom:
            secretKeyRef:
              name: flightctl-db-secret
              key: masterUser
        - name: DB_ADMIN_PASSWORD
          valueFrom:
            secretKeyRef:
              name: flightctl-db-secret
              key: masterPassword
        - name: DB_MIGRATION_USER
          valueFrom:
            secretKeyRef:
              name: flightctl-db-secret
              key: migrationUser
        - name: DB_MIGRATION_PASSWORD
          valueFrom:
            secretKeyRef:
              name: flightctl-db-secret
              key: migrationPassword
        - name: DB_APP_USER
          valueFrom:
            secretKeyRef:
              name: flightctl-db-secret
              key: user
        - name: DB_APP_PASSWORD
          valueFrom:
            secretKeyRef:
              name: flightctl-db-secret
              key: userPassword
        command:
        - /bin/bash
        - -c
        - |
          set -eo pipefail
          until PGPASSWORD="\$DB_ADMIN_PASSWORD" psql -h "\$DB_HOST" -p "\$DB_PORT" -U "\$DB_ADMIN_USER" -d "\$DB_NAME" -c "SELECT 1" >/dev/null 2>&1; do
            echo "Database not ready, waiting..."
            sleep 5
          done
          export DB_HOST DB_PORT DB_NAME DB_ADMIN_USER DB_ADMIN_PASSWORD
          export DB_MIGRATION_USER DB_MIGRATION_PASSWORD DB_APP_USER DB_APP_PASSWORD
          SQL_FILE="/tmp/setup_database_users.sql"
          envsubst < ./deploy/scripts/setup_database_users.sql > "\$SQL_FILE"
          PGPASSWORD="\$DB_ADMIN_PASSWORD" psql -h "\$DB_HOST" -p "\$DB_PORT" -U "\$DB_ADMIN_USER" -d "\$DB_NAME" -f "\$SQL_FILE"
      containers:
      - name: run-migrations
        image: quay.io/flightctl/flightctl-db-setup:latest
        env:
        - name: HOME
          value: "/root"
        - name: DB_USER
          valueFrom:
            secretKeyRef:
              name: flightctl-db-secret
              key: migrationUser
        - name: DB_PASSWORD
          valueFrom:
            secretKeyRef:
              name: flightctl-db-secret
              key: migrationPassword
        command:
        - /bin/bash
        - -c
        - |
          set -eo pipefail
          mkdir -p /tmp/.flightctl
          cp /root/.flightctl/config.yaml /tmp/.flightctl/config.yaml
          export HOME=/tmp
          /usr/local/bin/flightctl-db-migrate
          export PGPASSWORD="\$DB_PASSWORD"
          psql -h "$DB_HOST" -p "5432" -U "\$DB_USER" -d "flightctl" -c "SELECT grant_app_permissions_on_existing_tables();"
        volumeMounts:
        - mountPath: /root/.flightctl/
          name: flightctl-db-migration-config
          readOnly: true
      volumes:
      - name: flightctl-db-migration-config
        configMap:
          name: flightctl-db-migration-config
EOF

# Run migration
kubectl apply -f external-db-migration-job.yaml
kubectl wait --for=condition=complete job/flightctl-external-db-migration -n $INTERNAL_NAMESPACE --timeout=300s

# Deploy Flight Control
helm install flightctl ./deploy/helm/flightctl \
  -n $NAMESPACE \
  --create-namespace

# Optional: Scale down built-in database
kubectl scale deployment flightctl-db --replicas=0 -n $INTERNAL_NAMESPACE

echo "Flight Control deployed with external database!"
```

## Summary

Using an existing PostgreSQL database with Flight Control requires:

1. **Prerequisites**: PostgreSQL 12+ with required extensions
2. **Database Setup**: Create dedicated database and users
3. **Configuration**: 
   - Helm: Custom values file and secrets
   - Quadlet: Modified deployment scripts
4. **Verification**: Test connectivity and permissions
5. **Monitoring**: Set up appropriate monitoring and alerting

This approach allows Flight Control to integrate with your existing database infrastructure while maintaining security and operational best practices.

The privilege separation architecture implemented in Flight Control makes it easier to use external databases by clearly separating administrative, migration, and runtime database access, which aligns well with enterprise database security practices. 