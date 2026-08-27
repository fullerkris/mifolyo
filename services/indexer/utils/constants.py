# Constants

# Message Queues
INDEXER_QUEUE_KEY = "pages_queue"
# Crash-safe claims live here until completion. Multiple replicas may start,
# but the renewable owner lock permits exactly one to process or recover work.
INDEXER_PROCESSING_QUEUE_KEY = "pages_queue:processing"
INDEXER_DEAD_LETTER_QUEUE_KEY = "pages_queue:dead"
INDEXER_DEAD_LETTER_MAX_ENTRIES = 1000
INDEXER_OWNER_LOCK_KEY = "pages_queue:indexer_owner"
INDEXER_FENCE_EPOCH_KEY = "pages_queue:indexer_fence_epoch"
INDEXER_LOCK_TTL_SECONDS = 60
INDEXER_LOCK_RENEW_SECONDS = 10
INDEXER_RECOVERY_BATCH_SIZE = 100
INDEXER_CLAIM_POLL_SECONDS = 0.1
MAX_CANONICAL_URL_BYTES = 2048
SIGNAL_QUEUE_KEY = "signal_queue"
RESUME_CRAWL = "RESUME_CRAWL"
IMAGE_INDEXER_QUEUE_KEY = "image_indexer_queue"

# Redis Data
NORMALIZED_URL_PREFIX = "normalized_url"
URL_METADATA_PREFIX = "url_metadata"
PAGE_IMAGES_PREFIX = "page_images"
WORD_IMAGES_PREFIX = "word_images"
IMAGE_PREFIX = "image_data"
PAGE_PREFIX = "page_data"
WORD_PREFIX = "word"

# These ones will be saved by the indexer and the backlinks server
BACKLINKS_PREFIX = "backlinks"
OUTLINKS_PREFIX = "outlinks"

# Maximum words to index
MAX_INDEX_WORDS = 1000

# File extensions (used to omit them when indexing images)
FILE_TYPES = ["png", "svg", "ico", "gif", "jpeg", "jpg"]

# Common Top-Level Domains (TLDs) this is clearly AI generated
# This also include other common words like google, facebook, wikpedia and so on
POPULAR_DOMAINS = [
    # Generic TLDs
    "com",
    "org",
    "net",
    "edu",
    "gov",
    "mil",
    "int",
    "biz",
    "info",
    "name",
    "pro",
    "xyz",
    "online",
    "site",
    "shop",
    "store",
    "blog",
    "news",
    "media",
    "art",
    "film",
    "game",
    "games",
    "tech",
    "app",
    "dev",
    "ai",
    "cloud",
    "io",
    "co",
    "me",
    "tv",
    "ly",
    "to",
    "fm",
    "wiki",
    "help",
    # Country Code TLDs
    "us",
    "uk",
    "ca",
    "au",
    "de",
    "fr",
    "jp",
    "cn",
    "ru",
    "br",
    "in",
    "cl",
    "mx",
    "es",
    "it",
    "nl",
    "se",
    "no",
    "fi",
    "dk",
    "pl",
    "be",
    "ch",
    "at",
    "nz",
    "za",
    "sg",
    "hk",
    "kr",
    "id",
    "my",
    "ph",
    "th",
    "vn",
    "il",
    "sa",
    "ae",
    "tr",
    "eg",
    "ar",
    "co",
    "pe",
    "ve",
    "pk",
    "ng",
    "ke",
    "tz",
    "ro",
    # Common Language Subdomains
    "en",  # English
    "es",  # Spanish
    "fr",  # French
    "de",  # German
    "it",  # Italian
    "pt",  # Portuguese
    "nl",  # Dutch
    "ru",  # Russian
    "zh",  # Chinese (Simplified)
    "ja",  # Japanese
    "ko",  # Korean
    "ar",  # Arabic
    "tr",  # Turkish
    "pl",  # Polish
    "sv",  # Swedish
    "no",  # Norwegian
    "da",  # Danish
    "fi",  # Finnish
    "el",  # Greek
    "cs",  # Czech
    "hu",  # Hungarian
    "ro",  # Romanian
    "he",  # Hebrew
    "th",  # Thai
    "id",  # Indonesian
    "ms",  # Malay
    "hi",  # Hindi
    "bn",  # Bengali
    "ur",  # Urdu
    "vi",  # Vietnamese
    # Major Brand & High-Traffic Domains
    "google",
    "facebook",
    "instagram",
    "twitter",
    "tiktok",
    "linkedin",
    "youtube",
    "reddit",
    "wikipedia",
    "yahoo",
    "bing",
    "microsoft",
    "apple",
    "amazon",
    "ebay",
    "netflix",
    "hulu",
    "spotify",
    "pinterest",
    "snapchat",
    "discord",
    "steam",
    "github",
    "gitlab",
    "bitbucket",
    "twitch",
    "paypal",
    "stripe",
    "wordpress",
    "tumblr",
    "medium",
    "quora",
    "stackoverflow",
    "dropbox",
    "icloud",
    "adobe",
    "salesforce",
    "slack",
    "zoom",
    "airbnb",
    "uber",
    "lyft",
    "doordash",
    "tesla",
    "openai",
    "nvidia",
    "amd",
    "intel",
    "samsung",
    "huawei",
    "xiaomi",
    "sony",
    "bbc",
    "cnn",
    "nytimes",
    "forbes",
    "bloomberg",
    "wsj",
    "reuters",
]
