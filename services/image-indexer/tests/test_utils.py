import unittest
from unittest.mock import patch

from utils.utils import is_valid_image


class IsValidImageTest(unittest.TestCase):
    @patch("utils.utils.Image.open")
    @patch("utils.utils.requests.get")
    def test_requests_absolute_urls_unchanged_and_falls_back_for_legacy_url(
        self, mock_get, mock_image_open
    ):
        mock_get.return_value.content = b"image"
        mock_image_open.return_value.size = (100, 100)
        cases = [
            ("http://cdn.example.com/image.jpg", "http://cdn.example.com/image.jpg"),
            ("https://cdn.example.com/image.jpg", "https://cdn.example.com/image.jpg"),
            ("cdn.example.com/legacy.jpg", "https://cdn.example.com/legacy.jpg"),
        ]

        for url, expected_url in cases:
            with self.subTest(url=url):
                mock_get.reset_mock()
                mock_image_open.reset_mock()

                self.assertTrue(is_valid_image(url))
                mock_get.assert_called_once_with(expected_url, timeout=5)
                mock_image_open.assert_called_once()
