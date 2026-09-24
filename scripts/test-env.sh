#!/bin/bash
# agentd test environment launcher
# Usage: ./scripts/test-env.sh [mock|real|docker]

set -e

MODE="${1:-docker}"
cd "$(dirname "$0")/.."

echo "=== agentd Test Environment Launcher ==="
echo "Mode: $MODE"
echo ""

case "$MODE" in
  docker)
    echo "Starting with Docker Compose..."
    echo "Stop: docker compose -f docker-compose.dev.yml down"
    echo "Open: http://localhost:3000"
    docker compose -f docker-compose.dev.yml up --build -d
    ;;
  mock)
    echo "Starting in MOCK mode (no backend required)"
    echo "Run: cd web && npm run dev"
    echo "Open: http://localhost:3000"
    ;;
  real)
    echo "Starting in REAL mode (requires mock LLM running)"
    echo ""
    echo "Terminal 1 - Start mock LLM:"
    echo "  python3 scripts/mock_llm.py --port 4000"
    echo ""
    echo "Terminal 2 - Start daemon:"
    echo "  MOCK_API_KEY=test ./bin/agentd start --skip-llm-warmup -v"
    echo ""
    echo "Terminal 3 - Start web:"
    echo "  cd web && NEXT_PUBLIC_USE_MOCK=false npm run dev"
    echo ""
    echo "Then open: http://localhost:3000"
    ;;
  *)
    echo "Unknown mode: $MODE"
    echo "Usage: $0 [mock|real|docker]"
    exit 1
    ;;
esac
