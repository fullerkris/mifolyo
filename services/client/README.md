# Moogle Client

The main client for the Moogle search engine. It provides a user-friendly 2000s-inspired interface for searching the web and viewing results. The client communicates with the backend services using REST APIs and is built with HTML, CSS, and JavaScript.

MiFolyo's current home, search results and source discussions are served by
`services/query-engine` at `/`, `/search?q=...` and `/discussions?url=...`.
This legacy standalone client defaults to the current origin. For a separately
hosted development client, set `VITE_QUERY_ENGINE_URL` to the Query Engine origin
before starting Vite or building (for example `http://127.0.0.1:8000`).
