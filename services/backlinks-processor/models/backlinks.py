from dataclasses import asdict, dataclass
from typing import Any, Dict, Set, Tuple


@dataclass(frozen=True)
class BacklinkMember:
    raw: bytes
    url: str


@dataclass(frozen=True)
class BacklinkBatch:
    redis_key: bytes
    target_url: str
    members: Tuple[BacklinkMember, ...]
    encoded_url_bytes: int


@dataclass
class Backlinks:
    _id:    str # page_url
    links:  Set[str]

    def to_dict(self) -> Dict[str, Any]:
        # Convert to dictionary
        data = asdict(self)
        data["links"] = list(self.links)
        return data

    def prettify(self) -> str:
        links_str = "\n" + "\n".join(f"\t│ \t - {link}" for link in self.links) if self.links else "\tNone"
        return f"""
        ┌──────────────────────────────────────────────────────┐
        │ IMAGE URL: {self._id}
        │
        │ BACKLINKS: {links_str}
        └──────────────────────────────────────────────────────┘
        """
