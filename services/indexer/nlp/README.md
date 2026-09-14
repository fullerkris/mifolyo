# Fixed-English inference — private provisioning / distribution blocked

**The project owner explicitly selected private build-time provisioning.** Keep
the five corpus files **OUT of Git**, fetch pinned/hash-verified archives only as
an explicit private build/test setup action, and run inference offline. This is
project workflow authorization, **not legal clearance or permission to publish
data/images**. Keep data-bearing images, caches and artifacts private; the
`--distribution` gate blocks publishing while licensing review is unresolved.

At the pinned `nltk_data` commit, both
`punkt_tab` and `stopwords` are explicitly listed as unclarified/unknown in
[`provenance/DATASET-LICENSES.md`](provenance/DATASET-LICENSES.md) (lines 152–203).
[`provenance/LICENSE-OVERVIEW.md`](provenance/LICENSE-OVERVIEW.md) says not to
assume the repository Apache license permits dataset redistribution. The package
XML metadata supplies no license. English Punkt's upstream README describes
training on Penn Treebank / Wall Street Journal, contributed by Jan Strunk /
Tibor Kiss; attribution and training-corpus provenance are not a license grant.
The stopwords README describes PostgreSQL/Snowball ancestry and an augmented
English list; that does not establish rights to this exact augmented snapshot.

Obtain a documented redistribution grant or qualified legal clearance for the
**exact** pinned English assets before distributing data or images. Do not silently replace
the model, assume public-domain status, copy an old pickle, or turn off the gate.
This repository's MIT license does not relicense upstream Apache source or data.

## Contract and boundaries

```python
from nlp import word_tokenize, english_stopwords
```

* `word_tokenize(text: str) -> list[str]`: NLTK **3.10.3 default English**,
  `preserve_line=False`: Punkt sentence segmentation (including closing-quote
  realignment), then `NLTKWordTokenizer`, not `TreebankWordTokenizer`.
* `english_stopwords() -> frozenset[str]`: the pinned English stopword set.
* Case, punctuation and contractions are preserved exactly as the reference
  tokenizer produces them. Caller `lower()`, `.isalnum()` filtering, stopword
  filtering and **10,000-character chunking** remain caller responsibilities.
* No language/resource-path options, environment lookup, installed-NLTK imports,
  cache-directory lookup, downloads, network operations, or writable persistence.
* `NLPDataError` (a `RuntimeError`) is raised on missing, altered, extra, unreadable,
  or symlinked bundled data. Even `word_tokenize("")` verifies the model first.
  A clean checkout is deliberately unprovisioned, so both public functions fail
  explicitly until approved private setup installs the five fixed files.
* The entire data bundle is verified on first use and cached in memory. Deploy
  immutable, non-writable application files; this is not continuous filesystem
  attestation, a concurrent-filesystem-writer sandbox, or protection against an
  attacker who can replace the interpreter, wrapper, or verifier itself.
* No user text is logged or persisted. Only model data/regexes are cached;
  zero-on-miss orthographic lookups do not store previously unseen input words.
* Python 3.10+ and the standard library only; no custom package metadata and no
  attempt to disguise or rename the full NLTK dependency.

## Source and provenance

| Component | Immutable identity |
| --- | --- |
| NLTK `v3.10.3` | `303f6e2ba8e4548a5f54fd65d86bb5c9a949f1db` |
| `nltk_data` `gh-pages` snapshot | `550b6625bcef1f2abff2ff770a5a0d272c9c6b2a` |
| `punkt_tab.zip` SHA-256 | `e57f64187974277726a3417ca6f181ec5403676c717672eef6a748a7b20e0106` |
| `stopwords.zip` SHA-256 | `48c0e52d8b52546e827f53761fb30300c0ab94f70660d28bd65ba0a86270946b` |

`component.json` records complete upstream file identities (SHA-256 and Git blob
SHA-1), selected classes/members, exact adaptations, advisory disposition,
maintenance obligations, local file inventory/hashes and required-but-untracked
asset identities. `_asset_hashes.py` is the runtime's fixed five-file allowlist.
Original headers and the exact Apache license text are retained; see `NOTICE`.
`provenance/data-index.json` retains only the two relevant public metadata records
from pinned `nltk_data/index.xml`, including its full-file SHA-256
`97dce5e72320cd9850b7c20130196006710c18f9c03134c822a37da330198bf6`.
The index advertises mutable `gh-pages` archive URLs; those are recorded for
provenance only. Provisioning fetches **only the immutable commit URLs** and never
consults a live index. The source generator authenticates the pinned full index
before extracting its metadata subset; no corpus list is embedded in that subset.

