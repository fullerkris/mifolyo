import copy
import json
import sys
import unittest
from datetime import datetime, timedelta, timezone
from pathlib import Path
from unittest.mock import patch


SERVICE_ROOT = Path(__file__).resolve().parents[1]
REPO_ROOT = Path(__file__).resolve().parents[3]
sys.path.insert(0, str(SERVICE_ROOT))

from crawl_seed import (  # noqa: E402
    ROOT_FIELDS,
    SOURCE_FIELDS,
    SOURCE_RAW_URL_MAX_UTF8_BYTES,
    CrawlSeedValidationError,
    make_source,
    merge_seed_documents,
    new_seed_document,
)
from crawl_seeds import (  # noqa: E402
    EXPECTED_MANUAL_ROWS,
    MANUAL_BASELINE_OBSERVED_AT,
    LocalMongoTarget,
    bootstrap_database,
    build_manual_seed_documents,
    parse_local_mongo_target,
    rebuild_database,
    require_rebuild_authorization,
)
from mongo_seeds import (  # noqa: E402
    COLLECTION_NAME,
    COLLECTION_VALIDATOR,
    CRAWL_SEED_BSON_SCHEMA,
    IncompatibleCrawlSeedCollection,
    RAW_URL_BYTE_LENGTH_VALIDATOR,
    ensure_crawl_seeds_collection,
    mongo_merge_pipeline,
)


class FakeCollection:
    def __init__(self, database, name, documents=None):
        self.database = database
        self.name = name
        self.documents = copy.deepcopy(list(documents or []))
        self.indexes = []

    def create_index(self, fields, **options):
        self.indexes.append((fields, options))
        self.database.events.append(("index", self.name, options.get("name")))
        return options.get("name")

    def count_documents(self, query):
        self.assert_empty_query(query)
        return len(self.documents)

    def find(self, query):
        self.assert_empty_query(query)
        return iter(copy.deepcopy(self.documents))

    def distinct(self, field):
        return list({document[field] for document in self.documents if field in document})

    def rename(self, new_name, **options):
        self.database.events.append(
            ("rename", self.name, new_name, options.get("dropTarget"))
        )
        if options.get("dropTarget"):
            self.database.collections.pop(new_name, None)
        self.database.collections.pop(self.name, None)
        self.name = new_name
        self.database.collections[new_name] = self
        return {"ok": 1}

    @staticmethod
    def assert_empty_query(query):
        if query != {}:
            raise AssertionError(f"unexpected query: {query}")


class FakeDatabase:
    def __init__(self, target_documents=None, validation_valid=True):
        self.name = "mifolyo_index"
        self.collections = {}
        if target_documents is not None:
            self.collections[COLLECTION_NAME] = FakeCollection(
                self, COLLECTION_NAME, target_documents
            )
        self.created = []
        self.commands = []
        self.dropped = []
        self.events = []
        self.validation_valid = validation_valid

    def list_collection_names(self):
        return list(self.collections)

    def create_collection(self, name, **options):
        if name in self.collections:
            raise RuntimeError(f"collection already exists: {name}")
        collection = FakeCollection(self, name)
        self.collections[name] = collection
        self.created.append((name, options))
        self.events.append(("create", name))
        return collection

    def command(self, command):
        self.commands.append(command)
        if "validate" in command:
            self.events.append(("validate", command["validate"]))
            return {"ok": 1, "valid": self.validation_valid}
        self.events.append(("collMod", command["collMod"]))
        return {"ok": 1}

    def drop_collection(self, name):
        self.dropped.append(name)
        self.events.append(("drop", name))
        self.collections.pop(name, None)

    def __getitem__(self, name):
        return self.collections[name]


