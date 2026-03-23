.PHONY: dev backend frontend install stop

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
	@echo "Cleanup complete."

# Install all dependencies for both frontend and backend
install:
	@echo "Installing Dependencies..."
	@cd backend && go mod download
	@cd frontend && npm install

push-build:
	docker buildx build --platform linux/amd64,linux/arm64 --tag hasmcp/agentrq:latest -f Dockerfile --push .
