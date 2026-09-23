# agentd Testing Plan

This document outlines the testing plan to verify agentd functionality through the web interface.

## Prerequisites

- Docker and Docker Compose installed
- Ports 3000, 4000, 8000, 8765 available

## Quick Start with Docker Compose

The easiest way to run the full stack locally:

```bash
# Build and start all services
docker compose -f docker-compose.dev.yml up --build

# Open http://localhost:3000

# To stop
docker compose -f docker-compose.dev.yml down
```

Services:
- **mockllm** (port 8000) - Fake OpenAI-compatible LLM
- **litellm** (port 4000) - Proxy routing to mockllm
- **agentd** (port 8765) - Daemon
- **web** (port 3000) - Next.js frontend

## Running the Test Environment (Manual)

### Terminal 1: Start mock LLM
```bash
python3 scripts/mock_llm.py --port 4000
```

### Terminal 2: Start agentd daemon
```bash
cd /home/anthony/code/agentd
MOCK_API_KEY=test ./bin/agentd start --skip-llm-warmup -v
```

### Terminal 3: Start web frontend
```bash
cd /home/anthony/code/agentd/web
NEXT_PUBLIC_USE_MOCK=false npm run dev
```

### Terminal 4: Access the web interface
Open browser to: **http://localhost:3000**

---

## Test Checkpoints

### Checkpoint 1: Verify Logs Appear When Loop Starts and Kanban is Accessible

**Steps:**
1. Open browser to http://localhost:3000
2. Check if the Kanban board loads
3. Look for logs panel/section
4. Verify the daemon logs are visible

**Expected Results:**
- Kanban board displays with task columns
- Logs show daemon activity
- No console errors

---

### Checkpoint 2: Chat Interface Works

**Steps:**
1. Find the chat input in the web interface
2. Type a message (e.g., "Hello, create a test task")
3. Send the message

**Expected Results:**
- Message appears in chat history
- Response from the agent appears
- No HTTP errors in console

---

### Checkpoint 3: Kanban View Reflects Database

**Steps:**
1. Create a task via chat (Checkpoint 2)
2. Check the Kanban board
3. Verify task appears in correct column

**Expected Results:**
- Tasks created via chat appear in Kanban
- Task states (PENDING, RUNNING, COMPLETED, etc.) are correct
- Data matches what's in SQLite database

---

### Checkpoint 4: Chat Creates Tasks in Kanban and Logs Populate

**Steps:**
1. Send a request to create multiple tasks via chat
2. Example: "Create tasks for: (1) Write documentation, (2) Fix login bug, (3) Add dark mode"
3. Watch the Kanban board for new tasks
4. Observe the logs panel

**Expected Results:**
- New tasks appear in Kanban PENDING column
- Agent processes tasks (moves to RUNNING)
- Logs show task execution details
- Tasks eventually complete (COMPLETED/FAILED)

---

## Troubleshooting

### If LiteLLM is not running:
- Tasks will fail with LLM errors
- Check logs for "connection refused" or "LLM error"

### If web shows "Not Connected":
- Check if daemon is running on port 8765
- Check browser console for errors

### If tasks don't appear:
- Check daemon logs for errors
- Verify database has tasks: `sqlite3 ~/.agentd/global.db "SELECT * FROM tasks;"`