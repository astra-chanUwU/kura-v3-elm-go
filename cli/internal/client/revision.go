package client

import "encoding/json"

// UnmarshalJSON retains legacy null targets and valid empty target states.
// Older servers omit revertible; target-state presence supplies the same fact.
func (r *Revision) UnmarshalJSON(data []byte) error {
	type revision Revision
	var value struct {
		revision
		Revertible *bool `json:"revertible"`
	}
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	*r = Revision(value.revision)
	r.Revertible = r.TargetTags != nil
	if value.Revertible != nil {
		r.Revertible = *value.Revertible
	}
	return nil
}