class CrawlSeedContractTests(unittest.TestCase):
    def setUp(self):
        self.csv_path = REPO_ROOT / "seeds" / "manual-seeds.csv"
        self.observed = datetime(2026, 1, 1, tzinfo=timezone.utc)

    def make_document(
        self,
        *,
        observed_at=None,
        priority=2,
        metadata=None,
        category="Technology",
    ):
        source = make_source(
            source_type="reddit_json",
            source_ref="post-1",
            raw_url="https://example.com/path",
            category=category,
            priority=priority,
            observed_at=observed_at or self.observed,
            metadata=metadata or {"score": 10},
        )
        return new_seed_document("https://example.com/path", source)

    def test_mongo_validator_matches_portable_fields_and_uses_bson_dates(self):
        portable = json.loads(
            (REPO_ROOT / "contracts" / "crawl-seed-v1.schema.json").read_text(
                encoding="utf-8"
            )
        )
        bson_schema = CRAWL_SEED_BSON_SCHEMA
        portable_raw_url = portable["$defs"]["source"]["properties"]["raw_url"]
        bson_raw_url = bson_schema["properties"]["sources"]["items"]["properties"][
            "raw_url"
        ]
        self.assertFalse(bson_schema["additionalProperties"])
        self.assertEqual(set(portable["properties"]), set(bson_schema["properties"]))
        self.assertEqual(ROOT_FIELDS, frozenset(bson_schema["properties"]))
        self.assertEqual(
            set(portable["$defs"]["source"]["properties"]),
            set(bson_schema["properties"]["sources"]["items"]["properties"]),
        )
        self.assertEqual(
            SOURCE_FIELDS,
            frozenset(
                bson_schema["properties"]["sources"]["items"]["properties"]
            ),
        )
        self.assertEqual("date", bson_schema["properties"]["discovered_at"]["bsonType"])
        self.assertEqual("date", bson_schema["properties"]["updated_at"]["bsonType"])
        self.assertEqual(
            "date",
            bson_schema["properties"]["sources"]["items"]["properties"][
                "observed_at"
            ]["bsonType"],
        )
        self.assertEqual(portable_raw_url["maxLength"], bson_raw_url["maxLength"])
        self.assertEqual(
            portable_raw_url["x-maxUtf8Bytes"],
            SOURCE_RAW_URL_MAX_UTF8_BYTES,
        )

    def test_mongo_validator_combines_json_schema_with_utf8_byte_expression(self):
        self.assertIn("$and", COLLECTION_VALIDATOR)
        self.assertEqual(
            CRAWL_SEED_BSON_SCHEMA,
            COLLECTION_VALIDATOR["$and"][0]["$jsonSchema"],
        )
        self.assertIn(
            "$strLenBytes",
            repr(COLLECTION_VALIDATOR["$and"][1]),
        )
        mongo_byte_limit = RAW_URL_BYTE_LENGTH_VALIDATOR["$expr"]["$cond"][1][
            "$allElementsTrue"
        ][0]["$map"]["in"]["$cond"][1]["$lte"][1]
        self.assertEqual(SOURCE_RAW_URL_MAX_UTF8_BYTES, mongo_byte_limit)

    def test_runtime_rejects_raw_url_over_2048_utf8_bytes(self):
        prefix = "https://example.com/"
        remaining = SOURCE_RAW_URL_MAX_UTF8_BYTES - len(prefix.encode("utf-8"))
        at_limit = prefix + ("é" * (remaining // 2)) + ("a" if remaining % 2 else "")
        self.assertEqual(
            SOURCE_RAW_URL_MAX_UTF8_BYTES, len(at_limit.encode("utf-8"))
        )
        source = make_source(
            source_type="manual",
            source_ref="manual#byte-limit",
            raw_url=at_limit,
            category="General",
            priority=1,
            observed_at=self.observed,
        )
        self.assertEqual(at_limit, source["raw_url"])

        with self.assertRaises(CrawlSeedValidationError):
            make_source(
                source_type="manual",
                source_ref="manual#over-byte-limit",
                raw_url=at_limit + "é",
                category="General",
                priority=1,
                observed_at=self.observed,
            )

    def test_collection_is_created_with_strict_validation_and_required_indexes(self):
        database = FakeDatabase()
        collection = ensure_crawl_seeds_collection(database)
        self.assertIs(database.collections[COLLECTION_NAME], collection)
        self.assertEqual(1, len(database.created))
        name, options = database.created[0]
        self.assertEqual(COLLECTION_NAME, name)
        self.assertEqual("strict", options["validationLevel"])
        self.assertEqual("error", options["validationAction"])
        self.assertEqual(COLLECTION_VALIDATOR, options["validator"])
        index_names = {options["name"] for _, options in collection.indexes}
        self.assertEqual(
            {
                "uq_crawl_seeds_canonical_url",
                "ix_crawl_seeds_feed",
                "ix_crawl_seeds_source_key",
            },
            index_names,
        )

    def test_compatible_nonempty_collection_is_preflighted_before_collmod(self):
        document = self.make_document()
        database = FakeDatabase(target_documents=[document])
        collection = ensure_crawl_seeds_collection(database)
        self.assertEqual(COLLECTION_NAME, database.commands[0]["collMod"])
        self.assertEqual("strict", database.commands[0]["validationLevel"])
        self.assertEqual(3, len(collection.indexes))

    def test_bootstrap_rejects_nonempty_legacy_collection_without_mutation(self):
        legacy = {"_id": "example.com", "url": "https://example.com/"}
        database = FakeDatabase(target_documents=[legacy])
        with patch("crawl_seeds.write_seed_documents") as writer:
            with self.assertRaises(IncompatibleCrawlSeedCollection) as raised:
                bootstrap_database(database, [self.make_document()])
        writer.assert_not_called()
        self.assertIn("production migration is out of scope", str(raised.exception).lower())
        self.assertEqual([], database.commands)
        self.assertEqual([], database.collections[COLLECTION_NAME].indexes)
        self.assertEqual([legacy], database.collections[COLLECTION_NAME].documents)

    def test_manual_baseline_is_exactly_70_direct_rows_and_deterministic(self):
        first = build_manual_seed_documents(self.csv_path)
        second = build_manual_seed_documents(self.csv_path)
        self.assertEqual(EXPECTED_MANUAL_ROWS, len(first))
        self.assertEqual(first, second)
        self.assertEqual(
            sorted(document["_id"] for document in first),
            [document["_id"] for document in first],
        )
        for document in first:
            self.assertEqual(ROOT_FIELDS, frozenset(document))
            self.assertEqual(MANUAL_BASELINE_OBSERVED_AT, document["discovered_at"])
            self.assertEqual(MANUAL_BASELINE_OBSERVED_AT, document["updated_at"])
            self.assertEqual(1, len(document["sources"]))
            self.assertEqual("manual", document["sources"][0]["type"])

    def test_merge_uses_newest_observation_and_stored_source_wins_ties(self):
        existing = self.make_document(priority=2, metadata={"score": 20})
        source_key = existing["sources"][0]["key"]

        older = self.make_document(
            observed_at=self.observed - timedelta(days=1),
            priority=1,
            metadata={"score": 1},
            category="General",
        )
        merged_older = merge_seed_documents(existing, older)
        self.assertEqual({"score": 20}, merged_older["sources"][0]["metadata"])
        self.assertEqual(2, merged_older["priority"])
        self.assertEqual("Technology", merged_older["sources"][0]["category"])

        tied = self.make_document(
            observed_at=self.observed,
            priority=1,
            metadata={"score": 999},
            category="General",
        )
        merged_tie = merge_seed_documents(existing, tied)
        self.assertEqual(source_key, merged_tie["sources"][0]["key"])
        self.assertEqual({"score": 20}, merged_tie["sources"][0]["metadata"])
        self.assertEqual(2, merged_tie["priority"])

        # Values in the same BSON millisecond are ties even if their original
        # Python datetimes had different microseconds.
        same_millisecond_existing = self.make_document(
            observed_at=self.observed.replace(microsecond=123100),
            metadata={"score": 30},
        )
        same_millisecond_incoming = self.make_document(
            observed_at=self.observed.replace(microsecond=123900),
            priority=1,
            metadata={"score": 31},
        )
        merged_millisecond_tie = merge_seed_documents(
            same_millisecond_existing, same_millisecond_incoming
        )
        self.assertEqual(
            {"score": 30}, merged_millisecond_tie["sources"][0]["metadata"]
        )

        newer = self.make_document(
            observed_at=self.observed + timedelta(days=1),
            priority=1,
            metadata={"score": 1000},
            category="General",
        )
        merged_newer = merge_seed_documents(existing, newer)
        self.assertEqual({"score": 1000}, merged_newer["sources"][0]["metadata"])
        self.assertEqual(1, merged_newer["priority"])
        self.assertEqual(["General"], merged_newer["categories"])

        self.assertIn("$gte", repr(mongo_merge_pipeline(newer)))

    def test_merge_preserves_distinct_provenance_and_recomputes_rollups(self):
        first_time = self.observed
        second_time = first_time + timedelta(days=1)
        manual_source = make_source(
            source_type="manual",
            source_ref="manual#example",
            raw_url="https://example.com/path",
            category="Technology",
            priority=2,
            observed_at=first_time,
            metadata={"notes": "curated"},
        )
        reddit_source = make_source(
            source_type="reddit_json",
            source_ref="https://old.reddit.com/r/test/comments/1/post/",
            raw_url="https://example.com/path#fragment",
            category="Reference & Research",
            priority=1,
            observed_at=second_time,
            metadata={"score": 100},
        )
        existing = new_seed_document("https://example.com/path", manual_source)
        existing["enabled"] = False
        incoming = new_seed_document(
            "https://example.com/path#fragment", reddit_source
        )
        merged = merge_seed_documents(existing, incoming)
        self.assertFalse(merged["enabled"])
        self.assertEqual(1, merged["priority"])
        self.assertEqual(
            ["Reference & Research", "Technology"], merged["categories"]
        )
        self.assertEqual(2, len(merged["sources"]))
        self.assertEqual(first_time, merged["discovered_at"])
        self.assertEqual(second_time, merged["updated_at"])

    def test_local_target_and_confirmation_are_bound_to_parsed_uri(self):
        target = parse_local_mongo_target(
            "mongodb://localhost:27017/mifolyo_index"
        )
        self.assertEqual(
            "localhost:27017/mifolyo_index/crawl_seeds", target.confirmation
        )
        for environment_name in ("development", "test"):
            with self.subTest(environment_name=environment_name):
                require_rebuild_authorization(
                    target,
                    target.confirmation,
                    environ={"MIFOLYO_ENV": environment_name},
                )
                with self.assertRaises(PermissionError):
                    require_rebuild_authorization(
                        target,
                        "mongo:27017/mifolyo_index/crawl_seeds",
                        environ={"MIFOLYO_ENV": environment_name},
                    )
        with self.assertRaises(PermissionError):
            require_rebuild_authorization(
                target,
                target.confirmation,
                environ={},
            )

        for uri in (
            "mongodb://db.example.com:27017/mifolyo_index",
            "mongodb://localhost:27018/mifolyo_index",
            "mongodb://localhost:27017/production",
            "mongodb+srv://cluster.example.com/mifolyo_index",
        ):
            with self.subTest(uri=uri), self.assertRaises(PermissionError):
                parse_local_mongo_target(uri)

    def test_unsafe_rebuild_environments_are_rejected_before_mutation(self):
        target = LocalMongoTarget("localhost", 27017)
        legacy = {"legacy": True}
        unsafe_environments = (
            ("unset", {}),
            ("empty", {"MIFOLYO_ENV": ""}),
            ("production", {"MIFOLYO_ENV": "production"}),
            ("staging", {"MIFOLYO_ENV": "staging"}),
        )

        for label, environ in unsafe_environments:
            with self.subTest(environment=label):
                database = FakeDatabase(target_documents=[legacy])
                with patch("crawl_seeds.write_seed_documents") as writer:
                    with self.assertRaises(PermissionError):
                        rebuild_database(
                            database,
                            [self.make_document()],
                            mongo_uri="mongodb://localhost:27017/mifolyo_index",
                            confirmation=target.confirmation,
                            environ=environ,
                            temporary_name="crawl_seeds__must_not_exist",
                        )

                writer.assert_not_called()
                self.assertEqual([], database.created)
                self.assertEqual([], database.commands)
                self.assertEqual([], database.dropped)
                self.assertEqual([], database.events)
                self.assertEqual(
                    [legacy], database.collections[COLLECTION_NAME].documents
                )

    def test_rebuild_stages_validates_then_atomically_renames(self):
        old_target = {"legacy": True}
        database = FakeDatabase(target_documents=[old_target])
        target = LocalMongoTarget("localhost", 27017)
        documents = build_manual_seed_documents(self.csv_path)[:2]
        stage_name = "crawl_seeds__rebuild_v1_test"

        def fake_write(collection, incoming):
            self.assertEqual([old_target], database.collections[COLLECTION_NAME].documents)
            collection.documents = copy.deepcopy(list(incoming))
            database.events.append(("write", collection.name))
            return "written"

        with patch("crawl_seeds.write_seed_documents", side_effect=fake_write):
            result = rebuild_database(
                database,
                documents,
                mongo_uri="mongodb://localhost:27017/mifolyo_index",
                confirmation=target.confirmation,
                environ={"MIFOLYO_ENV": "test"},
                temporary_name=stage_name,
            )

        self.assertEqual("written", result)
        self.assertEqual(documents, database.collections[COLLECTION_NAME].documents)
        self.assertNotIn(stage_name, database.collections)
        self.assertEqual([], database.dropped)
        validate_index = database.events.index(("validate", stage_name))
        rename_event = ("rename", stage_name, COLLECTION_NAME, True)
        self.assertGreater(database.events.index(rename_event), validate_index)

    def test_failed_staged_rebuild_leaves_target_untouched_and_cleans_stage(self):
        old_target = {"legacy": True}
        database = FakeDatabase(
            target_documents=[old_target], validation_valid=False
        )
        target = LocalMongoTarget("localhost", 27017)
        documents = build_manual_seed_documents(self.csv_path)[:1]
        stage_name = "crawl_seeds__rebuild_v1_failure"

        def fake_write(collection, incoming):
            collection.documents = copy.deepcopy(list(incoming))
            return "written"

        with patch("crawl_seeds.write_seed_documents", side_effect=fake_write):
            with self.assertRaises(RuntimeError):
                rebuild_database(
                    database,
                    documents,
                    mongo_uri="mongodb://localhost:27017/mifolyo_index",
                    confirmation=target.confirmation,
                    environ={"MIFOLYO_ENV": "development"},
                    temporary_name=stage_name,
                )

        self.assertEqual([old_target], database.collections[COLLECTION_NAME].documents)
        self.assertNotIn(stage_name, database.collections)
        self.assertEqual([stage_name], database.dropped)
        self.assertFalse(any(event[0] == "rename" for event in database.events))


if __name__ == "__main__":
    unittest.main()
