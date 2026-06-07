# Query Engine

The Query Engine is the main API and web interface for the Moogle search engine. It provides endpoints and web pages for searching web pages and images, retrieving page metadata, exploring page connections (outlinks and backlinks), and viewing search statistics. The Query Engine is built with Laravel and serves as the bridge between users and the indexed data stored in MongoDB.

## Features

- **Keyword Search**: Search for web pages using keywords, ranked by TF-IDF and PageRank.
- **Image Search**: Search for images using keywords and view associated metadata.
- **Suggestions**: Provides search suggestions and fuzzy matching for misspelled queries.
- **Page Connections**: Explore outlinks and backlinks for any indexed page.
- **Statistics**: View search statistics, top searches, and random page recommendations.
- **Web Interface**: User-friendly frontend for searching and browsing results.
- **REST API**: JSON endpoints for integration with other services or clients.

## Setup

### Using Docker

The recommended way to run the Query Engine is with Docker. This ensures all dependencies are handled and the service runs in an isolated environment.

1. **Install Docker**:  
   Follow the instructions for your OS on the [Docker website](https://docs.docker.com/get-docker/).

2. **Configure Environment Variables**:  
   Create a `.env` file in the `services/query-engine` directory using `.env.example` as the starting point. The local Docker setup uses PostgreSQL for MiFolyo application data and MongoDB for Moogle index/search data:
   ```env
   APP_NAME=MiFolyo
   APP_KEY=base64:your_app_key_here
   APP_ENV=local
   APP_DEBUG=true
   APP_URL=http://localhost

   DB_CONNECTION=pgsql
   DB_HOST=postgres
   DB_PORT=5432
   DB_DATABASE=mifolyo
   DB_USERNAME=mifolyo
   DB_PASSWORD=mifolyo

   MONGODB_URI=mongodb://mongo:27017
   MONGODB_DATABASE=mifolyo_index

   CACHE_STORE=redis
   QUEUE_CONNECTION=redis
   REDIS_CLIENT=predis
   REDIS_HOST=redis
   REDIS_PASSWORD=null
   REDIS_PORT=6379
   ```

3. **Build and Run**:  
   In the `services/query-engine` directory, run:
   ```bash
   docker compose build
   docker compose up
   ```

### Without Docker
The process of running the Query Engine without Docker is a bit more involved, as it requires setting up the environment manually. I will update this README with the necessary steps to run the Query Engine without Docker in the future. For now, please refer to the official Laravel documentation for setting up a Laravel application locally: [Laravel Installation](https://laravel.com/docs/installation).
