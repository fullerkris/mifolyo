import hashlib
from dataclasses import asdict, dataclass
from typing import Any, Dict

from utils.utils import image_filename


@dataclass(frozen=True)
class Image:
    # Mongo identity is the page/image association. Query mapping restores the
    # source URL as the public result _id.
    _id: str
    source_url: str
    page_url: str
    alt: str
    filename: str

    @classmethod
    def from_contract(
        cls, payload: Dict[str, Any], publication_id: str, page_url: str
    ) -> "Image":
        if not isinstance(payload, dict):
            raise TypeError("image payload must be an object")
        if not isinstance(publication_id, str) or not isinstance(page_url, str):
            raise TypeError("image identity must be text")
        required = {
            "contract_version",
            "publication_id",
            "normalized_page_url",
            "normalized_source_url",
            "alt",
        }
        if set(payload) != required:
            raise ValueError("image payload fields do not match contract")
        if payload["contract_version"] != "1":
            raise ValueError("unsupported image payload contract")
        if payload["publication_id"] != publication_id:
            raise ValueError("image payload publication mismatch")
        if payload["normalized_page_url"] != page_url:
            raise ValueError("image payload page mismatch")
        source_url = payload["normalized_source_url"]
        alt = payload["alt"]
        if not isinstance(source_url, str) or not source_url:
            raise ValueError("image source is required")
        if not isinstance(alt, str):
            raise TypeError("image alt must be text")
        return cls(
            _id=cls.association_id(page_url, source_url),
            source_url=source_url,
            page_url=page_url,
            alt=alt,
            filename=image_filename(source_url),
        )

    @staticmethod
    def association_id(page_url: str, source_url: str) -> str:
        if not isinstance(page_url, str) or not isinstance(source_url, str):
            raise TypeError("association URLs must be text")
        return hashlib.sha256(f"{page_url}\0{source_url}".encode("utf-8")).hexdigest()

    def to_dict(self) -> Dict[str, Any]:
        return asdict(self)
