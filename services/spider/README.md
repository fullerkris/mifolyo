# Spider

This is the main actor involved in web crawling. It is responsible for fetching web pages, extracting links, and storing the data in Redis. The spider uses a breadth-first search algorithm to discover new links and stores the crawled data in Redis for fast access. The spider is designed to be simple and efficient, with a focus on educational purposes rather than production-level performance.

## Setup

### Using Docker
Using Docker is the recommended way to run the spider. It allows you to run the spider in an isolated environment without worrying about dependencies or system configurations. To properly run the spider with Docker you need to add a `variables.env` file in the `services/spider` directory. The `variables.env` file should contain the following variables:

```bash
REDIS_HOST=<your_redis_host>
REDIS_PORT=<your_redis_port> (default: 6379)
REDIS_PASSWORD=<your_redis_password> (default: empty)
REDIS_DB=<your_redis_db> (default: 0)
STARTING_URL=<your_starting_url> (default: https://en.wikipedia.org/wiki/Kamen_Rider)
USER_AGENT=<crawler_user_agent> (default: MiFolyoBot/1.0)
```

To run the spider using Docker, follow these steps:
1. **Install Docker**: The installation instructions will depend on your operating system. You can find the installation instructions for your OS on the [Docker website](https://docs.docker.com/get-docker/).
2. **Build the Docker image**: Navigate to the `services/spider` directory (if you're not already here) and run the following command.
   ```bash
   docker compose up --build
   ```
3. **Running in detached mode**: If you want to run the spider in the background, you can use the `-d` flag.
   ```bash
   docker compose up --build -d
   ```
4. **Scaling the spider**: If you want to run multiple instances of the spider when the spider is running under detached mode, you can use the `--scale` option.
   ```bash
   docker compose up --scale spider=3
   ```
5. **Stopping the spider**: To stop the spider, you can use the following command.
   ```bash
   # If you are running in detached mode
   docker compose down
   # If you are running in the foreground
   Ctrl + C
   ```

### Without Docker

If you prefer to run the spider without Docker, you can do so by building and running the Go binary directly on your system. Make sure you have Go installed (version 1.18 or higher is recommended).

1. **Install Go**:  
   Download and install Go from the [official website](https://go.dev/dl/).

2. **Set up environment variables**:  
   Create a `variables.env` file in the `services/spider` directory with the following content (adjust values as needed):
   ```env
   REDIS_HOST=<your_redis_host>
   REDIS_PORT=<your_redis_port>
   REDIS_PASSWORD=<your_redis_password>
   REDIS_DB=<your_redis_db>
   STARTING_URL=<your_starting_url>
   ```

3. **Export environment variables**:  
   Before running the spider, export the variables in your shell:
   ```bash
   export $(grep -v '^#' variables.env | xargs)
   ```

4. **Build the spider**:  
   Navigate to the `services/spider` directory and run:
   ```bash
   go build -o spider ./cmd/spider
   ```

5. **Run the spider**:  
   Start the spider with optional flags for concurrency and batch size:
   ```bash
   ./spider -max-concurrency=10 -max-pages=100
   ```

   For a bounded development crawl, exit after one batch:
   ```bash
   ./spider -max-concurrency=2 -max-pages=10 -once
   ```

6. **Stopping the spider**:  
   Press `Ctrl + C` in the terminal to stop the process.

**Note:**  
- Make sure Redis is running and accessible with the credentials you provided.
- You may need to install Go dependencies using `go mod tidy` before building.

For development or debugging, you can also run the spider directly:
```bash
go run
```
