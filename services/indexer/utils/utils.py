import re
import py3langid as langid

from bs4 import BeautifulSoup, SoupStrainer
from nltk.tokenize import word_tokenize
from utils.constants import FILE_TYPES, POPULAR_DOMAINS

from . import nlp_utils

# Initialize NLTK resources
STOP_WORDS = nlp_utils.initialize_nlp()
stop_words_set = set(STOP_WORDS)

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


def get_meta_content(soup, property_value=None, name_value=None):
    tag = None
    if property_value:
        tag = soup.find("meta", property=property_value)
    if not tag and name_value:
        tag = soup.find("meta", attrs={"name": name_value})

    if tag and "content" in tag.attrs:
        return tag["content"]

    return None


def process_text(soup):
    unfiltered_text = soup.get_text()
    unfiltered_text = WHITESPACE_PATTERN.sub(
        " ", NEWLINE_PATTERN.sub(" ", unfiltered_text)
    ).strip()

    # Get the summary_text
    summary_text = unfiltered_text

    # Tokenize the text
    filtered_text = word_tokenize(unfiltered_text)

    # Check that we don't have any stop words or punctuation
    filtered_text = [
        word.lower()
        for word in filtered_text
        if word.lower() not in stop_words_set and word.lower().isalnum()
    ]

    # TODO: Return language as well
    return {
        "summary_text": summary_text,
        "filtered_text": filtered_text,
        # RETURN LANGUAGE HERE
    }


def detect_language(text, sample_size=1000):
    # Only use first 1000 characters for language detection
    sample = text[:sample_size]
    language, confidence = langid.classify(sample)
    return language, confidence


def tokenize_large_text(text, chunk_size=10000):
    # Tokenize large text in manageable chunks
    tokens = []
    for i in range(0, len(text), chunk_size):
        chunk = text[i : i + chunk_size]
        tokens.extend(word_tokenize(chunk))
    return tokens


def extract_page_text(soup, rendered=False):
    if not rendered:
        paragraphs = soup.select("p")
        return " ".join(
            BRACKETS_PATTERN.sub("", paragraph.get_text()).strip()
            for paragraph in paragraphs
        )

    for element in soup.select(
        "script, style, noscript, template, svg, [hidden], [aria-hidden='true']"
    ):
        element.decompose()
    content = (
        soup.select_one("main")
        or soup.select_one("[role='main']")
        or soup.select_one("article")
        or soup.body
        or soup
    )
    return BRACKETS_PATTERN.sub("", content.get_text(" ", strip=True)).strip()


def get_html_data(html: str, rendered=False):
    # Static pages retain the existing paragraph-only behavior. Rendered pages
    # need their full semantic DOM because custom elements often contain no p tags.
    parse_only = None if rendered else SoupStrainer(["meta", "p", "title"])
    soup = BeautifulSoup(html, "lxml", parse_only=parse_only)

    # Get all meta tags
    meta_tags = {
        (meta.get("property") or meta.get("name")): meta.get("content")
        for meta in soup.select("meta[property], meta[name]")
        if meta.get("content")
    }

    title = meta_tags.get("og:title") or meta_tags.get("title")
    description = meta_tags.get("og:description") or meta_tags.get("description")
    canonical_url = meta_tags.get("og:url") or meta_tags.get("url")

    page_text = extract_page_text(soup, rendered=rendered)

    # Summary text: take first 500 words
    summary_text = page_text.split()
    summary_text = (
        " ".join(summary_text)
        if len(summary_text) < 500
        else " ".join(summary_text[:500])
    )

    # Process text
    tokens = tokenize_large_text(page_text)

    # Check that we don't have any stop words or punctuation
    filtered_text = [
        word.lower()
        for word in tokens
        if word.lower() not in stop_words_set and word.lower().isalnum()
    ]

    language, _ = detect_language(summary_text)

    return {
        "title": title,
        "description": description,
        "summary_text": summary_text,
        "text": filtered_text,
        "language": language,
    }
