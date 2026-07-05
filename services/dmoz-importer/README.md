# DMOZ Importer

Standalone importer for the Internet Archive DMOZ `content.rdf.u8` dump.

It stream-parses the RDF file, filters MiFolyo's launch categories, canonicalizes URLs, maps each record to a MiFolyo category, and writes pending crawl seeds to MongoDB.

Default target collection: `dmoz_seeds`.

The importer intentionally does not write into `metadata` by default because the current indexer expects crawled metadata documents to match its existing schema exactly.

## Download

```bash
wget -c "https://archive.org/download/dmoz-odp-data/content.rdf.u8.gz" -O data/content.rdf.u8.gz
gunzip data/content.rdf.u8.gz
```

## Dry Run

```bash
docker compose run --rm dmoz-importer python import.py /data/content.rdf.u8 --dry-run --limit 10000
```

## Import

```bash
docker compose run --rm dmoz-importer python import.py /data/content.rdf.u8 --batch-size 1000
```

## Optional Health Sample

```bash
docker compose run --rm dmoz-importer python import.py /data/content.rdf.u8 --dry-run --health-sample-size 100
```

Health sampling sends HEAD requests with `MiFolyoBot/1.0` and is optional. Full URL health handling still belongs to the spider.
