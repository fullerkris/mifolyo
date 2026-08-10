# Image Indexer

The Image Indexer is a core service in the Moogle search engine pipeline. Its job is to process images found on crawled web pages from the Spider, extract relevant metadata, and store image data in MongoDB for fast retrieval by other services. The Image Indexer builds an inverted index for images, manages image metadata, and prepares data for image search and ranking.

> [!WARNING]
> **BLOCKED:** Do not run this service until external image retrieval implements
> and tests DNS-pinned address authorization, redirect revalidation, private and
> host address rejection, and response-size limits. The commands below are
> retained only as post-hardening reference procedures.

## Setup

### Using Docker

The recommended way to run the Image Indexer is with Docker. This ensures all dependencies are handled and the service runs in an isolated environment.

1. **Install Docker**:  
   Follow the instructions for your OS on the [Docker website](https://docs.docker.com/get-docker/).

2. **Configure Environment Variables**:  
   Create a `variables.env` file in the `services/image-indexer` directory with the following content (adjust as needed):
   ```env
   REDIS_HOST=<your_redis_host>
   REDIS_PORT=<your_redis_port>         # default: 6379
   REDIS_PASSWORD=<your_redis_password> # default: empty
   REDIS_DB=<your_redis_db>             # default: 0
   MONGO_HOST=<your_mongo_host>
   MONGO_PORT=<your_mongo_port>         # default: 27017
   MONGO_DB=<your_mongo_db>             # default: test
   MONGO_USERNAME=<your_mongo_username> # default: empty
   MONGO_PASSWORD=<your_mongo_password> # default: empty
   ```

3. **Build and Run**:  
   In the `services/image-indexer` directory, run the following commands after
   the security block above is resolved:
   ```bash
   docker-compose build
   docker-compose up
   ```

### Without Docker

If you prefer not to use Docker, you can run the Indexer directly on your machine. Ensure you have all dependencies installed. It is recommended to use a virtual environment to avoid conflicts with other Python packages.

1. **Install Dependencies**:  
   Install the required packages using `pip`:
   ```bash
   pip install -r requirements.txt
   ```
2. **Configure Environment Variables**:  
   Set up the environment variables in your shell or create a `.env` file in the `services/indexer` directory.
3. **Run the Indexer**:  
   Execute the Indexer script:
   ```bash
   python indexer.py
   ```
