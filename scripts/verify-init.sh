#!/bin/bash
# verify-init.sh - Verify agentd init works correctly from clean state
#
# Usage:
#   ./scripts/verify-init.sh              # Uses default AGENTD_HOME or ~/.agentd
#   AGENTD_HOME=/tmp/test-agentd ./scripts/verify-init.sh  # Custom location
#   ./scripts/verify-init.sh --clean        # Force clean state
#   ./scripts/verify-init.sh --verbose      # Test verbose mode

set -e

# Find agentd binary
AGENTD_BIN="${AGENTD_BIN:-$(which agentd 2>/dev/null || echo './agentd')}"

if [ ! -x "$AGENTD_BIN" ] && [ ! -x "./agentd" ]; then
    echo "ERROR: agentd binary not found. Build with: go build -o agentd ./cmd/agentd"
    exit 1
fi

# Parse arguments
CLEAN=false
VERBOSE=false
while [[ $# -gt 0 ]]; do
    case $1 in
        --clean)
            CLEAN=true
            shift
            ;;
        --verbose)
            VERBOSE=true
            shift
            ;;
        *)
            echo "Unknown option: $1"
            echo "Usage: $0 [--clean] [--verbose]"
            exit 1
            ;;
    esac
done

AGENTD_HOME="${AGENTD_HOME:-$HOME/.agentd}"

echo "=== agentd init verification ==="
echo "Home directory: $AGENTD_HOME"
echo "Verbose mode: $VERBOSE"

# Check for existing installation
if [ -d "$AGENTD_HOME" ]; then
    echo ""
    echo "WARNING: Existing agentd home found at $AGENTD_HOME"
    if [ "$CLEAN" = true ]; then
        echo "Removing existing installation (--clean flag provided)..."
        rm -rf "$AGENTD_HOME"
    else
        echo "Use --clean flag to remove and reinitialize, or set AGENTD_HOME to a different location."
        exit 1
    fi
fi

# Build init command
INIT_CMD="$AGENTD_BIN init --home \"$AGENTD_HOME\""
if [ "$VERBOSE" = true ]; then
    INIT_CMD="$INIT_CMD -v"
fi

# Run init and capture output
echo ""
echo "Running: $INIT_CMD"
output=$(eval "$INIT_CMD" 2>&1) || {
    echo "ERROR: agentd init failed"
    echo "$output"
    exit 1
}

echo ""
echo "=== Init output ==="
echo "$output"

# Verify files exist
echo ""
echo "=== Verifying results ==="

passed=0
failed=0

check_result() {
    if [ $? -eq 0 ]; then
        echo "✓ $1"
        ((passed++)) || true
    else
        echo "✗ $1"
        ((failed++)) || true
    fi
}

# Verify basic file structure
[ -d "$AGENTD_HOME" ] && check_result "Home directory exists" || echo "✗ Home directory exists"
[ -f "$AGENTD_HOME/global.db" ] && check_result "Database file exists" || echo "✗ Database file exists"
[ -f "$AGENTD_HOME/agentd.crontab" ] && check_result "Crontab file exists" || echo "✗ Crontab file exists"
[ -d "$AGENTD_HOME/projects" ] && check_result "Projects directory exists" || echo "✗ Projects directory exists"
[ -d "$AGENTD_HOME/uploads" ] && check_result "Uploads directory exists" || echo "✗ Uploads directory exists"
[ -d "$AGENTD_HOME/archives" ] && check_result "Archives directory exists" || echo "✗ Archives directory exists"

# Check for WAL files (journal mode)
[ -f "$AGENTD_HOME/global.db-wal" ] || [ -f "$AGENTD_HOME/global.db" ] && check_result "Database files present" || echo "✗ Database files present"

# In verbose mode, verify log messages
if [ "$VERBOSE" = true ]; then
    echo ""
    echo "=== Verifying verbose logs ==="
    echo "$output" | grep -q '"msg":"loading configuration"' && check_result "Configuration loading logged" || echo "✗ Configuration loading logged"
    echo "$output" | grep -q '"msg":"creating directories"' && check_result "Directory creation logged" || echo "✗ Directory creation logged"
    echo "$output" | grep -q '"msg":"migration v2 applied"' && check_result "Migration v2 logged" || echo "✗ Migration v2 logged"
    echo "$output" | grep -q '"msg":"migration v7 applied"' && check_result "Migration v7 logged" || echo "✗ Migration v7 logged"
fi

# In non-verbose mode, verify logs are NOT present
if [ "$VERBOSE" = false ]; then
    echo ""
    echo "=== Verifying quiet mode ==="
    if echo "$output" | grep -q '"msg":"loading configuration"'; then
        echo "✗ Logs should not appear in quiet mode"
        ((failed++)) || true
    else
        echo "✓ No verbose logs in quiet mode"
        ((passed++)) || true
    fi
fi

echo ""
echo "=== Verification summary ==="
echo "Passed: $passed"
echo "Failed: $failed"

if [ $failed -gt 0 ]; then
    echo ""
    echo "Verification FAILED"
    exit 1
fi

echo ""
echo "=== All verifications passed ==="