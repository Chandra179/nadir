---
title: "Trafae"
description: "Trafae searches several open book catalogs and combines their results into one ranked list of books available to read for free."
seoTitle: "Trafae: Search Multiple Open Book Collections"
seoDescription: "Trafae searches multiple book catalogs, removes duplicates, and combines the results into one ranked list."
answerSummary: "Trafae searches several open book catalogs and combines their results into one ranked list of books available to read for free."
tags: [books, search, open-access, aggregation]
links:
  github: "https://github.com/Chandra179/trafae"
created: 2026-10-03
---

# Trafae: Search Multiple Open Book Collections

Trafae searches several open book collections at once and combines the
results into one ranked list. Search for a topic or browse by genre to find
books available to read for free.

It is useful for:

- students and independent learners researching a subject;
- readers of public-domain classics and open-access scholarship;
- researchers looking for freely readable books on a topic; and
- readers who want to search several catalogs in one place.

Trafae does not require an account or host books. It links to the collections
that provide them; availability and access terms may vary by collection.

## How it works

```text
your search or genre browse
        │
        ▼
seven open book collections are asked at the same time
        │
        ▼
duplicates removed · rankings combined · results ordered
        │
        ▼
browse the list, filter it, turn the pages
        │
        ▼
open the book where it can be read for free
```

Trafae presents this through a web page with a search box, a row of
genre shortcuts, and a list of book cards with covers, descriptions, and
each collection's popularity and rating evidence.

### 1. Search or browse

Search for a topic ("psychology", "cartography", "ancient Rome") or browse
a genre. Either can be combined with filters:
language, publication year range, a popularity floor, or a minimum rating.

### 2. Every collection at once

Trafae queries all connected collections at the same time, each with its
own time limit. This is known as
[scatter-gather](https://www.enterpriseintegrationpatterns.com/patterns/messaging/BroadcastAggregate.html):
ask all sources at once, then gather the results. A slow or unreachable
collection does not prevent results from other sources. The page indicates
which collections responded.

### 3. One merged list

The same book found in several collections appears once, keeping every
source's evidence: downloads, ratings, links. The rankings from the
separate collections are then combined using a technique called
[Reciprocal Rank Fusion](https://doi.org/10.1145/1571941.1572114), so that
books that rank highly across several collections move up the list.

### 4. Open and read

Each result links to where the book can be read or downloaded. Trafae
counts anonymous clicks on these links to track how often readers open
books.

## Main features

- **One search, many collections:** seven open book catalogs are searched
  in parallel with a single question.
- **Free to read:** sources provide public-domain, open-access, or other
  books available to read at no cost; access terms may vary.
- **Merged, deduplicated results:** the same book from several catalogs
  collapses into one entry that keeps each source's ratings and popularity.
- **Combined ranking:** each catalog's ordering contributes to one list;
  books ranked highly by multiple catalogs receive a boost.
- **Genre browsing:** browse history, science, psychology and more with no
  search term at all; non-fiction is the default lens.
- **Filters:** language, year range, popularity
  floor, and rating floor are applied to the results you actually see.
- **Transparent sources:** every card shows where a book came from, and
  every search reports which collections answered.
- **Cached searches and pages:** recent searches are saved for a few
  minutes, so revisiting them or changing pages can return quickly.
- **Stable paging:** results keep the same order across pages, and the list
  ends when the collections have no more results available.
- **Shareable searches:** the address captures the search and its filters,
  so the back button works and a link shows a friend exactly what you saw.
- **Partial results:** a blocked or slow collection can limit its own
  results without preventing the rest of the search.

## Where the books come from

| Source | What it offers |
|---|---|
| Open Library | A lending catalog of published books, with community ratings |
| Project Gutenberg | Public-domain books from its catalog, refreshed daily |
| Gutendex | An API for searching Project Gutenberg's catalog |
| Internet Archive | A general digital library, with download counts and ratings |
| DOAB | Scholarly open-access books from academic publishers worldwide |
| Library of Congress | Digital collections from the US national library |
| Wikidata | Community-maintained book records covering subjects the others miss |

Each collection contributes its own ordering. Popularity and ratings are
shown by source rather than combined into a single score.

## How the ranking works

### Combining the collections' opinions

Each collection returns its own best-first list, but they score books in
incompatible ways: one counts downloads, another uses star ratings, a
third just orders results. Trafae therefore compares *positions*, not
scores: a book that appears near the top of any collection's list earns
credit, and appearing high in several lists earns more. This technique is
called Reciprocal Rank Fusion (RRF). It combines rankings without requiring
the collections to use the same scoring system.

### Collapsing duplicates

The same book often appears in three or four collections under slightly
different records. Trafae matches books by identifiers, title, and author,
then keeps one entry with the available evidence from each source.

### Evidence you can filter on

Download counts and star ratings are shown on the results and can be used
as floors: "only books rated four stars or better", "only books many people
have read". Publication year and language are also filterable. Language
aliases such as "english", "eng", and "en" are treated as equivalent.

### Paging that stays fast

When a reader requests page three, Trafae fetches a larger pool of results,
sorts it, returns the requested page, and keeps the remaining results ready.
Later pages can use that pool without changing the order. If a reader moves
beyond the pool, Trafae fetches more results.
Collections can only be asked so deep before they run out of answers
(roughly ten pages of twenty-four books); after that, the list ends.

### Careful matching

Subjects are matched by whole words, so a search for "history" does not
match "prehistory". Non-fiction browsing excludes books marked as fiction.

## Privacy

Trafae has no accounts and stores no personal data. It counts searches,
cache hits, and clicks on book links anonymously. These counters are kept
in memory and reset when the service restarts.

## Known limitations

- **Some catalogs refuse some networks.** DOAB and the Library of Congress
  block some hosting-provider addresses, so those sources may be unavailable
  from affected networks.
- **Gutendex is occasionally slow.** It has its own time budget and
  recovers on the next search.
- **Covers can be blank.** Open Library sometimes serves an empty cover
  image that still loads successfully, so a book card may show a blank
  where a cover should be.
- **Deep pages run dry.** Collections limit how deep they can be queried, so
  very deep result pages may have no results.

## References

- Gordon V. Cormack, Charles L. A. Clarke, and Stefan Büttcher (2009).
  [Reciprocal Rank Fusion Outperforms Condorcet and Individual Rank Learning
  Methods](https://doi.org/10.1145/1571941.1572114). *SIGIR '09*,
  pages 759-760. The paper behind the ranking described in "How the
  ranking works".
- Gregor Hohpe and Bobby Woolf (2003). [Scatter-Gather](https://www.enterpriseintegrationpatterns.com/patterns/messaging/BroadcastAggregate.html).
  *Enterprise Integration Patterns*. The pattern named in "How it
  works".