Only inference portions of `nltk/tokenize/punkt.py` and
`nltk/tokenize/destructive.py` are retained. The only contraction helper is the
required `MacIntyreContractions.CONTRACTIONS2/3` in `destructive.py` itself.
Trainers, legacy pickle loading, model loading/saving/export, debug file dumping,
parameter mutation/debug APIs, alternate sentence APIs, unused token properties,
word-span alignment, optional PTB-parenthesis conversion, the deprecated warning
option, and all downloader/resource/parser/tagger/classifier frameworks are
physically absent. Retained upstream inference bodies/regexes are source-sliced,
not regenerated by an AST pretty-printer. See the generator's explicit allowlists.

## Explicit private build/test provisioning

Run from `services/indexer`. Supply an **existing, non-symlink cache directory
outside the workspace/build source tree** (for example a private directory under
`/tmp` in a build container). The parent build/test integration creates that
directory, keeps `nlp/data` out of Git/build contexts, and runs the unchanged
dependency audit before provisioning. No application init or runtime import may
invoke the provisioner.

```sh
python -B tools/vendor_nlp.py --cache /private/external/nlp-cache --fetch --provision-data
# An already-populated private cache can also provision completely OFFLINE:
python -B tools/vendor_nlp.py --cache /private/external/nlp-cache --provision-data
python -B verify_nlp.py
```

This mode needs **only `punkt_tab.zip` and `stopwords.zip`**, not full NLTK source,
source-generation inputs, an installed NLTK package, or a network index request.
It authenticates both archives' exact sizes/SHA-256 **before ZIP parsing**, then
checks the exact sizes, hashes, record counts and regular-file types of these
five fixed selected members and writes only:

```text
nlp/data/punkt_tab/english/collocations.tab
nlp/data/punkt_tab/english/sent_starters.txt
nlp/data/punkt_tab/english/abbrev_types.txt
nlp/data/punkt_tab/english/ortho_context.tab
nlp/data/english_stopwords.txt
```

There is no generic `extractall`, archive-controlled output path, pickle, language
option, runtime fetch, or automatic network fallback. `--fetch` is the **only**
network permission. Missing inputs without it fail. HTTP redirects are rejected
before following them; no alternative URLs are tried. Existing cache files and
assets must be regular, exact-size/hash matches; symlinks, extras and stale or
tampered files are rejected, **not repaired or overwritten**, even with `--fetch`.
Correct assets are left untouched, so provisioning is idempotent. Source/security
and existing-asset preflight runs before downloads; complete source/asset
verification runs after provisioning. Interrupted partial output cannot pass the
normal verifier; inspect/remove only failed generated files before retrying. This
is not a sandbox against a concurrent privileged filesystem writer.

## Offline verification and distribution gate

Run from `services/indexer` with bytecode disabled (or redirected outside `nlp`).
The exact inventory deliberately rejects **all extras**, including `__pycache__`,
`.pyc`, unexpected directories and symlinks; there is no broad cache exclusion.

```sh
python -B verify_nlp.py                  # source/security/assets: requires private provisioning
python -B verify_nlp.py --source-only    # source + any present assets; missing corpus permitted
python -B verify_nlp.py --distribution   # also enforces data/image redistribution approval
python -B verify_nlp.py --source-only --distribution  # blocks publishing even without corpus
```

The independent stdlib verifier never imports `nlp` or NLTK. A digest anchored
outside the package authenticates the inventory relative to the reviewed source
tree. It verifies exact inventory, bytes, hashes, ancestry, AST import allowlists
and forbidden persistence/training/network/dynamic-code entrypoints.
Default integrity verification can pass for a privately provisioned bundle while
reporting `private_build_ready=true`, `distribution_allowed=false` and the exact
unresolved license status. Integrity success does **not** clear licensing.
`--source-only` does not require corpus provisioning, but never ignores altered
or extra files that are present. It can be combined with `--distribution`.
`--component-dir` is for maintenance/negative tests, not a runtime resource API.

**Before any Indexer registry login/push**, the parent release workflow must run,
from the repository root:

