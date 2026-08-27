import sys
import types
import unittest
from pathlib import Path


SERVICE_ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(SERVICE_ROOT))

fake_nlp_utils = types.ModuleType("utils.nlp_utils")
fake_nlp_utils.initialize_nlp = lambda: []
sys.modules["utils.nlp_utils"] = fake_nlp_utils

from bs4 import BeautifulSoup  # noqa: E402
from utils.utils import extract_page_text  # noqa: E402


class RenderedTextTests(unittest.TestCase):
    def test_static_extraction_remains_paragraph_only(self):
        soup = BeautifulSoup(
            "<html><body><div>ignored</div><p>kept paragraph</p></body></html>",
            "lxml",
        )
        self.assertEqual("kept paragraph", extract_page_text(soup))

    def test_rendered_extraction_reads_custom_elements_and_removes_hidden_content(self):
        soup = BeautifulSoup(
            """
            <html><body><main><gf-root>Rendered font catalog</gf-root>
            <div hidden>hidden words</div><script>ignored()</script></main></body></html>
            """,
            "lxml",
        )
        self.assertEqual(
            "Rendered font catalog",
            extract_page_text(soup, rendered=True),
        )

    def test_rendered_extraction_prefers_main_over_earlier_article(self):
        soup = BeautifulSoup(
            "<html><body><article>teaser</article><main>primary content</main></body></html>",
            "lxml",
        )
        self.assertEqual("primary content", extract_page_text(soup, rendered=True))


if __name__ == "__main__":
    unittest.main()
