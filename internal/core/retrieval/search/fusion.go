package search

import (
	"regexp"
	"sort"
	"strings"
)

var fusionTokenRE = regexp.MustCompile(`[\pL\pN]+`)

// fuseHybrid applies weighted reciprocal-rank fusion to the two provider
// legs. It deliberately ignores provider score magnitudes because cosine,
// sparse-IDF, and Qdrant fusion scores are not calibrated to one scale.
func fuseHybrid(query string, queryType QueryType, result HybridSearchResult, cfg FusionConfig) []SearchCandidate {
	if len(result.Dense) == 0 && len(result.Lexical) == 0 {
		return append([]SearchCandidate(nil), result.Fused...)
	}
	profile := cfg.profile(queryType)
	rrfK := cfg.RRFK
	if rrfK <= 0 {
		rrfK = 60
	}

	denseRanks := rankByKey(result.Dense)
	lexicalRanks := rankByKey(result.Lexical)
	candidates := make(map[string]SearchCandidate, len(result.Dense)+len(result.Lexical))
	for _, candidate := range result.Dense {
		candidates[candidate.Key()] = candidate
	}
	for _, candidate := range result.Lexical {
		if _, exists := candidates[candidate.Key()]; !exists {
			candidates[candidate.Key()] = candidate
		}
	}

	queryTokens := tokenSet(query)
	queryPhrase := normalizePhrase(query)
	out := make([]SearchCandidate, 0, len(candidates))
	for key, candidate := range candidates {
		score := float64(0)
		if rank, ok := denseRanks[key]; ok {
			score += float64(profile.DenseWeight) / float64(rrfK+rank)
		}
		if rank, ok := lexicalRanks[key]; ok {
			score += float64(profile.BM25Weight) / float64(rrfK+rank)
		}

		body := normalizePhrase(candidate.Text + " " + candidate.WindowText)
		bodyTokens := tokenSet(body)
		overlap := countOverlap(queryTokens, bodyTokens)
		if queryPhrase != "" && strings.Contains(body, queryPhrase) && overlap >= profile.MinExactTokens {
			score += float64(profile.ExactMatchBoost)
		}

		headerOverlap := countOverlap(queryTokens, tokenSet(candidate.Header))
		if headerOverlap >= profile.MinHeaderTokens {
			score += float64(profile.HeaderMatchBoost) * float64(headerOverlap) / float64(max(1, len(queryTokens)))
		}
		candidate.Score = float32(score)
		out = append(out, candidate)
	}

	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		return out[i].Key() < out[j].Key()
	})
	return out
}

func (cfg FusionConfig) profile(queryType QueryType) FusionProfile {
	if profile, ok := cfg.Profiles[queryType]; ok {
		return normalizeFusionProfile(profile, cfg)
	}
	return normalizeFusionProfile(FusionProfile{}, cfg)
}

func normalizeFusionProfile(profile FusionProfile, cfg FusionConfig) FusionProfile {
	if profile.DenseWeight == 0 {
		profile.DenseWeight = cfg.DenseWeight
	}
	if profile.BM25Weight == 0 {
		profile.BM25Weight = cfg.BM25Weight
	}
	if profile.ExactMatchBoost == 0 {
		profile.ExactMatchBoost = cfg.ExactMatchBoost
	}
	if profile.HeaderMatchBoost == 0 {
		profile.HeaderMatchBoost = cfg.HeaderMatchBoost
	}
	if profile.MinExactTokens == 0 {
		profile.MinExactTokens = cfg.MinExactTokens
	}
	if profile.MinHeaderTokens == 0 {
		profile.MinHeaderTokens = cfg.MinHeaderTokens
	}
	if profile.MinExactTokens <= 0 {
		profile.MinExactTokens = 1
	}
	if profile.MinHeaderTokens <= 0 {
		profile.MinHeaderTokens = 1
	}
	return profile
}

func rankByKey(candidates []SearchCandidate) map[string]int {
	ranks := make(map[string]int, len(candidates))
	for rank, candidate := range candidates {
		key := candidate.Key()
		if _, exists := ranks[key]; !exists {
			ranks[key] = rank + 1
		}
	}
	return ranks
}

func tokenSet(value string) map[string]struct{} {
	set := make(map[string]struct{})
	for _, token := range fusionTokenRE.FindAllString(strings.ToLower(value), -1) {
		set[token] = struct{}{}
	}
	return set
}

func normalizePhrase(value string) string {
	return strings.Join(fusionTokenRE.FindAllString(strings.ToLower(value), -1), " ")
}

func countOverlap(left, right map[string]struct{}) int {
	count := 0
	for token := range left {
		if _, ok := right[token]; ok {
			count++
		}
	}
	return count
}

func resolveQueryType(query string, supplied QueryType) QueryType {
	if isQueryType(supplied) {
		return supplied
	}
	lower := strings.ToLower(query)
	switch {
	case strings.ContainsAny(lower, "=^√∫") || strings.Contains(lower, "formula") || strings.Contains(lower, "equation") || strings.Contains(lower, "derivative"):
		return QueryTypeFormula
	case strings.Contains(lower, "compare") || strings.Contains(lower, "difference") || strings.Contains(lower, "versus") || strings.Contains(lower, " vs "):
		return QueryTypeComparison
	case strings.Contains(lower, " and ") && (strings.Contains(lower, "what") || strings.Contains(lower, "how")):
		return QueryTypeMultiHop
	case strings.HasPrefix(strings.TrimSpace(lower), "how ") || strings.Contains(lower, "steps") || strings.Contains(lower, "method") || strings.Contains(lower, "algorithm"):
		return QueryTypeProcedure
	default:
		return QueryTypeFactoid
	}
}

func isQueryType(value QueryType) bool {
	switch value {
	case QueryTypeFactoid, QueryTypeFormula, QueryTypeProcedure, QueryTypeComparison, QueryTypeMultiHop:
		return true
	default:
		return false
	}
}
