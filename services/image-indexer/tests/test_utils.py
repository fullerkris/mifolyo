import inspect
import unittest

import main
from models.image import Image
from utils import utils


class MetadataOnlyTests(unittest.TestCase):
    def test_filename_and_tokens_are_derived_without_network(self):
        image = Image.from_contract(
            {
                "contract_version": "1",
                "publication_id": "a" * 64,
                "normalized_page_url": "https://example.org/page",
                "normalized_source_url": "https://cdn.example.org/Blue-Sky.jpg?q=1",
                "alt": "Blue sky",
            },
            "a" * 64,
            "https://example.org/page",
        )
        self.assertEqual("Blue-Sky.jpg", image.filename)
        self.assertEqual(["blue", "sky"], utils.split_name(image.filename))

    def test_runtime_has_no_http_or_image_decoder_fetch_path(self):
        source = inspect.getsource(utils) + inspect.getsource(main)
        for forbidden in ("requests", "PIL", "urlopen", "httpx", "aiohttp"):
            self.assertNotIn(forbidden, source)
        self.assertFalse(hasattr(utils, "is_valid_image"))


if __name__ == "__main__":
    unittest.main()
