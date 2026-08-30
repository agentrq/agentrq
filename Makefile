.PHONY: dev backend frontend desktop desktop-dev install stop mocks test fmt hooks

remote-claude:
	claude --dangerously-load-development-channels server:agentrq-0ZzhYQG2qtl

# Default command to start everything in development mode
dev:
	@echo "Starting AgentRQ Development Environment..."
	@make -j 2 backend frontend

# Start the backend server
backend:
	@echo "Starting Backend..."
	-@lsof -ti:3000,3001 | xargs kill -9 2>/dev/null || true
	@cd backend/cmd/server && mkdir -p _storage && go build -o agentrq_binary main.go && ./agentrq_binary

# Start the frontend dev server
frontend:
	@echo "Waiting for backend on port 3000..."
	@until curl -s http://localhost:3000 > /dev/null; do sleep 1; done
	@echo "Starting Frontend..."
	-@lsof -ti:5173 | xargs kill -9 2>/dev/null || true
	@cd frontend && npm run dev

# Stop all backend and frontend processes
stop:
	@echo "Stopping all AgentRQ processes..."
	-@lsof -ti:3000,3001 | xargs kill -9 2>/dev/null || true
	-@lsof -ti:5173 | xargs kill -9 2>/dev/null || true
	-@pkill -f "go run main.go" || true
	-@pkill -f "vite" || true
	-@pkill -f "agentrq_binary" || true
	-@lsof -ti:5174 | xargs kill -9 2>/dev/null || true
	-@pkill -f "desktop/node_modules/electron" || true
	@echo "Cleanup complete."

# Run the desktop app against a local server.
# It expects a backend on :3000 — `make dev` in another terminal gives you one.
# Point it elsewhere with AGENTRQ_SERVER_URL=https://... make desktop-dev
desktop-dev:
	@echo "Starting Desktop App..."
	@cd desktop && npm run dev

# Build installers into desktop/release/. Publishes nothing.
desktop:
	@echo "Building Desktop App..."
	@cd desktop && npm run package

# Install all dependencies for the whole repo
install:
	@echo "Installing Dependencies..."
	@cd backend && go mod download
	@cd frontend && npm install
	@# The desktop renderer is built from frontend/src, so the frontend's
	@# dependencies have to be in place before this.
	@cd desktop && npm install
	@make hooks

# Point git at the tracked hooks in .githooks so every clone shares them
# (.git/hooks itself is not version-controlled).
hooks:
	@git config core.hooksPath .githooks
	@chmod +x .githooks/* 2>/dev/null || true
	@echo "Git hooks installed (core.hooksPath -> .githooks)"

# Format all Go sources. The pre-commit hook formats only staged files; this is
# the whole-tree version for cleaning up in one deliberate pass.
fmt:
	@echo "Formatting Go sources..."
	@gofmt -w backend/

mocks:
	@echo "Generating Mocks..."
	@mkdir -p backend/internal/service/mocks/repository \
		backend/internal/service/mocks/memq \
		backend/internal/service/mocks/smtp \
		backend/internal/service/mocks/idgen \
		backend/internal/service/mocks/scheduler \
		backend/internal/service/mocks/image \
		backend/internal/service/mocks/storage \
		backend/internal/service/mocks/auth \
		backend/internal/service/mocks/dbconn \
		backend/internal/service/mocks/pubsub
	@cd backend && \
		mockgen -source=internal/repository/base/repository.go -destination=internal/service/mocks/repository/mock_repository.go -package=repository && \
		mockgen -source=internal/service/memq/memq.go -destination=internal/service/mocks/memq/mock_memq.go -package=memq && \
		mockgen -source=internal/service/smtp/smtp.go -destination=internal/service/mocks/smtp/mock_smtp.go -package=smtp && \
		mockgen -source=internal/service/idgen/idgen.go -destination=internal/service/mocks/idgen/mock_idgen.go -package=idgen && \
		mockgen -source=internal/service/scheduler/scheduler.go -destination=internal/service/mocks/scheduler/mock_scheduler.go -package=scheduler && \
		mockgen -source=internal/service/image/image.go -destination=internal/service/mocks/image/mock_image.go -package=image && \
		mockgen -source=internal/service/storage/storage.go -destination=internal/service/mocks/storage/mock_storage.go -package=storage && \
		mockgen -source=internal/service/auth/auth.go -destination=internal/service/mocks/auth/mock_auth.go -package=auth && \
		mockgen -source=internal/service/auth/jwt.go -destination=internal/service/mocks/auth/mock_jwt.go -package=auth && \
		mockgen -source=internal/repository/dbconn/dbconn.go -destination=internal/service/mocks/dbconn/mock_dbconn.go -package=dbconn && \
		mockgen -source=internal/service/pubsub/pubsub.go -destination=internal/service/mocks/pubsub/mock_pubsub.go -package=pubsub

test: mocks
	@cd backend && go test ./internal/service/... ./internal/controller/...

# Typecheck, test, and build the DeepSeek Harness plugin bundle.
# Harness packages declare peers that pnpm resolves from a dsh profile, so a
# standalone npm install needs --legacy-peer-deps.
plugin-deepseek:
	@echo "Building @agentrq/dsh-plugin-agentrq..."
	@cd plugins/deepseek-harness && \
		npm install --legacy-peer-deps && \
		npm run typecheck && \
		npm test && \
		npm run build
