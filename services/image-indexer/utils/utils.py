import re

from utils.constants import FILE_TYPES, POPULAR_DOMAINS

# Compile regex patterns once at module level
NAME_SPLIT_PATTERN = re.compile(r"[-_./\s]+")
URL_SPLIT_PATTERN = re.compile(r"[.,\-_\/+\:\(\)]")
NEWLINE_PATTERN = re.compile(r"\n+")
WHITESPACE_PATTERN = re.compile(r"\s+")
BRACKETS_PATTERN = re.compile(r"\[.*?\]")


def split_name(filename: str):
    parts = NAME_SPLIT_PATTERN.split(filename)
    parts = [
        part.lower()
        for part in parts
        if part and part.lower() not in FILE_TYPES and "px" not in part.lower()
    ]
    return parts


def split_url(url: str):
    parts = URL_SPLIT_PATTERN.split(url)
    parts = [
        part.lower() for part in parts if part and part.lower() not in POPULAR_DOMAINS
    ]
    return parts


from PIL import Image
import requests
from io import BytesIO


def is_valid_image(url, min_width=100, min_height=100):
    try:
        absUrl = url if url.startswith(("http://", "https://")) else "https://" + url
        response = requests.get(absUrl, timeout=5)
        response.raise_for_status()
        img = Image.open(BytesIO(response.content))
        width, height = img.size
        return width >= min_width and height >= min_height
    except Exception as e:
        print(f"Image at {url} is not valid: {e}")
        return False
