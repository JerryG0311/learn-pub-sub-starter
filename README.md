# Vidify

Vidify is a high-performance, distributed video management platform that enables users to upload, transcode, and organize video content at scale. Built with a microservices-inspired architecture, it offloads heavy processing to dedicated workers to ensure a seamless and responsive user experience.

## Motivation

Managing video content is computationally expensive and traditionally slows down web applications. I built Vidify because I wanted to solve the "blocking problem" in standard web architectures. By decoupling the video upload and transcoding processes using a distributed worker system and RabbitMQ, I created a platform that remains snappy and responsive for the user, regardless of the processing load on the backend. This project allowed me to dive deep into asynchronous systems, cloud storage durability, and multi-user authentication.

## Quick Start

1. Clone the repository.
2. Set up your environment variables in docker-compose.yml (AWS Credentials, S3 Bucket Name).
3. Run the application:
   ```bash
   docker-compose up --build
   ```
4. Access the app at http://localhost:8080.

## Usage

Vidify provides a web-based dashboard and a CLI-capable worker system for distributed video processing.

### Configuration Flags and Environment Variables
The system behavior can be adjusted using the following optional environment variables in your `docker-compose.yml`:

- `MAX_UPLOAD_SIZE` - Sets the maximum video file size (default: 500MB)
- `WORKER_CONCURRENCY` - Number of simultaneous transcoding threads per worker instance (default: 2)
- `S3_RETRY_ATTEMPTS` - Number of times the worker will attempt to re-upload to AWS on failure (default: 3)

### System Scaling Examples
To handle high-traffic scenarios, you can scale the processing power of the system horizontally without restarting the core API:

**Scale to 5 concurrent processing workers:**
```bash
docker-compose up -d --scale worker=5
```

### User Workflow
- **Authentication:** Access `/signup` to initialize a new user profile.
- **Categorization:** Use the `Playlist` field during upload to automatically group videos via metadata tags.
- **Metadata Management:** Click any video title in the Gallery to trigger an inline AJAX update to the SQLite backend.

## Contributing

Contributions are welcome! If you would like to help improve Vidify, please follow these steps to get your local development environment running.

### Clone the repo

```bash
git clone [https://github.com/JerryG0311/Vidify](https://github.com/JerryG0311/Vidify)
cd Vidify
```

### Build and run for development

The project is containerized to manage the Go environment, SQLite, and RabbitMQ dependencies automatically. Ensure you have Docker installed.

```bash
# Start all services in the background
docker-compose up -d --build

# Follow the API logs to see real-time interaction
docker-compose logs -f api
```

### Run the test suite

To ensure core logic remains stable after your changes, run the test suite from the root directory:

```bash
go test ./...
```

### Submit a pull request

1. Fork the repository on GitHub.
2. Create a feature branch: `git checkout -b feature/amazing-feature`.
3. Commit your changes: `git commit -m 'feat: add amazing feature'`.
4. Push to your branch: `git push origin feature/amazing-feature`.
5. Open a Pull Request to the main branch.