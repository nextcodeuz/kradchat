package repository

import (
	"fmt"
	"time"
)

// ScanTime — vaqt maydonlarini barcha drayverlardan (sqlite string/[]byte,
// mysql/postgres time.Time) time.Time ga scan qiladi.
// Ishlatish:
//   - non-null maydon: Scan(&ScanTime{T: &u.LastSeen})
//   - nullable maydon: Scan(&ScanTime{T: &s.RevokedAt})
type ScanTime struct {
	T any // *time.Time yoki **time.Time (nullable)
}

func (s ScanTime) Scan(v any) error {
	switch t := s.T.(type) {
	case nil:
		return nil
	case *time.Time:
		val, err := parseTimeValue(v)
		if err != nil {
			return err
		}
		*t = val
		return nil
	case **time.Time:
		if v == nil {
			*t = nil
			return nil
		}
		val, err := parseTimeValue(v)
		if err != nil {
			return err
		}
		if *t == nil {
			*t = new(time.Time)
		}
		**t = val
		return nil
	default:
		return fmt.Errorf("ScanTime: unsupported target type %T", s.T)
	}
}

func parseTimeValue(v any) (time.Time, error) {
	switch x := v.(type) {
	case nil:
		return time.Time{}, nil
	case time.Time:
		return x, nil
	case string:
		return parseTimeString(x)
	case []byte:
		return parseTimeString(string(x))
	default:
		return time.Time{}, fmt.Errorf("cannot scan %T into time.Time", v)
	}
}

func parseTimeString(s string) (time.Time, error) {
	for _, layout := range []string{
		"2006-01-02 15:04:05.999999999-07:00",
		"2006-01-02 15:04:05.999999999",
		"2006-01-02 15:04:05",
		time.RFC3339Nano,
		time.RFC3339,
	} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("cannot parse time %q", s)
}
