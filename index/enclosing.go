package index

// enclosingRange is ordered by span, then symbol ID, matching EnclosingSymbol's
// innermost-first selection and its insertion-order tie breaking.
type enclosingRange struct {
	name       string
	start, end int
}

type enclosingRanges []enclosingRange

func (ranges enclosingRanges) at(line int) string {
	for _, r := range ranges {
		if r.start <= line && line <= r.end {
			return r.name
		}
	}
	return ""
}

// enclosingSymbols loads a file's intervals once for an impact traversal,
// replacing a SQL lookup for every reference in the file.
func (s *Store) enclosingSymbols(fileID int64) (enclosingRanges, error) {
	rows, err := s.db.Query(`
		SELECT name, start_line, end_line FROM symbols
		WHERE file_id = ? ORDER BY (end_line - start_line), id
	`, fileID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ranges enclosingRanges
	for rows.Next() {
		var r enclosingRange
		if err := rows.Scan(&r.name, &r.start, &r.end); err != nil {
			return nil, err
		}
		ranges = append(ranges, r)
	}
	return ranges, rows.Err()
}
