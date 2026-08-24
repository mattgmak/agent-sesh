package registry

// Category groups agent session statuses for compact status-bar display.
type Category string

const (
	CategoryAttention Category = "attention"
	CategoryActive    Category = "active"
	CategoryIdle      Category = "idle"
)

// StatusCategory maps a session status to its display bucket.
func StatusCategory(status Status) Category {
	switch status {
	case StatusHalted, StatusAwaitingInput:
		return CategoryAttention
	case StatusWorking, StatusToolCall:
		return CategoryActive
	default:
		return CategoryIdle
	}
}

// CategoryCounts holds session totals per category.
type CategoryCounts struct {
	Attention int `json:"attention"`
	Active    int `json:"active"`
	Idle      int `json:"idle"`
}

// CountByCategory tallies sessions into attention, active, and idle buckets.
func CountByCategory(sessions []Session) CategoryCounts {
	var counts CategoryCounts
	for _, s := range sessions {
		switch StatusCategory(s.Status) {
		case CategoryAttention:
			counts.Attention++
		case CategoryActive:
			counts.Active++
		default:
			counts.Idle++
		}
	}
	return counts
}