```sh
python3 services/indexer/verify_nlp.py --source-only --distribution
```

This exits nonzero while rights are unclarified, even if private assets verify.
Do not suppress that failure, publish data-bearing image artifacts via another
job, or substitute a default/source-only integrity pass. Self-tests cannot be
combined with `--distribution` to bypass the actual gate. The verifier adds no
advisory suppression and makes no claim that private workflow approval is a
dataset license grant.

## Source-only maintenance and reproducibility

The source extractor never runs at runtime, package import, or ordinary build/test
initialization; private builds invoke only the explicit data-provisioning mode.
`--fetch` is the **only** way the tool accesses the network. All URLs
contain the pinned commits and every input is checked against a fixed SHA-256.
It reads selected ZIP members, never executes upstream code or extracts paths
from an archive. Source regeneration is a distinct maintenance action, not a
dependency of data provisioning.

```sh
# Explicit maintenance only. Create/verify this parent directory beforehand.
python -B tools/vendor_nlp.py --cache /approved/temp/nlp-component-work --fetch --write
# Normal reproduction using the already-populated, hash-checked cache: OFFLINE.
python -B tools/vendor_nlp.py --cache /approved/temp/nlp-component-work --check
# Optional component-only synthetic checks in a disposable private asset copy.
python -B tools/vendor_nlp.py --cache /approved/temp/nlp-component-work --test-private-bundle
python -B verify_nlp.py --self-test-work-dir /approved/temp/nlp-component-work
```

`--write` writes only named generated files and `component.json` under `nlp`.
It does not edit the verifier's inventory trust anchor. Review every source and
inventory diff, then update that anchor manually; do not blindly bless hashes.
Missing/tampered cache inputs fail; `--fetch` does not repair a corrupt cache.
The generated output is deterministic and has no timestamps, host paths or
installed-package dependencies. Manual wrapper/docs changes require re-generating
the inventory and reviewing its new digest, not editing generated source.
The optional private-bundle test executes only a disposable copy of this component
(not upstream NLTK), using the already-verified cache. It tests tokens, stopwords,
no model-cache growth, synthetic names/pronouns, missing/corrupt/extra/symlinked
assets, archive authentication before ZIP parsing, cache/output rejection,
idempotent provisioning, and both forms of the distribution gate. This test needs
only the two data archives, and never provisions the in-repo data
directory and is not a replacement for independent reference/application goldens.
The verifier checks pinned provenance claims and trusted extracted-file hashes
offline; reconstruction from the original upstream bytes is the generator's
separate `--check` operation and requires its private input cache.

## Security disposition and maintenance ownership

**Owner: mifolyo Indexer maintainers**, not upstream NLTK. This choice transfers
responsibility for monitoring upstream security releases, reviewing the retained
inference call graph and regex changes, license/provenance updates, supported
Python versions, test coverage and release approvals to that team. Re-review on
every change to either pin, extraction selection, wrapper, model or interpreter.

`PYSEC-2026-3740` / `GHSA-8mgp-746c-j5xp` / `CVE-2026-81726` concerns model-artifact
APIs bypassing pathsec (`TransitionParser.train/parse`,
`AveragedPerceptron.save/load`, `PerceptronTagger.save_to_json`,
`save_maxent_params`). Their modules/classes/functions are **not retained**.
Adjacent Punkt save/load/dump and legacy deserialization APIs are also removed.
This is a removed-code disposition, **not** an upstream patch, an advisory ignore,
or a claim that all NLTK vulnerabilities are fixed. The independent verifier adds
source-integrity/surface coverage; **pip-audit does not inspect vendored source**.
Keep the existing pip-audit invocation and policy unchanged for dependencies.

For the approved private workflow, require the exact privately provisioned bundle,
a passing default verifier and independent offline goldens/application parity
tests against both pins. Before data/image distribution, additionally require
documented rights and a passing `--distribution` gate; never check corpus bytes
into Git. Tests must include Unicode/quotes/contractions, empty text and
10,000-character boundaries. Test representative names, pronouns and dialects
across demographic groups for differential tokenization/term-loss behavior;
parity alone does not establish fairness of this English newspaper-trained model.
No demographic classification, content moderation, or safety judgment is made
by tokenization; untrusted text remains data, never instructions or model paths.
Keep downstream content safety and request-size/time/resource controls. This
component does not claim worst-case regex latency bounds or input sanitization.
