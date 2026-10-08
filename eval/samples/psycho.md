---
title: "Psycho"
description: "Psycho estimates psychological traits from text and shows the evidence behind each score."
seoTitle: "Psycho: Personality Profiling from Text"
seoDescription: "Psycho estimates Big Five traits, motivations, and values from writing using a dictionary-based method."
answerSummary: "Psycho estimates psychological traits from text and shows the evidence behind each score."
tags: [system-design, nlp, psycholinguistics]
links:
  github: "https://github.com/Chandra179/psycho"
created: 2026-09-29
---

# Psycho: Personality Profiling from Text

Psycho analyzes a writing sample and estimates Big Five traits, motivational
and cognitive tendencies, and value orientations. Each score includes a
confidence level and the linguistic evidence used to calculate it.

It is useful for:

- reflecting on your own writing;
- comparing style and emphasis across blogs, emails, or chat logs;
- research on the relationship between language use and personality;
- writing-sample analysis where the reasoning behind a score matters; and
- analyzing text you own using psycholinguistic methods.

Psycho runs as one process and stores data in an embedded database. It requires
no account and makes no external calls during analysis. It is not a clinical
instrument and does not provide diagnoses or mental-health assessments.

## How it works

```text
your writing → clean and organize → read word by word → infer tendencies → your profile
                                            ↑                 │
                              a psycholinguistic        the evidence behind
                                  dictionary            every score
```

Psycho returns a profile with estimated scores, score ranges, supporting
evidence, and a short summary. You can view it on screen or export it as a PDF.

### 1. Add your writing

You provide a sample, such as saved blog posts, emails, or chat logs. Psycho
checks its length, normalizes the text, and preserves paragraph and sentence
structure.

### 2. Analyze the text

Psycho looks up words in a psycholinguistic dictionary that maps them to
categories. It counts category usage, measures stylistic features, and records
the share of text covered by the dictionary. Coverage indicates how well the
dictionary represents the sample.

### 3. Read your profile

Psycho combines the counts into a profile with estimated score ranges and a
short summary. It saves the profile and the evidence associated with each
score, so results can be traced to the words that contributed to them.

## Main features

- **Local analysis**: no accounts, and the analysis pipeline makes no external calls.
- **Dictionary-based scoring**: the same text produces the same result under the same model, dictionary, and calibration; scores include their supporting evidence.
- **Big Five (OCEAN) traits**: openness, conscientiousness, extraversion, agreeableness, and neuroticism, estimated from word-use patterns.
- **Regulatory Focus**: whether the writing leans toward promotion (gains, aspirations) or prevention (safety, obligations).
- **Need for Cognition**: the writer's tendency toward effortful, analytic thinking.
- **Need for Closure**: comfort with definite answers versus ambiguity.
- **Cognitive style**: systematic versus intuitive processing markers.
- **Schwartz value orientations**: which values the writing emphasizes, from a cross-cultural framework.
- **Score ranges**: ranges depend on text length and dictionary coverage; they are not validated probabilities of correctness.
- **Evidence for scores**: outputs include the linguistic evidence used to calculate them.
- **Quality flags**: short samples or low dictionary coverage widen the ranges and may trigger a warning.
- **Reports you can keep**: a PDF report to save or print, plus the single on-screen report.
- **Saved analyses**: past analyses can be viewed or exported again.

## Algorithms

### Text normalization

Formatting is removed while paragraph structure is preserved. The text is
segmented into sentences and paragraphs to reduce formatting effects on
feature counts.

### Dictionary mapping

Each word is matched against a LIWC-style psycholinguistic dictionary. The
category counts are converted to percentages for the full sample, covering
features such as emotion, cognition, and social language. Coverage is the share
of words matched by at least one category. The feature vector also includes
stylometric measures such as lexical diversity and word length.

### Big Five correlation-weighted heuristic

The Big Five scorer uses a correlation-weighted heuristic inspired by language associations (Yarkoni, 2010). It scales correlations using assumed trait and category standard deviations rather than fitted regression coefficients. With the same model, dictionary, and calibration, category ordering makes results reproducible. Other language measures are project-defined proxies, not official LIWC algorithms or validated personality tests.

### Regulatory Focus

Words associated with gains and aspirations are counted separately from words
associated with safety and duty (Higgins, 1997). Project-defined weights turn
these counts into a text proxy and label. Higgins (1997) supports the construct,
not this word-list scoring formula.

### Need for Cognition

The project weights analytic and intuitive word categories as a proxy for
effortful thinking. Cacioppo and Petty (1982) support the construct, not these
word lists or weights.

### Need for Closure

The project weights certainty language positively and tentative language
negatively as a text proxy. Webster and Kruglanski (1994) describe the construct,
not this scoring formula.

