# Sentria Fact-Checker Service (Go)

[![Go Version](https://img.shields.io/badge/go-1.21+-blue.svg)](https://golang.org/dl/)
## Project Overview

The Sentria Fact-Checker is a backend microservice written in Go. It serves as a component of the larger Sentria platform, a community-driven disaster reporting system.

This service's primary role is to:
* Receive disaster report information (typically via a job queue, triggered by the main Sentria Node.js application).
* Perform automated fact-checking by querying external, open-source disaster APIs (starting with GDACS - Global Disaster Alert and Coordination System).
* Calculate a fact-check score or confidence level based on the findings from these external sources.
* (Eventually) Report these findings back to the main Sentria application to enhance the reliability of disaster posts.

## Requirements

To build and run this service, you will need:

* **Go:** Version 1.21 or higher is recommended. You can download it from [golang.org/dl/](https://golang.org/dl/).
* **Git:** For cloning the repository (if applicable).
* **(Optional for Development)** `air`: For live-reloading during development. Install via:
    ```bash
    go install [github.com/air-verse/air@latest](https://github.com/air-verse/air@latest)
    ```
    Ensure your Go binary path (usually `$HOME/go/bin` or `$(go env GOPATH)/bin`) is in your system's `PATH` environment variable for `air` to be accessible.

## Setup and Installation

1.  **Clone the Repository (if you haven't already):**
    ```bash
    # git clone https://github.com/AungKyawPhyo1142/sentria-go
    # cd sentria-go
    ```

2.  **Environment Variables:**
    This service uses environment variables for configuration.
    * Copy the example environment file:
        ```bash
        cp .env.example .env
        ```
    * Edit the `.env` file to set your specific configurations, especially for local development:
        * `PORT`: The port the Go service will listen on (e.g., `8081`).
        * `GIN_MODE`: Set to `debug` for development or `release` for production.
        *(You will need to create an `.env.example` file in your project root. See "Environment Variables" section in the more detailed README example for content, or start with just `FACTCHECKER_SERVER_PORT=8081` and `GIN_MODE=debug`)*

3.  **Install Dependencies:**
    Go modules are used for dependency management. Dependencies are typically fetched when you build or run the project. To explicitly download/update:
    ```bash
    go mod tidy
    ```

## Running the Service (for Development)

* **Directly with `go run`:**
    ```bash
    go run ./cmd/factchecker-api/main.go
    ```

* **Using `air` for live reloading (recommended):**
    (Ensure you have an `.air.toml` configuration file in your project root as discussed previously.)
    ```bash
    air
    ```


The service will start, and you should see log output indicating it's running (e.g., listening on port 8081). For initial testing of the GDACS integration, observe the console logs for output from the `testGDACSFactCheckFromFile()` function which processes data from `test.json`.

---

*Further details on API endpoints, building for production, advanced testing, job queue integration, and callbacks will be added as those features are implemented.*