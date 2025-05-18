# Sentria Fact-Checker - Project Structure

This document outlines the project structure for the Sentria Fact-Checking Service, a Go application responsible for background verification of disaster reports.

## Root Directory (`sentria-go/`)

The root directory contains essential project-level files and top-level directories.

sentria-go/
├── cmd/
├── internal/****
├── pkg/
├── docs/
│   └── project-structure.md
├── go.mod
├── go.sum
├── Dockerfile
└── .env.example         (Recommended: Add for environment variable guidance)

- **`cmd/`**: Contains the main applications (executables) of the project.
- **`internal/`**: Contains all the private application code. This code is not meant to be imported by other projects.
- **`pkg/`**: (Optional) Contains library code that's safe to be used by external applications. For this service, we might not have much here initially.
- **`docs/`**: Contains project documentation.
  - `project-structure.md`: This file.
- **`go.mod`**: Defines the module's path and its dependencies.
- **`go.sum`**: Contains the checksums of direct and indirect dependencies.
- **`Dockerfile`**: Instructions to build a Docker image for the service.
- **`.env.example`**: (Recommended) An example file showing the required environment variables for running the application (e.g., `FACTCHECKER_SERVER_PORT`, `GIN_MODE`, Job Queue connection details, Node.js API callback URL/key). Users would copy this to `.env` and fill in their values for local development.

---

## `cmd/` Directory

This directory holds the entry points for your executable applications.

- **`cmd/factchecker-api/`**: The main application for our fact-checking service.
  - **`main.go`**: The entry point for the service. It initializes configurations, sets up the Gin HTTP server, initializes the job queue consumer, and handles graceful shutdown.

---

## `internal/` Directory

This is where the core logic of our application resides. Code in `internal` is not importable by other projects.

- **`internal/api/`**: Handles HTTP API concerns, using the Gin framework.
  - **`handlers/`**: Contains HTTP request handlers (controllers in MVC terms).
    - `health_handler.go`: Handles the `/health` endpoint.
    - `(Future) factcheck_callback_handler.go`: Could handle callbacks from the Node.js API if the Go service needs to receive updates or triggers via HTTP (though primarily it consumes from a job queue).
  - **`router.go`**: Defines HTTP routes and wires them to their respective handlers. Initializes Gin middleware.

- **`internal/config/`**: Manages application configuration.
  - **`config.go`**: Defines the `Config` struct and provides functions to load configuration settings (e.g., from environment variables or config files).

- **`internal/core/`**: Contains the core business logic and domain-specific operations.
  - **`service/`**: Business logic services that orchestrate tasks.
    - `(Future) factcheck_service.go`: Will contain the logic for performing fact-checking on a given report (e.g., interacting with external APIs, applying rules). This service will be called by the job queue worker.

- **`internal/worker/`**: Handles background job processing via a Job Queue.
  - **`(Future) consumer.go`**: Initializes the connection to the Job Queue (e.g., RabbitMQ, Redis/BullMQ, NATS) and defines the logic for consuming messages (jobs).
  - **`(Future) processors/`**: (Optional subdirectory) Could contain different job processors if there are various types of background tasks related to fact-checking.
    - `(Future) disaster_report_processor.go`: Logic to handle a specific "fact-check disaster report" job from the queue. It would likely use the `factcheck_service.go`.

- **`internal/platform/`**: Contains clients or adapters for interacting with external platforms or services.
  - **`(Future) jobqueue/`**: Abstracted client code for interacting with the chosen Job Queue system.
  - **`(Future) node_api_client/`**: Client code for making HTTP calls back to the Sentria Node.js API (e.g., to update a report's fact-check status and score after processing).
  - **`(Future) external_apis/`**: Clients for any third-party APIs used for fact-checking (e.g., weather APIs, news APIs, geolocation services).

---

## `pkg/` Directory (Optional)

Public library code that can be imported by other projects. For a standalone microservice like the fact-checker, this might not be heavily used unless you develop utility packages that could be shared.

- Example: `pkg/commonerrors/`, `pkg/customvalidator/`

---

This structure aims to follow standard Go project layout principles, promoting separation of concerns, maintainability, and testability. As the project evolves, we can add more specific components within these directories.