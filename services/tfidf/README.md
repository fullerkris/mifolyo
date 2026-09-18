# TF-IDF Processor

> [!IMPORTANT]
> **Status — current V1 offline postprocessor; outside F3.** TF-IDF is a
> MongoDB-only batch job, not a Crawl Jobs V2 producer or consumer. It is not in
> the F3 implementation path and must receive neither Crawl Jobs Redis
> credentials nor network access to that Redis instance. The commands below do
> not authorize V2 operation. See the
> [parent remediation plan](../../docs/spider-render-remediation-plan-2026-09-01.md)
> and [F3 implementation plan](../../docs/crawl-jobs-v2-plan.md).

The TF-IDF Processor computes TF-IDF (Term Frequency-Inverse Document
Frequency) after the Indexer has processed crawled pages. It reads indexed data
from MongoDB, calculates scores for each term and document, and writes the
results to MongoDB for query-time ranking.

## Setup

### Using Docker

The recommended way to run the TF-IDF Processor is with Docker. This ensures all dependencies are handled and the service runs in an isolated environment.

1. **Install Docker**:  
   Follow the instructions for your OS on the [Docker website](https://docs.docker.com/get-docker/).

2. **Configure Environment Variables**:  
   Create a `variables.env` file in the `services/tfidf` directory with the following content (adjust as needed):
   ```env
   MONGO_HOST=<your_mongo_host>
   MONGO_PORT=<your_mongo_port>         # default: 27017
   MONGO_DB=<your_mongo_db>             # default: test
   MONGO_USERNAME=<your_mongo_username> # default: empty
   MONGO_PASSWORD=<your_mongo_password> # default: empty
   ```

3. **Build and Run**:  
   In the `services/tfidf` directory, run the following commands:
   ```bash
   docker compose build
   docker compose up
   ```

### Without Docker

If you prefer not to use Docker, you can run the TF-IDF Processor directly on your machine. Ensure you have all dependencies installed. It is recommended to use a virtual environment to avoid conflicts with other Python packages.

1. **Install Dependencies**:  
   Install the required packages using `pip`:
   ```bash
   pip install -r requirements.txt
   ```
2. **Configure Environment Variables**:  
   Export the required variables in the process environment before startup. The
   Python service does not load a `.env` file by itself; source one explicitly
   in your shell only if it contains isolated development values.
3. **Run the TF-IDF Processor**:  
   Execute the TF-IDF Processor script:
   ```bash
   python main.py
   ```
