package search

import (
	"sort"
	"strings"
	"sync"

	"github.com/alexander-akhmetov/poisk/internal/store"
)

func appendInClause(query string, args []any, column string, values []string) (string, []any) {
	if len(values) == 0 {
		return query, args
	}
	placeholders := strings.Repeat("?,", len(values))
	placeholders = placeholders[:len(placeholders)-1]
	query += " AND " + column + " IN (" + placeholders + ")"
	for _, v := range values {
		args = append(args, v)
	}
	return query, args
}

func searchVec(s *store.Store, queryBlob []byte, topK int, folders []string, filters MetadataFilters, threshold float64) ([]Result, error) {
	if !s.VecAvailable() {
		return nil, nil
	}

	// Metadata filters are applied after KNN, so a filtered query can lose most
	// of its neighbours and needs a wider fetch plus one widening retry.
	// Folders are scoped inside vec0 by the source partition key, which applies
	// k within each requested partition, so a wider k cannot return a row the
	// first attempt missed.
	scoped := !filters.Empty()
	fetchLimit, retryLimit := vecFetchLimits(topK, scoped)
	partitions := vecPartitions(s, folders)

	results, err := runVecQuery(s, queryBlob, topK, fetchLimit, partitions, filters, threshold)
	if err != nil {
		return nil, err
	}

	if scoped && len(results) < topK && retryLimit > fetchLimit {
		results, err = runVecQuery(s, queryBlob, topK, retryLimit, partitions, filters, threshold)
		if err != nil {
			return nil, err
		}
	}

	return results, nil
}

// vecPartitions returns the source values to scan as separate queries. A vec0
// KNN over one partition reads only that partition, so splitting an unscoped
// search into one query per source turns a single-threaded scan of the whole
// table into scans that run at the same time.
//
// Returning nil means "scan everything in one query", which is what an index
// with a single source needs: splitting it would buy nothing.
func vecPartitions(s *store.Store, folders []string) []string {
	if len(folders) > 0 {
		return folders
	}
	sources, err := s.AllSources()
	if err != nil || len(sources) < 2 {
		return nil
	}
	return sources
}

// runVecQuery scans each partition concurrently and keeps the best rows across
// all of them. Every row of the true top k lives in the top k of its own
// partition, so taking k per partition and re-sorting returns the same rows a
// single global scan would.
func runVecQuery(s *store.Store, queryBlob []byte, topK, fetchLimit int, partitions []string, filters MetadataFilters, threshold float64) ([]Result, error) {
	if len(partitions) < 2 {
		return execVecQuery(s, queryBlob, topK, fetchLimit, partitions, filters, threshold)
	}

	per := make([][]Result, len(partitions))
	errs := make([]error, len(partitions))
	var wg sync.WaitGroup
	for i, p := range partitions {
		wg.Go(func() {
			per[i], errs[i] = execVecQuery(s, queryBlob, fetchLimit, fetchLimit, []string{p}, filters, threshold)
		})
	}
	wg.Wait()

	merged := make([]Result, 0, topK*len(partitions))
	for i := range partitions {
		if errs[i] != nil {
			return nil, errs[i]
		}
		merged = append(merged, per[i]...)
	}

	// Score is 1-distance, so the nearest neighbour has the highest score.
	sort.SliceStable(merged, func(a, b int) bool { return merged[a].Score > merged[b].Score })
	if len(merged) > topK {
		merged = merged[:topK]
	}
	return merged, nil
}

// vecFetchLimits returns the k for the first vec0 attempt and for the widening
// retry. Both are capped at store.MaxVecK because sqlite-vec fails a KNN query
// outright above it, which would silently disable vector search. The retry is
// never narrower than the first attempt.
func vecFetchLimits(topK int, scoped bool) (fetchLimit, retryLimit int) {
	overFetch := 5
	if scoped {
		overFetch = 10
	}
	return min(topK*overFetch, store.MaxVecK), min(topK*50, store.MaxVecK)
}

func execVecQuery(s *store.Store, queryBlob []byte, topK, fetchLimit int, folders []string, filters MetadataFilters, threshold float64) ([]Result, error) {
	acquireDBQuery()
	defer releaseDBQuery()

	query := `SELECT e.id, e.file_path, e.line_num, e.end_line, e.chunk_text, v.distance, e.folder, e.language, e.chunk_kind, e.symbol
		FROM vec_embeddings v
		JOIN embeddings e ON e.id = v.rowid
		WHERE v.embedding MATCH ` + s.VecValueExpr() + ` AND k = ?`
	args := []any{queryBlob, fetchLimit}

	// v.source, not e.source: the constraint has to sit on the vec0 table for
	// the source partition key to restrict the KNN scan.
	query, args = appendInClause(query, args, "v.source", folders)
	query, args = appendInClause(query, args, "e.language", filters.Languages)
	query, args = appendInClause(query, args, "e.chunk_kind", filters.Kinds)
	query, args = appendInClause(query, args, "LOWER(e.symbol)", filters.Symbols)
	query += " ORDER BY v.distance ASC"

	rows, err := s.DB().Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []Result
	for rows.Next() {
		var r Result
		var distance float64
		if err := rows.Scan(&r.RowID, &r.FilePath, &r.LineNum, &r.EndLine, &r.Text, &distance, &r.Folder, &r.Language, &r.Kind, &r.Symbol); err != nil {
			return nil, err
		}
		r.Score = 1.0 - distance
		if r.Score >= threshold {
			results = append(results, r)
		}
		if len(results) >= topK {
			break
		}
	}
	return results, rows.Err()
}
