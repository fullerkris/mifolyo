FROM python:3.13-slim

# Set working directory
WORKDIR /app

ENV PYTHONDONTWRITEBYTECODE=1

# Copy and install dependencies
COPY requirements.txt ./
RUN pip install --no-cache-dir -r requirements.txt

# Copy the rest of the application code
COPY . .

# Private builds only: redistribution remains blocked by the release workflow.
RUN python -B -c "import importlib.util; assert importlib.util.find_spec('nltk') is None, 'Full NLTK must not be installed'" \
    && python -B verify_nlp.py --source-only \
    && mkdir /tmp/indexer-nlp \
    && python -B tools/vendor_nlp.py --cache /tmp/indexer-nlp --fetch --provision-data \
    && python -B verify_nlp.py \
    && rm -rf /tmp/indexer-nlp

# Run the application
CMD ["python", "./main.py"]