### Schwartz values

Value keywords are grouped by the Schwartz Value Survey framework (Schwartz,
1992), a cross-cultural model of human values, and adapted for how values
co-occur in text.

### Rough score ranges

Each score has a range based on the amount of evidence. Longer samples
generally have narrower ranges, while low dictionary coverage widens them.
Very short samples receive a low-confidence flag. Samples with fewer than 10
normalized Unicode characters or no letters or numbers are rejected. The ranges
use project assumptions and are not validated confidence intervals. Scores are
percentiles relative to about 4,000 blog posts in the Blog Authorship Corpus.
A 60th-percentile score is higher than 60% of texts in that reference sample.

## Current evidence

Psycho is early stage. The speed figure below comes from an automated benchmark
on one development machine. It is for tracking regressions, not a production
measurement. Other figures are design targets.

| Area | Current status |
|---|---|
| Speed | a 5,000-word corpus analyzes in a median of **5 ms** (p95: **11 ms**) on the benchmark machine; the design target is **under 5 seconds** |
| Usage | designed for 1–10 analyses per minute, personal, single-user pacing |
| Storage | about **10 MB** per analyzed subject, including the text, the evidence, and the profile |
| Short samples | samples below 500 words are flagged low-confidence; invalid text is rejected |
| Quality | automated tests check feature counts, word-to-category mappings, and score directions; the dictionary matches about **58%** of words in typical test samples (2,155 words across 36 categories) |
| Measured accuracy | evaluated on **2,442 essays** from a local CSV of 2,467 rows with binary questionnaire labels; provenance is unverified. Trait-ranking AUC ranged from **0.524 to 0.554**, near chance (0.5). These results do not validate individual predictions or show that a larger dictionary would improve accuracy. |

An automated validation suite runs known-profile samples through the full
pipeline. It checks score directions and feature counts, and records latency
percentiles.

The separate [offline supervised experiment](offline-supervised.md) evaluates learned models on held-out essay authors.

## Your data

Each analysis stores the submitted text, word counts, scores, supporting
evidence, and summary in one database file. Analyses remain available after
restarts, and you can back them up by copying that file. Analysis makes no
external service calls.

## References

- Pennebaker, J.W., Boyd, R.L., Jordan, K., & Blackburn, K. (2015). [*The development and psychometric properties of LIWC2015*](https://www.liwc.net/). University of Texas at Austin. The dictionary model behind the word-to-category mapping, and the word-length summary variables stylized here.
- Tweedie, F.J., & Baayen, R.H. (1998). [*How variable may a constant be? Measures of lexical richness in perspective*](https://doi.org/10.1023/A:1001749303136). Computers and the Humanities, 32(5), 323-352. The type-token ratio and related lexical diversity measures.
- Yarkoni, T. (2010). [*Personality in 100,000 words: A large-scale analysis of personality and word use among bloggers*](https://doi.org/10.1016/j.jrp.2010.04.001). Journal of Research in Personality, 44(3), 363-373. Language-category associations inspiring the Big Five heuristic, not fitted weights.
- Pennebaker, J.W., & King, L.A. (1999). [*Linguistic styles: Language use as an individual difference*](https://doi.org/10.1037/0022-3514.77.6.1296). Journal of Personality and Social Psychology, 77(6), 1296-1312. The finding that function words carry stable personality signals.
- Higgins, E.T. (1997). [*Beyond pleasure and pain*](https://doi.org/10.1037/0003-066X.52.12.1280). American Psychologist, 52(12), 1280-1300. Regulatory Focus Theory, promotion versus prevention.
- Cacioppo, J.T., & Petty, R.E. (1982). [*The need for cognition*](https://doi.org/10.1037/0022-3514.42.1.116). Journal of Personality and Social Psychology, 42(1), 116-131. The need for cognition construct.
- Webster, D.M., & Kruglanski, A.W. (1994). [*Individual differences in need for cognitive closure*](https://doi.org/10.1037/0022-3514.67.6.1049). Journal of Personality and Social Psychology, 67(6), 1049-1062. The need for closure construct; the project's text mapping is a proxy.
- Schwartz, S.H. (1992). [*Universals in the content and structure of values: Theoretical advances and empirical tests in 20 countries*](https://doi.org/10.1016/S0065-2601(08)60281-6). Advances in Experimental Social Psychology, 25, 1-65. The Schwartz Value Survey behind the value orientations.
- Schler, J., Koppel, M., Argamon, S., & Pennebaker, J.W. (2006). *Effects of age and gender on blogging*. AAAI Spring Symposium on Computational Approaches to Analyzing Weblogs. The Blog Authorship Corpus used for percentile calibration.
