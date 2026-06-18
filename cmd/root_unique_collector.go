package cmd

type uniqueIDCollector struct {
	limit int
	ids   []string
	seen  map[string]struct{}
}

// newUniqueIDCollector returns an empty collector with the specified unique ID limit.
func newUniqueIDCollector(limit int) *uniqueIDCollector {
	return &uniqueIDCollector{
		limit: limit,
		seen:  make(map[string]struct{}),
	}
}

// add appends unseen IDs and reports whether pagination should continue.
func (c *uniqueIDCollector) add(ids []string) bool {
	for _, id := range ids {
		if _, ok := c.seen[id]; ok {
			continue
		}
		c.seen[id] = struct{}{}
		c.ids = append(c.ids, id)
		if c.limit > 0 && len(c.ids) >= c.limit {
			return false
		}
	}
	return true
}
