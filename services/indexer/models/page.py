import json
from datetime import datetime
from typing import List, Dict, Any
from dataclasses import dataclass
from email.utils import parsedate_to_datetime

@dataclass
class Page:
    normalized_url: str
    html: str
    content_type: str
    status_code: int
    last_crawled: datetime
    rendered: bool = False
    original_html: str = ""
    render_policy_rule: str = ""
    render_policy_sha256: str = ""

    @classmethod
    def from_hash(cls, page_data: Dict[str, Any]) -> 'Page':

        if page_data == None:
            return None

        # Parse fields
        last_crawled = parsedate_to_datetime(page_data['last_crawled'])

        rendered_value = page_data.get("rendered", "false")
        if rendered_value not in {"true", "false"}:
            raise ValueError("rendered must be true or false")
        rendered = rendered_value == "true"
        original_html = page_data.get("original_html", "")
        render_policy_rule = page_data.get("render_policy_rule", "")
        render_policy_sha256 = page_data.get("render_policy_sha256", "")
        valid_policy_sha256 = (
            len(render_policy_sha256) == 64
            and all(character in "0123456789abcdef" for character in render_policy_sha256)
        )
        if rendered and (not original_html or not render_policy_rule or not valid_policy_sha256):
            raise ValueError("rendered page is missing provenance")
        if not rendered and (original_html or render_policy_rule or render_policy_sha256):
            raise ValueError("static page contains rendered-page provenance")

        return cls (
            normalized_url=page_data['normalized_url'],
            html=page_data['html'],
            content_type=page_data['content_type'],
            status_code=int(page_data['status_code']),
            last_crawled=last_crawled,
            rendered=rendered,
            original_html=original_html,
            render_policy_rule=render_policy_rule,
            render_policy_sha256=render_policy_sha256,
        )

    def prettify(self) -> str:
        return f"""
        -----------------------------------------------------
        URL: {self.normalized_url}
        HTML: {self.html[:15] + "..." if len(self.html) > 15 else self.html}
        Content Type: {self.content_type}
        Status Code: {self.status_code}
        Last Crawled: {self.last_crawled}
        -----------------------------------------------------
        """
