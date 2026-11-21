package readstate

import "encoding/json"

// CalendarSelection is audit metadata for an exact selection; evaluation uses
// its frozen subject IDs.
type CalendarSelection struct {
	Cutoff        string `json:"cutoff"`
	TimeZone      string `json:"timeZone"`
	ReferenceDate string `json:"referenceDate"`
}

// UnmarshalJSON decodes with exact keys.
func (c *CalendarSelection) UnmarshalJSON(data []byte) error {
	value, err := decodeCalendar(data)
	if err != nil {
		return err
	}
	*c = value
	return nil
}

func decodeCalendar(raw json.RawMessage) (CalendarSelection, error) {
	value, err := parseObject(raw)
	if err != nil {
		return CalendarSelection{}, err
	}
	var result CalendarSelection
	if result.Cutoff, err = value.str("cutoff"); err != nil {
		return CalendarSelection{}, err
	}
	if result.TimeZone, err = value.str("timeZone"); err != nil {
		return CalendarSelection{}, err
	}
	if result.ReferenceDate, err = value.str("referenceDate"); err != nil {
		return CalendarSelection{}, err
	}
	return result, nil
}
